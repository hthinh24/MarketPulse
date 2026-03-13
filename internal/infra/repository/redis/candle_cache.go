package redis

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-redis/redis/v8"
	"log"
	"strconv"
	"time"
)

var keyPrefix = "marketpulse:candles:"

type CandleCache struct {
	redis *redis.Client
}

func NewCandleCache(redis *redis.Client) *CandleCache {
	return &CandleCache{redis: redis}
}

// GetCandles
/**
 * Get candles from cache from newest to oldest, with endTime as the upper bound of the timestamp.
 * If endTime is 0, it will get the newest candles.
 */
// TODO(Refactor): Add ZREMRANGEBYSCORE to remove old candles
func (c *CandleCache) GetCandles(ctx context.Context, symbol string, interval string, limit int, endTime int64) ([]*dto.CandleResponse, error) {
	key := keyPrefix + interval + ":" + symbol

	maxScore := "+inf"
	if endTime > 0 {
		maxScore = strconv.FormatInt(endTime, 10)
	}

	opt := &redis.ZRangeBy{
		Max:    maxScore,
		Min:    "-inf",
		Offset: 0,
		Count:  int64(limit),
	}

	val, err := c.redis.ZRevRangeByScore(ctx, key, opt).Result()
	if errors.Is(err, redis.Nil) || len(val) == 0 {
		return nil, nil // Cache misss
	} else if err != nil {
		return nil, err
	}

	var candles []*dto.CandleResponse
	for _, item := range val {
		var candle dto.CandleResponse
		if err := json.Unmarshal([]byte(item), &candle); err != nil {
			log.Println("Failed to unmarshal candle from Redis: " + err.Error())
			continue
		}
		candles = append(candles, &candle)
	}

	return candles, nil
}

func (c *CandleCache) SetCandles(ctx context.Context, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error {
	key := keyPrefix + interval + ":" + symbol

	zItems := make([]*redis.Z, 0, len(candles))
	for _, candle := range candles {
		data, err := json.Marshal(candle)
		if err != nil {
			continue
		}
		zItems = append(zItems, &redis.Z{
			Score:  float64(candle.OpenTime),
			Member: data,
		})

	}

	if len(zItems) == 0 {
		return nil
	}

	if err := c.redis.ZAdd(ctx, key, zItems...).Err(); err != nil {
		log.Println("Failed to add candles to Redis: " + err.Error())
		return err
	}
	if err := c.redis.Expire(ctx, key, ttl).Err(); err != nil {
		log.Println("Failed to set TTL for candles in Redis: " + err.Error())
		return err
	}

	return nil
}
