package service

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"context"
	"log"
	"time"
)

type ICandleRepository interface {
	SaveCandle(candle *entity.CandleEntity) error
	GetHistoricalCandles(symbol string, limit int) ([]*entity.CandleEntity, error)
	GetSymbolDayVolumeScores() ([]model.SymbolScore, error)
}

type ICandleCache interface {
	GetCandles(ctx context.Context, symbol string, interval string, limit int, endTime int64) ([]*dto.CandleResponse, error)
	SetCandles(ctx context.Context, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error
	GetAvailableSymbols(ctx context.Context) ([]string, error)
	UpdateSymbolRanking(ctx context.Context, scores []model.SymbolScore, expiredTime time.Duration) error
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

func (m *CandleQueryService) GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) ([]*dto.CandleResponse, error) {

	// TODO(refactor): By pass cache flag just for testing, remove when deploying to production
	if !request.ByPassCache {
		candleResponse, err := m.candleCache.GetCandles(ctx, request.Symbol, request.Interval, request.Limit, request.EndTime)
		if err == nil && len(candleResponse) > 0 {
			log.Printf("Cache hit for symbol: %s, returning %d candles\n", request.Symbol, len(candleResponse))
			return candleResponse, nil
		}
	}

	log.Println("Cache miss for symbol: " + request.Symbol + ", fetching from repository")

	candleEntities, err := m.repository.GetHistoricalCandles(request.Symbol, request.Limit)
	if err != nil {
		log.Printf("Error fetching historical candles from repository: %v\n", err)
		return nil, err
	}

	candleResponses := make([]*dto.CandleResponse, len(candleEntities))
	for i, candle := range candleEntities {
		open, _ := candle.Open.Float64()
		high, _ := candle.High.Float64()
		low, _ := candle.Low.Float64()
		closePrice, _ := candle.Close.Float64()
		volume, _ := candle.Volume.Float64()

		candleResponses[i] = &dto.CandleResponse{
			OpenTime: candle.StartTime.UnixMilli(),
			Open:     open,
			High:     high,
			Low:      low,
			Close:    closePrice,
			Volume:   volume,
		}
	}

	err = m.candleCache.SetCandles(ctx, request.Symbol, request.Interval, candleResponses, 5*time.Minute)
	if err != nil {
		log.Printf("Error set candles into cache: %v\n", err)
		return nil, err
	}

	return candleResponses, nil
}

func (c *CandleQueryService) GetAvailableSymbols(ctx context.Context) ([]string, error) {
	symbols, err := c.candleCache.GetAvailableSymbols(ctx)
	if err != nil {
		log.Printf("Error fetching available symbols from cache: %v\n", err)
		return nil, err
	}

	if len(symbols) == 0 {
		log.Println("cache miss, fetching from repository")

		symbolScores, err := c.repository.GetSymbolDayVolumeScores()
		if err != nil {
			return nil, err
		}

		expiredTime := 24 * time.Hour
		if err := c.candleCache.UpdateSymbolRanking(ctx, symbolScores, expiredTime); err != nil {
			return nil, err
		}
	}

	return symbols, nil
}
