package redis

import (
	"MarketPulse/internal/server/dto"
	"MarketPulse/internal/server/model"
	"MarketPulse/pkg/logger"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-redis/redis/v8"
	"strconv"
	"time"
)

var keyPrefix = "marketpulse:"
var maxCandleCacheSize = 1000

type CandleCache struct {
	log   *logger.Logger
	redis *redis.Client
}

func NewCandleCache(log *logger.Logger, redis *redis.Client) *CandleCache {
	return &CandleCache{
		log:   log,
		redis: redis,
	}
}

// GetCandles
/**
 * Get candles from cache from newest to oldest, with endTime as the upper bound of the timestamp.
 * If endTime is 0, it will get the newest candles.
 */
func (c *CandleCache) GetCandles(ctx context.Context, exchange string, symbol string, interval string, limit int, endTime int64) ([]*dto.CandleResponse, error) {
	key := keyPrefix + exchange + ":candles:" + symbol + ":" + interval

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
			c.log.Warn(ctx, "failed to unmarshal candle from redis", logger.Error(err))
			continue
		}
		candles = append(candles, &candle)
	}

	return candles, nil
}

func (c *CandleCache) SetCandles(ctx context.Context, exchange string, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error {
	if len(candles) == 0 {
		return nil
	}

	key := keyPrefix + exchange + ":candles:" + symbol + ":" + interval

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
		c.log.Error(ctx, "failed to add candles to redis", err, logger.String("exchange", exchange), logger.String("symbol", symbol))
		return err
	}
	if err := c.redis.Expire(ctx, key, ttl).Err(); err != nil {
		c.log.Error(ctx, "failed to set ttl for candles in redis", err, logger.String("exchange", exchange), logger.String("symbol", symbol))
		return err
	}

	stop := -int64(maxCandleCacheSize) - 1
	if err := c.redis.ZRemRangeByRank(ctx, key, 0, stop).Err(); err != nil {
		c.log.Error(ctx, "failed to remove old candles from redis", err, logger.String("exchange", exchange), logger.String("symbol", symbol))
		return err
	}

	return nil
}

func (c *CandleCache) GetActiveExchanges(ctx context.Context) ([]string, error) {
	key := keyPrefix + "rank:exchanges"

	val, err := c.redis.ZRevRange(ctx, key, 0, -1).Result()
	if errors.Is(err, redis.Nil) || len(val) == 0 {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	return val, nil
}

func (c *CandleCache) UpdateExchangeRanking(ctx context.Context, scores []model.ExchangeScore, ttl time.Duration) error {
	key := keyPrefix + "rank:exchanges"
	tmpKey := key + ":tmp"

	var zItems []*redis.Z
	for _, s := range scores {
		zItems = append(zItems, &redis.Z{
			Score:  s.TotalQuoteVolume,
			Member: s.Exchange,
		})
	}

	pipe := c.redis.TxPipeline()
	pipe.Del(ctx, tmpKey)
	pipe.ZAdd(ctx, tmpKey, zItems...)
	pipe.Expire(ctx, tmpKey, ttl)
	pipe.Rename(ctx, tmpKey, key)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *CandleCache) GetAvailableSymbols(ctx context.Context, exchange string) ([]string, error) {
	key := keyPrefix + "rank:" + exchange + ":symbols"
	val, err := c.redis.ZRevRange(ctx, key, 0, -1).Result()

	if errors.Is(err, redis.Nil) || len(val) == 0 {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	return val, nil
}

func (c *CandleCache) UpdateSymbolRanking(ctx context.Context, exchange string, scores []model.ExchangeSymbolScore, expiredTime time.Duration) error {
	key := keyPrefix + "rank:" + exchange + ":symbols"
	tmpKey := key + ":tmp"

	var zItems []*redis.Z
	for _, s := range scores {
		zItems = append(zItems, &redis.Z{
			Score:  s.Score,
			Member: s.Symbol,
		})
	}

	pipe := c.redis.TxPipeline()
	pipe.Del(ctx, tmpKey)
	pipe.ZAdd(ctx, tmpKey, zItems...)
	pipe.Expire(ctx, tmpKey, expiredTime)
	pipe.Rename(ctx, tmpKey, key)
	_, err := pipe.Exec(ctx)

	c.log.Info(ctx, "updated symbol ranking for exchange", logger.String("exchange", exchange))

	return err
}

func (c *CandleCache) GetMinStartTime(ctx context.Context, exchange string, symbol string, interval string) int64 {
	key := keyPrefix + exchange + ":candles:" + symbol + ":" + interval

	val, err := c.redis.ZRangeWithScores(ctx, key, 0, 0).Result()
	if errors.Is(err, redis.Nil) || len(val) == 0 {
		return 0
	}

	if err != nil {
		c.log.Warn(ctx, "failed to get min start time from redis", logger.Error(err), logger.String("exchange", exchange), logger.String("symbol", symbol))
		return 0
	}

	return int64(val[0].Score)
}

func (c *CandleCache) IsNotFoundSymbol(ctx context.Context, exchange string, symbol string) bool {
	key := keyPrefix + "candles:notfound:" + exchange + ":symbols"

	isMember, err := c.redis.SIsMember(ctx, key, symbol).Result()
	if err != nil {
		c.log.Warn(ctx, "failed to check not found symbol in redis", logger.Error(err), logger.String("exchange", exchange), logger.String("symbol", symbol))
		return false
	}

	return isMember
}

func (c *CandleCache) SetNotFoundSymbol(ctx context.Context, exchange string, symbol string, ttl time.Duration) error {
	key := keyPrefix + "notfound:" + exchange + ":symbols"

	return c.redis.SAdd(ctx, key, symbol).Err()
}
