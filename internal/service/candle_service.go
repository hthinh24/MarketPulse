package service

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v8"
	"github.com/shopspring/decimal"
	"log"
	"sync"
	"time"
)

var candleChannelPrefix = "marketpulse:candles:"

type ICandleRepository interface {
	SaveCandle(candle *entity.CandleEntity) error
	GetHistoricalCandles(symbol string, limit int) ([]*entity.CandleEntity, error)
}

type ICandleCache interface {
	GetCandles(ctx context.Context, symbol string, interval string, limit int, endTime int64) ([]*dto.CandleResponse, error)
	SetCandles(ctx context.Context, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error
}

type CandleService struct {
	mu          sync.RWMutex
	candles     map[string]*model.CandleModel
	redisClient *redis.Client
	candleCache ICandleCache
	repository  ICandleRepository
}

func NewCandleService(redisClient *redis.Client, candleCache ICandleCache, repository ICandleRepository) *CandleService {
	return &CandleService{
		mu:          sync.RWMutex{},
		candles:     make(map[string]*model.CandleModel),
		redisClient: redisClient,
		candleCache: candleCache,
		repository:  repository,
	}
}

func (m *CandleService) ProcessTick(symbol string, tickTime int64, trade dto.Trade) {
	m.mu.Lock()
	defer m.mu.Unlock()

	candle, exists := m.candles[symbol]
	if !exists {
		alignedStartTime := (trade.EventTime / 60000) * 60000
		candle = model.NewCandleModel(symbol, alignedStartTime, 60000)
		m.candles[symbol] = candle
	}

	if tickTime > candle.EndTime {
		candleEntity := entity.NewCandleEntity(candle)
		log.Printf("CandleEntity created for %s: Start=%d, End=%d, O=%s, H=%s, L=%s, C=%s, V=%s\n",
			candleEntity.Symbol, candleEntity.StartTime.UnixMilli(), candleEntity.EndTime.UnixMilli(),
			candleEntity.Open.String(), candleEntity.High.String(), candleEntity.Low.String(), candleEntity.Close.String(), candleEntity.Volume.String(),
		)
		err := m.repository.SaveCandle(candleEntity)
		if err != nil {
			return
		}

		newStartTime := (trade.EventTime / 60000) * 60000
		candle.ResetForNextMinute(newStartTime, 60000)
	}

	var price decimal.Decimal
	var quantity decimal.Decimal
	var err error

	if price, err = decimal.NewFromString(trade.Price); err != nil {
		log.Printf("Error parsing price: %v\n", err)
		return
	}
	if quantity, err = decimal.NewFromString(trade.Quantity); err != nil {
		log.Printf("Error parsing quantity: %v\n", err)
		return
	}

	candle.Update(price, quantity, trade.IsMaker)

	channel := candleChannelPrefix + candle.Symbol + ":1m"
	candleResponse := createCandleResponse(candle)
	wsEvent := dto.WSEvent{
		Type: dto.CandleUpdatedEvent,
		Data: candleResponse,
	}

	redisMessage, err := json.Marshal(wsEvent)
	if err != nil {
		log.Printf("Error marshalling candle for Redis: %v\n", err)
		return
	}

	m.redisClient.Publish(context.Background(), channel, redisMessage)
}

func (m *CandleService) GetHistoricalCandles(ctx context.Context, request *dto.GetCandlesRequest) ([]*dto.CandleResponse, error) {

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

func createCandleResponse(candle *model.CandleModel) *dto.CandleResponse {
	startTime := time.UnixMilli(candle.StartTime).UTC()
	open, _ := candle.Open.Float64()
	high, _ := candle.High.Float64()
	low, _ := candle.Low.Float64()
	closePrice, _ := candle.Close.Float64()
	volume, _ := candle.Volume.Float64()

	return &dto.CandleResponse{
		OpenTime: startTime.UnixMilli(),
		Open:     open,
		High:     high,
		Low:      low,
		Close:    closePrice,
		Volume:   volume,
	}
}
