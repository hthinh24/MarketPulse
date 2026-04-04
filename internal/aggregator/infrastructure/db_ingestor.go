package worker

import (
	"MarketPulse/internal/aggregator/application"
	"MarketPulse/internal/aggregator/domain"
	"context"
	"log"
	"sync"
	"time"
)

type DBIngestor struct {
	saveChan    <-chan *domain.CandleModel
	candleCache application.ICandleCache
	repository  application.ICandleRepository
	batchSize   int
}

func NewDBIngestor(saveChan <-chan *domain.CandleModel, candleCache application.ICandleCache, repository application.ICandleRepository, batchSize int) *DBIngestor {
	return &DBIngestor{
		saveChan:    saveChan,
		candleCache: candleCache,
		repository:  repository,
		batchSize:   batchSize,
	}
}

func (d *DBIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer d.cleanUp()

	log.Println("DB Ingestor started")

	batch := make([]*domain.CandleModel, 0, d.batchSize)

	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("DB Ingestor stopping due to context cancellation")
			d.flush(batch)
			return

		case <-flushTicker.C:
			batch = d.flush(batch)

		case candle, ok := <-d.saveChan:
			if !ok {
				d.flush(batch)
				return
			}

			batch = append(batch, candle)

			if len(batch) >= d.batchSize {
				batch = d.flush(batch)
				flushTicker.Reset(5 * time.Second)
			}
		}
	}
}

func (d *DBIngestor) flush(batch []*domain.CandleModel) []*domain.CandleModel {
	if len(batch) == 0 {
		return batch
	}

	ctx := context.Background()
	if err := d.repository.SaveCandles(ctx, batch); err != nil {
		log.Printf("Failed to save batch of %d candles: %v\n", len(batch), err)
		// TODO: Implement retry logic or move to a dead-letter queue for failed saves
	}

	err := d.candleCache.SetCandles(ctx, batch, 5*time.Minute)
	if err != nil {
		log.Printf("Failed to cache batch of %d candles: %v\n", len(batch), err)
	}

	log.Printf("Flushed batch of %d candles to database successfully\n", len(batch))
	return make([]*domain.CandleModel, 0, d.batchSize)
}

func (d *DBIngestor) cleanUp() {
	//close(d.saveChan)
}
