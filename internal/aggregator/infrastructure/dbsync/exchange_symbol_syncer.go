package dbsync

import (
	"MarketPulse/internal/server/entity"
	"context"
	"log"
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
	db       *gorm.DB
	adapters []IExchangeAPIAdapter
}

func NewExchangeSymbolSyncer(db *gorm.DB, adapters []IExchangeAPIAdapter) *ExchangeSymbolSyncer {
	return &ExchangeSymbolSyncer{
		db:       db,
		adapters: adapters,
	}
}

func (s *ExchangeSymbolSyncer) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Println("Symbol Syncer Worker started...")

	s.syncAllExchanges()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Symbol Syncer Worker stopped.")
			return
		case <-ticker.C:
			s.syncAllExchanges()
		}
	}
}

func (s *ExchangeSymbolSyncer) syncAllExchanges() {
	for _, adapter := range s.adapters {
		exchangeCode := adapter.GetExchangeCode()
		log.Printf("Syncing symbols for exchange: %s\n", exchangeCode)

		symbols, err := adapter.FetchSymbols()
		if err != nil {
			log.Printf("Error fetching symbols for exchange %s: %v\n", exchangeCode, err)
			continue
		}

		s.upsertSymbols(symbols)
		log.Printf("Finished syncing symbols for exchange: %s\n", exchangeCode)
	}
}

func (s *ExchangeSymbolSyncer) upsertSymbols(symbols []entity.ExchangeSymbol) {
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
			log.Printf("Lỗi Upsert symbols: %v\n", err)
		}
	}
}
