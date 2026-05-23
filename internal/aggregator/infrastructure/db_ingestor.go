package infrastructure

import (
	"MarketPulse/internal/aggregator/application"
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/common"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"MarketPulse/pkg/logger"
	"context"
	"sync"
	"time"
)

type DBIngestor struct {
	log         *logger.Logger
	saveChan    <-chan common.Envelope[domain.CandleModel]
	candleCache application.ICandleCache
	repository  application.ICandleRepository
	batchSize   int
}

func NewDBIngestor(log *logger.Logger, saveChan <-chan common.Envelope[domain.CandleModel], candleCache application.ICandleCache, repository application.ICandleRepository, batchSize int) *DBIngestor {
	return &DBIngestor{
		log:         log,
		saveChan:    saveChan,
		candleCache: candleCache,
		repository:  repository,
		batchSize:   batchSize,
	}
}

func (d *DBIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer d.cleanUp()

	d.log.Info(ctx, "db ingestor started")

	batch := make([]*domain.CandleModel, 0, d.batchSize)

	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.log.Info(ctx, "db ingestor stopping due to context cancellation")
			d.flush(ctx, batch)
			return

		case <-flushTicker.C:
			batch = d.flush(ctx, batch)

		case candle, ok := <-d.saveChan:
			if !ok {
				d.flush(ctx, batch)
				return
			}

			batch = append(batch, &candle.Payload)

			if len(batch) >= d.batchSize {
				batch = d.flush(ctx, batch)
				flushTicker.Reset(5 * time.Second)
			}
		}
	}
}

func (d *DBIngestor) flush(ctx context.Context, batch []*domain.CandleModel) []*domain.CandleModel {
	ctx, span := observation.Tracer.Start(ctx, "flush_candles")
	defer span.End()

	if len(batch) == 0 {
		return batch
	}

	if err := d.repository.SaveCandles(ctx, batch); err != nil {
		d.log.Error(ctx, "failed to save batch of candles", err, logger.Int("count", len(batch)))
		// TODO: Implement retry logic or move to a dead-letter queue for failed saves
	}

	err := d.candleCache.SetCandles(ctx, batch, 5*time.Minute)
	if err != nil {
		d.log.Error(ctx, "failed to cache batch of candles", err, logger.Int("count", len(batch)))
	}

	d.log.Info(ctx, "flushed batch of candles to database successfully", logger.Int("count", len(batch)))
	return make([]*domain.CandleModel, 0, d.batchSize)
}

func (d *DBIngestor) cleanUp() {
	//close(d.saveChan)
}
