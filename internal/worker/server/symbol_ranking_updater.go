package server

import (
	"MarketPulse/internal/model"
	"context"
	"log"
	"sync"
	"time"
)

type ICandleRepository interface {
	GetSymbolDayVolumeScores() ([]model.SymbolScore, error)
}

type ICandleCache interface {
	UpdateSymbolRanking(ctx context.Context, scores []model.SymbolScore, expiredTime time.Duration) error
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
			scores, err := s.candleRepository.GetSymbolDayVolumeScores()
			if err == nil && len(scores) > 0 {
				expiredTime := 24 * time.Hour
				err := s.candleCache.UpdateSymbolRanking(context.Background(), scores, expiredTime)
				if err != nil {
					return
				}
			}

			log.Println("Updated symbol rankings on Redis")
		}
	}
}
