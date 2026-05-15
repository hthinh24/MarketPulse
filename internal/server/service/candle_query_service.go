package service

import (
	"MarketPulse/internal/server/dto"
	entity2 "MarketPulse/internal/server/entity"
	error2 "MarketPulse/internal/server/error"
	"MarketPulse/internal/server/model"
	"MarketPulse/pkg/logger"
	"context"
	"errors"
	"time"
)

type ICandleRepository interface {
	IsSymbolExisted(exchange string, symbol string, timeframe string) (bool, error)
	GetNewestCandles(exchange string, symbol string, timeframe string, limit int) ([]*entity2.CandleEntity, error)
	GetHistoricalCandles(exchange string, symbol string, timeframe string, endTime int64, limit int) ([]*entity2.CandleEntity, error)
	GetActiveExchanges() ([]entity2.Exchange, error)
	GetExchangeQuoteVolumeScores() ([]model.ExchangeScore, error)
	GetSymbolDayVolumeScores(exchange string) ([]model.ExchangeSymbolScore, error)
}

type ICandleCache interface {
	GetCandles(ctx context.Context, exchange string, symbol string, interval string, limit int, endTime int64) ([]*dto.CandleResponse, error)
	SetCandles(ctx context.Context, exchange string, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error
	GetActiveExchanges(ctx context.Context) ([]string, error)
	UpdateExchangeRanking(ctx context.Context, scores []model.ExchangeScore, ttl time.Duration) error
	GetAvailableSymbols(ctx context.Context, exchange string) ([]string, error)
	UpdateSymbolRanking(ctx context.Context, exchange string, scores []model.ExchangeSymbolScore, expiredTime time.Duration) error
	GetMinStartTime(ctx context.Context, exchange string, symbol string, interval string) int64
	IsNotFoundSymbol(ctx context.Context, exchange string, symbol string) bool
	SetNotFoundSymbol(ctx context.Context, exchange string, symbol string, ttl time.Duration) error
}

type CandleQueryService struct {
	log         *logger.Logger
	candleCache ICandleCache
	repository  ICandleRepository
}

func NewCandleQueryService(log *logger.Logger, candleCache ICandleCache, repository ICandleRepository) *CandleQueryService {
	return &CandleQueryService{
		log:         log,
		candleCache: candleCache,
		repository:  repository,
	}
}

func (m *CandleQueryService) GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) (*dto.CandleHistoryResponse, error) {
	isExisted := m.isSymbolExisted(ctx, request.Exchange, request.Symbol, request.Timeframe)
	if !isExisted {
		m.log.Warn(ctx, "symbol does not exist in repository", logger.String("exchange", request.Exchange), logger.String("symbol", request.Symbol))
		cacheErr := m.candleCache.SetNotFoundSymbol(ctx, request.Exchange, request.Symbol, 30*time.Minute)
		if cacheErr != nil {
			m.log.Error(ctx, "failed to set not found symbol in cache", cacheErr)
		}
		return nil, errors.New("NOT FOUND")
	}

	minHotCandleStartTime := m.candleCache.GetMinStartTime(ctx, request.Exchange, request.Symbol, request.Timeframe)

	m.log.Info(ctx, "cache lookup params", logger.Int64("request_end_time", request.EndTime), logger.Int64("min_hot_candle_start_time", minHotCandleStartTime))
	m.log.Info(ctx, "cold data check", logger.Bool("is_cold_data", request.EndTime < minHotCandleStartTime))

	isColdData := request.EndTime != 0 && minHotCandleStartTime != 0 && request.EndTime < minHotCandleStartTime
	if isColdData {
		m.log.Info(ctx, "fetching purely cold data", logger.String("symbol", request.Symbol), logger.Int64("before_time", request.EndTime))
		candles, err := m.fetchAndCache(ctx, request)
		if err != nil {
			m.log.Error(ctx, "error fetching candles from db", err, logger.String("symbol", request.Symbol))
			return nil, err
		}
		return createCandleHistoryResponse(request, candles, isColdData), nil
	}

	candleResponses, err := m.candleCache.GetCandles(ctx, request.Exchange, request.Symbol, request.Timeframe, request.Limit, request.EndTime)
	if err != nil || len(candleResponses) == 0 {
		m.log.Info(ctx, "cache miss", logger.String("exchange", request.Exchange), logger.String("symbol", request.Symbol), logger.String("timeframe", request.Timeframe), logger.Int64("start_time", request.EndTime))
		candles, err := m.fetchAndCache(ctx, request)
		if err != nil {
			return nil, err
		}
		return createCandleHistoryResponse(request, candles, false), nil
	}

	m.log.Info(ctx, "cache hit", logger.String("symbol", request.Symbol), logger.Int("candle_count", len(candleResponses)))

	if len(candleResponses) == request.Limit {
		return createCandleHistoryResponse(request, candleResponses, false), nil
	}

	newestCandle := candleResponses[0]
	oldestCandle := candleResponses[len(candleResponses)-1]

	isNoMoreHotCandle := newestCandle.OpenTime > minHotCandleStartTime
	if isNoMoreHotCandle {
		return createCandleHistoryResponse(request, candleResponses, false), nil
	}

	isCacheSlipped := oldestCandle.OpenTime == minHotCandleStartTime
	if isCacheSlipped {
		newEndTime := oldestCandle.OpenTime
		olderCandles, err := m.fetchFromRepository(ctx, request.Exchange, request.Symbol, request.Timeframe, newEndTime, request.Limit-len(candleResponses))
		if err != nil {
			m.log.Error(ctx, "error fetching slipped candles from db", err, logger.String("symbol", request.Symbol))
			return createCandleHistoryResponse(request, candleResponses, false), nil
		}
		candleResponses = append(candleResponses, olderCandles...)
	}

	return createCandleHistoryResponse(request, candleResponses, false), nil
}

func createCandleHistoryResponse(request *dto.GetCandlesRequest, candles []*dto.CandleResponse, isCoolData bool) *dto.CandleHistoryResponse {
	hasMore := len(candles) == request.Limit
	nextEndTime := int64(0)

	if hasMore {
		nextEndTime = candles[len(candles)-1].OpenTime
	}

	return &dto.CandleHistoryResponse{
		Exchange:    request.Exchange,
		Symbol:      request.Symbol,
		Interval:    request.Timeframe,
		HasMore:     hasMore,
		NextEndTime: nextEndTime,
		IsColdData:  isCoolData,
		Candles:     candles,
	}
}

func (c *CandleQueryService) GetActiveExchanges(ctx context.Context) ([]string, error) {
	exchanges, err := c.candleCache.GetActiveExchanges(ctx)
	if err != nil {
		c.log.Error(ctx, "error fetching active exchanges from cache", err)
		return nil, err
	}

	if len(exchanges) != 0 {
		c.log.Info(ctx, "cache hit", logger.String("event", "get_active_exchanges"), logger.Int("exchange_count", len(exchanges)))
		return exchanges, nil
	}

	c.log.Info(ctx, "cache miss, fetching from repository", logger.String("event", "get_active_exchanges"))

	exchangeScores, err := c.repository.GetExchangeQuoteVolumeScores()
	if err != nil {
		return nil, err
	}

	expiredTime := 1 * time.Hour
	if err := c.candleCache.UpdateExchangeRanking(ctx, exchangeScores, expiredTime); err != nil {
		return nil, err
	}

	return c.candleCache.GetActiveExchanges(ctx)
}

func (c *CandleQueryService) GetAvailableSymbols(ctx context.Context, exchange string) ([]string, error) {
	symbols, err := c.candleCache.GetAvailableSymbols(ctx, exchange)
	if err != nil {
		c.log.Error(ctx, "error fetching available symbols from cache", err, logger.String("exchange", exchange))
		return nil, err
	}

	if len(symbols) != 0 {
		c.log.Info(ctx, "cache hit", logger.String("event", "get_available_symbols"), logger.String("exchange", exchange), logger.Int("symbol_count", len(symbols)))
		return symbols, nil
	}

	c.log.Info(ctx, "cache miss, fetching from repository", logger.String("event", "get_available_symbols"), logger.String("exchange", exchange))

	symbolScores, err := c.repository.GetSymbolDayVolumeScores(exchange)
	if err != nil {
		return nil, err
	}

	expiredTime := 1 * time.Hour
	if err := c.candleCache.UpdateSymbolRanking(ctx, exchange, symbolScores, expiredTime); err != nil {
		return nil, err
	}

	return symbols, nil
}

func (m *CandleQueryService) fetchFromRepository(ctx context.Context, exchange, symbol, timeframe string, endTime int64, limit int) ([]*dto.CandleResponse, error) {
	var entities []*entity2.CandleEntity
	var err error

	m.log.Info(ctx, "repository fetch params", logger.String("exchange", exchange), logger.String("symbol", symbol), logger.Int64("end_time", endTime), logger.Int64("min_hot_candle_start_time", m.candleCache.GetMinStartTime(ctx, exchange, symbol, "1m")))

	if endTime == 0 {
		entities, err = m.repository.GetNewestCandles(exchange, symbol, timeframe, limit)
	} else {
		entities, err = m.repository.GetHistoricalCandles(exchange, symbol, timeframe, endTime, limit)
	}

	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CandleResponse, len(entities))
	for i, candle := range entities {
		responses[i] = m.createCandleResponse(candle)
	}

	m.log.Info(ctx, "fetched candles from repository", logger.String("exchange", exchange), logger.String("symbol", symbol), logger.String("timeframe", timeframe), logger.Int("candle_count", len(responses)))
	return responses, nil
}

func (m *CandleQueryService) fetchAndCache(ctx context.Context, req *dto.GetCandlesRequest) ([]*dto.CandleResponse, error) {
	responses, err := m.fetchFromRepository(ctx, req.Exchange, req.Symbol, req.Timeframe, req.EndTime, req.Limit)
	if err != nil && errors.Is(err, error2.NOT_FOUND_ERROR) {
		m.log.Warn(ctx, "symbol not found in repository", logger.String("exchange", req.Exchange), logger.String("symbol", req.Symbol))

		cacheErr := m.candleCache.SetNotFoundSymbol(ctx, req.Exchange, req.Symbol, 30*time.Minute)
		if cacheErr != nil {
			m.log.Error(ctx, "failed to set not found symbol in cache", cacheErr)
		}

		return nil, errors.New("NOT FOUND")
	}
	err = m.candleCache.SetCandles(ctx, req.Exchange, req.Symbol, req.Timeframe, responses, 5*time.Minute)
	if err != nil {
		m.log.Error(ctx, "failed to warm up cache", err, logger.String("exchange", req.Exchange), logger.String("symbol", req.Symbol))
	}
	return responses, nil
}

func (c *CandleQueryService) createCandleResponse(candle *entity2.CandleEntity) *dto.CandleResponse {
	open, _ := candle.Open.Float64()
	high, _ := candle.High.Float64()
	low, _ := candle.Low.Float64()
	closePrice, _ := candle.Close.Float64()
	volume, _ := candle.Volume.Float64()

	return &dto.CandleResponse{
		OpenTime: candle.StartTime.UnixMilli(),
		Open:     open,
		High:     high,
		Low:      low,
		Close:    closePrice,
		Volume:   volume,
	}
}

func (m *CandleQueryService) isSymbolExisted(ctx context.Context, exchange string, symbol string, timeframe string) bool {
	if m.candleCache.IsNotFoundSymbol(ctx, exchange, symbol) {
		return false
	}

	isExisted, err := m.repository.IsSymbolExisted(exchange, symbol, timeframe)
	if err != nil {
		m.log.Error(ctx, "error checking symbol existence in repository", err, logger.String("exchange", exchange), logger.String("symbol", symbol))
		return false
	}

	return isExisted
}
