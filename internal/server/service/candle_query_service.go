package service

import (
	"MarketPulse/internal/server/dto"
	entity2 "MarketPulse/internal/server/entity"
	error2 "MarketPulse/internal/server/error"
	"MarketPulse/internal/server/model"
	"context"
	"errors"
	"log"
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
	candleCache ICandleCache
	repository  ICandleRepository
}

func NewCandleQueryService(candleCache ICandleCache, repository ICandleRepository) *CandleQueryService {
	return &CandleQueryService{
		candleCache: candleCache,
		repository:  repository,
	}
}

func (m *CandleQueryService) GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) (*dto.CandleHistoryResponse, error) {
	isExisted := m.isSymbolExisted(ctx, request.Exchange, request.Symbol, request.Timeframe)
	if !isExisted {
		log.Printf("Symbol %s on exchange %s does NOT EXIST in repository, marking as NOT FOUND in cache\n", request.Symbol, request.Exchange)
		cacheErr := m.candleCache.SetNotFoundSymbol(ctx, request.Exchange, request.Symbol, 30*time.Minute)
		if cacheErr != nil {
			log.Printf("Failed to set NOT FOUND symbol in cache: %v", cacheErr)
		}
		return nil, errors.New("NOT FOUND")
	}

	minHotCandleStartTime := m.candleCache.GetMinStartTime(ctx, request.Exchange, request.Symbol, request.Timeframe)

	log.Print("request end time: ", request.EndTime, " min hot candle start time: ", minHotCandleStartTime)
	log.Print("is cold data: ", request.EndTime < minHotCandleStartTime)

	isColdData := request.EndTime != 0 && minHotCandleStartTime != 0 && request.EndTime < minHotCandleStartTime
	if isColdData {
		log.Printf("Fetching purely COLD data for %s before %d", request.Symbol, request.EndTime)
		//candles, err := m.fetchFromRepository(request.Exchange, request.Symbol, request.Timeframe, request.EndTime, request.Limit)
		candles, err := m.fetchAndCache(ctx, request)
		if err != nil {
			log.Printf("Error fetching candles from DB: %v", err)
			return nil, err
		}
		return createCandleHistoryResponse(request, candles, isColdData), nil
	}

	candleResponses, err := m.candleCache.GetCandles(ctx, request.Exchange, request.Symbol, request.Timeframe, request.Limit, request.EndTime)
	if err != nil || len(candleResponses) == 0 {
		log.Printf("Cache miss for exchange: %s symbol: %s in timeframe: %s startTime: %d, fetching from repository\n", request.Exchange, request.Symbol, request.Timeframe, request.EndTime)
		candles, err := m.fetchAndCache(ctx, request)
		if err != nil {
			return nil, err
		}
		return createCandleHistoryResponse(request, candles, false), nil
	}

	log.Printf("Cache hit for symbol: %s, returning %d candles\n", request.Symbol, len(candleResponses))

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
		olderCandles, err := m.fetchFromRepository(request.Exchange, request.Symbol, request.Timeframe, newEndTime, request.Limit-len(candleResponses))
		if err != nil {
			log.Printf("Error fetching slipped candles from DB: %v", err)
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
		log.Printf("Error fetching active exchanges from cache: %v\n", err)
		return nil, err
	}

	if len(exchanges) != 0 {
		log.Printf("Cache hit, returning %d active exchanges\n", len(exchanges))
		return exchanges, nil
	}

	log.Println("cache miss, fetching from repository")

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
		log.Printf("Error fetching available symbols from cache: %v\n", err)
		return nil, err
	}

	if len(symbols) != 0 {
		log.Printf("Cache hit, returning %d available symbols for exchange %s\n", len(symbols), exchange)
		return symbols, nil
	}

	log.Println("cache miss, fetching from repository")

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

func (m *CandleQueryService) fetchFromRepository(exchange, symbol, timeframe string, endTime int64, limit int) ([]*dto.CandleResponse, error) {
	var entities []*entity2.CandleEntity
	var err error

	log.Printf("EndTime: %d, MinHotCandleStartTime: %d", endTime, m.candleCache.GetMinStartTime(context.Background(), exchange, symbol, "1m"))

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

	log.Printf("Fetched %d candles from repository for exchange: %s symbol: %s in timeframe: %s\n", len(responses), exchange, symbol, timeframe)
	return responses, nil
}

func (m *CandleQueryService) fetchAndCache(ctx context.Context, req *dto.GetCandlesRequest) ([]*dto.CandleResponse, error) {
	responses, err := m.fetchFromRepository(req.Exchange, req.Symbol, req.Timeframe, req.EndTime, req.Limit)
	if err != nil && errors.Is(err, error2.NOT_FOUND_ERROR) {
		log.Printf("Symbol %s on exchange %s is NOT FOUND in repository, marking as NOT FOUND in cache\n", req.Symbol, req.Exchange)

		cacheErr := m.candleCache.SetNotFoundSymbol(ctx, req.Exchange, req.Symbol, 30*time.Minute)
		if cacheErr != nil {
			log.Printf("Failed to set NOT FOUND symbol in cache: %v", cacheErr)
		}

		return nil, errors.New("NOT FOUND")
	}
	err = m.candleCache.SetCandles(ctx, req.Exchange, req.Symbol, req.Timeframe, responses, 5*time.Minute)
	if err != nil {
		log.Printf("Failed to warm up cache: %v", err)
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
		log.Printf("Error checking symbol existence in repository: %v\n", err)
		return false
	}

	return isExisted
}
