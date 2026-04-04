package infrastructure

import (
	"MarketPulse/internal/server/model"
	"context"
	"log"
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
	candleRepository ICandleRepository
	candleCache      ICandleCache
	intervalTime     time.Duration
}

func NewSymbolRankingUpdater(candleRepository ICandleRepository, candleCache ICandleCache, intervalTime time.Duration) *SymbolRankingUpdater {
	return &SymbolRankingUpdater{
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
				log.Printf("Failed to get exchange quote volume scores: %v", err)
				continue
			}

			if err := s.updateExchangesRanking(exchangeScores); err != nil {
				log.Printf("Failed to update exchange rankings in cache: %v", err)
				continue
			}

			for _, exchangeScore := range exchangeScores {
				err := s.updateExchangeSymbolsRanking(exchangeScore)
				if err != nil {
					log.Printf("Failed to update symbol rankings for exchange %s: %v", exchangeScore.Exchange, err)
					continue
				}
			}

			log.Println("Updated symbol rankings on Redis")
		}
	}
}

func (s *SymbolRankingUpdater) updateExchangesRanking(exchangeScores []model.ExchangeScore) error {
	exchangeTTL := 30 * time.Minute
	err := s.candleCache.UpdateExchangeRanking(context.Background(), exchangeScores, exchangeTTL)
	if err != nil {
		log.Printf("Failed to update exchange rankings in cache: %v", err)
	}

	return err
}

func (s *SymbolRankingUpdater) updateExchangeSymbolsRanking(exchangeScore model.ExchangeScore) error {
	exchange := exchangeScore.Exchange
	symbolScores, err := s.candleRepository.GetSymbolDayVolumeScores(exchange)
	if err != nil || len(symbolScores) == 0 {
		log.Printf("Failed to get symbol day volume scores for exchange %s: %v", exchange, err)
		return err
	}

	symbolTTL := 30 * time.Minute
	err = s.candleCache.UpdateSymbolRanking(context.Background(), exchange, symbolScores, symbolTTL)
	if err != nil {
		log.Printf("Failed to update symbol rankings in cache for exchange %s: %v", exchange, err)
		return err
	}

	return nil
}
