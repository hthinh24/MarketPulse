package redis

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/server/dto"
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v8"
	"log"
	"time"
)

var keyPrefix = "marketpulse:"
var maxCandleCacheSize = 1000

type CandleCache struct {
	redis *redis.Client
}

func NewCandleCache(redis *redis.Client) *CandleCache {
	return &CandleCache{redis: redis}
}

func (c *CandleCache) SetCandle(ctx context.Context, candleModel *domain.CandleModel, ttl time.Duration) error {
	key := keyPrefix + candleModel.Exchange + ":candles:" + candleModel.Symbol + ":" + candleModel.Timeframe

	candleResponse := c.createCandleResponse(candleModel)
	data, err := json.Marshal(candleResponse)
	if err != nil {
		log.Println("Failed to marshal candle for Redis: " + err.Error())
		return err
	}

	zItem := &redis.Z{
		Score:  float64(candleResponse.OpenTime),
		Member: data,
	}

	if err := c.redis.ZAdd(ctx, key, zItem).Err(); err != nil {
		log.Println("Failed to add candle to Redis: " + err.Error())
		return err
	}
	if err := c.redis.Expire(ctx, key, ttl).Err(); err != nil {
		log.Println("Failed to set TTL for candle in Redis: " + err.Error())
		return err
	}

	stop := -int64(maxCandleCacheSize) - 1
	if err := c.redis.ZRemRangeByRank(ctx, key, 0, stop).Err(); err != nil {
		log.Println("Failed to remove old candles from Redis: " + err.Error())
		return err
	}

	return nil
}

func (c *CandleCache) SetCandles(ctx context.Context, candleModels []*domain.CandleModel, ttl time.Duration) error {
	keyValues := make(map[string][]*redis.Z)

	for _, candleModel := range candleModels {
		candleResponse := c.createCandleResponse(candleModel)
		data, err := json.Marshal(candleResponse)
		if err != nil {
			log.Println("Failed to marshal candle for Redis: " + err.Error())
			continue
		}

		zItem := &redis.Z{
			Score:  float64(candleResponse.OpenTime),
			Member: data,
		}

		key := keyPrefix + candleModel.Exchange + ":candles:" + candleModel.Symbol + ":" + candleModel.Timeframe
		if _, exists := keyValues[key]; !exists {
			keyValues[key] = make([]*redis.Z, 0, maxCandleCacheSize)
		}

		keyValues[key] = append(keyValues[key], zItem)
	}

	for key, zItems := range keyValues {
		if len(zItems) == 0 {
			continue
		}

		if err := c.redis.ZAdd(ctx, key, zItems...).Err(); err != nil {
			log.Println("Failed to add candles to Redis: " + err.Error())
			return err
		}
		if err := c.redis.Expire(ctx, key, ttl).Err(); err != nil {
			log.Println("Failed to set TTL for candles in Redis: " + err.Error())
			return err
		}

		stop := -int64(maxCandleCacheSize) - 1
		if err := c.redis.ZRemRangeByRank(ctx, key, 0, stop).Err(); err != nil {
			log.Println("Failed to remove old candles from Redis: " + err.Error())
			return err
		}
	}

	return nil
}

func (c *CandleCache) createCandleResponse(candle *domain.CandleModel) *dto.CandleResponse {
	startTime := candle.StartTime
	open, _ := candle.Open.Float64()
	high, _ := candle.High.Float64()
	low, _ := candle.Low.Float64()
	closePrice, _ := candle.Close.Float64()
	volume, _ := candle.Volume.Float64()

	return &dto.CandleResponse{
		OpenTime: startTime,
		Open:     open,
		High:     high,
		Low:      low,
		Close:    closePrice,
		Volume:   volume,
	}
}
