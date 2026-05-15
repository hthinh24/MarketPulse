package dbsync

import (
	"MarketPulse/internal/server/entity"
	"MarketPulse/pkg/logger"
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IExchangeAPIAdapter interface {
	GetExchangeCode() string
	FetchSymbols() ([]entity.ExchangeSymbol, error)
}

type ExchangeSymbolSyncer struct {
	log      *logger.Logger
	db       *gorm.DB
	adapters []IExchangeAPIAdapter
}

func NewExchangeSymbolSyncer(log *logger.Logger, db *gorm.DB, adapters []IExchangeAPIAdapter) *ExchangeSymbolSyncer {
	return &ExchangeSymbolSyncer{
		log:      log,
		db:       db,
		adapters: adapters,
	}
}

func (s *ExchangeSymbolSyncer) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	s.log.Info(ctx, "symbol syncer worker started")

	s.syncAllExchanges(ctx)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info(ctx, "symbol syncer worker stopped")
			return
		case <-ticker.C:
			s.syncAllExchanges(ctx)
		}
	}
}

func (s *ExchangeSymbolSyncer) syncAllExchanges(ctx context.Context) {
	for _, adapter := range s.adapters {
		exchangeCode := adapter.GetExchangeCode()
		s.log.Info(ctx, "syncing symbols for exchange", logger.String("exchange", exchangeCode))

		symbols, err := adapter.FetchSymbols()
		if err != nil {
			s.log.Error(ctx, "error fetching symbols for exchange", err, logger.String("exchange", exchangeCode))
			continue
		}

		s.upsertSymbols(ctx, symbols)
		s.log.Info(ctx, "finished syncing symbols for exchange", logger.String("exchange", exchangeCode))
	}
}

func (s *ExchangeSymbolSyncer) upsertSymbols(ctx context.Context, symbols []entity.ExchangeSymbol) {
	if len(symbols) == 0 {
		return
	}

	chunkSize := 500
	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "exchange_code"}, {Name: "symbol"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
		}).Create(&batch).Error

		if err != nil {
			s.log.Error(ctx, "error upserting symbols", err)
		}
	}
}
