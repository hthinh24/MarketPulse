package infrastructure

import (
	"MarketPulse/internal/server/model"
	"MarketPulse/pkg/logger"
	"context"
	"sync"
	"time"
)

type ICandleRepository interface {
	GetExchangeQuoteVolumeScores() ([]model.ExchangeScore, error)
	GetSymbolDayVolumeScores(exchange string) ([]model.ExchangeSymbolScore, error)
}

type ICandleCache interface {
	UpdateExchangeRanking(ctx context.Context, scores []model.ExchangeScore, ttl time.Duration) error
	UpdateSymbolRanking(ctx context.Context, exchange string, scores []model.ExchangeSymbolScore, expiredTime time.Duration) error
}

type SymbolRankingUpdater struct {
	log              *logger.Logger
	candleRepository ICandleRepository
	candleCache      ICandleCache
	intervalTime     time.Duration
}

func NewSymbolRankingUpdater(log *logger.Logger, candleRepository ICandleRepository, candleCache ICandleCache, intervalTime time.Duration) *SymbolRankingUpdater {
	return &SymbolRankingUpdater{
		log:              log,
		candleRepository: candleRepository,
		candleCache:      candleCache,
		intervalTime:     intervalTime,
	}
}

func (s *SymbolRankingUpdater) Start(ctx context.Context, wg *sync.WaitGroup) {
	ticker := time.NewTicker(s.intervalTime)
	defer ticker.Stop()
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			exchangeScores, err := s.candleRepository.GetExchangeQuoteVolumeScores()
			if err != nil || len(exchangeScores) == 0 {
				s.log.Error(ctx, "failed to get exchange quote volume scores", err)
				continue
			}

			if err := s.updateExchangesRanking(ctx, exchangeScores); err != nil {
				s.log.Error(ctx, "failed to update exchange rankings in cache", err)
				continue
			}

			for _, exchangeScore := range exchangeScores {
				err := s.updateExchangeSymbolsRanking(ctx, exchangeScore)
				if err != nil {
					s.log.Error(ctx, "failed to update symbol rankings for exchange", err, logger.String("exchange", exchangeScore.Exchange))
					continue
				}
			}

			s.log.Info(ctx, "updated symbol rankings on redis")
		}
	}
}

func (s *SymbolRankingUpdater) updateExchangesRanking(ctx context.Context, exchangeScores []model.ExchangeScore) error {
	exchangeTTL := 30 * time.Minute
	err := s.candleCache.UpdateExchangeRanking(ctx, exchangeScores, exchangeTTL)
	if err != nil {
		s.log.Error(ctx, "failed to update exchange rankings in cache", err)
	}

	return err
}

func (s *SymbolRankingUpdater) updateExchangeSymbolsRanking(ctx context.Context, exchangeScore model.ExchangeScore) error {
	exchange := exchangeScore.Exchange
	symbolScores, err := s.candleRepository.GetSymbolDayVolumeScores(exchange)
	if err != nil || len(symbolScores) == 0 {
		s.log.Error(ctx, "failed to get symbol day volume scores for exchange", err, logger.String("exchange", exchange))
		return err
	}

	symbolTTL := 30 * time.Minute
	err = s.candleCache.UpdateSymbolRanking(ctx, exchange, symbolScores, symbolTTL)
	if err != nil {
		s.log.Error(ctx, "failed to update symbol rankings in cache for exchange", err, logger.String("exchange", exchange))
		return err
	}

	return nil
}
