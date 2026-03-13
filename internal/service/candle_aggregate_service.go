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

type CandleAggregateService struct {
	mu          sync.RWMutex
	candles     map[string]*model.CandleModel
	saveChan    chan entity.CandleEntity
	redisClient *redis.Client
}

func NewCandleAggregateService(redisClient *redis.Client, bufferSize int) *CandleAggregateService {
	return &CandleAggregateService{
		mu:          sync.RWMutex{},
		candles:     make(map[string]*model.CandleModel),
		saveChan:    make(chan entity.CandleEntity, bufferSize),
		redisClient: redisClient,
	}
}

func (m *CandleAggregateService) ProcessTick(symbol string, tickTime int64, trade dto.Trade) {
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

		//err := m.repository.SaveCandle(candleEntity)
		//if err != nil {
		//	return
		//}

		m.saveChan <- *candleEntity

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
