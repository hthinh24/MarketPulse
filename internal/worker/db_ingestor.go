package worker

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"context"
	"log"
	"sync"
	"time"
)

type ICandleRepository interface {
	SaveCandle(candle *entity.CandleEntity) error
	SaveCandles(candles []entity.CandleEntity) error
}

type ICandleCache interface {
	SetCandles(ctx context.Context, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error
}

type DBIngestor struct {
	saveChan    chan entity.CandleEntity
	candleCache ICandleCache
	repository  ICandleRepository
	batchSize   int
}

func NewDBIngestor(saveChan chan entity.CandleEntity, candleCache ICandleCache, repository ICandleRepository, batchSize int) *DBIngestor {
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

	batch := make([]entity.CandleEntity, 0, d.batchSize)

	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("DB Ingestor stopping due to context cancellation")
			batch = d.flush(batch)
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
				d.flush(batch)
				flushTicker.Reset(5 * time.Second)
			}
		}
	}
}

func (d *DBIngestor) flush(batch []entity.CandleEntity) []entity.CandleEntity {
	if len(batch) == 0 {
		return batch
	}

	if err := d.repository.SaveCandles(batch); err != nil {
		log.Printf("Failed to save batch of %d candles: %v\n", len(batch), err)
		// TODO: Implement retry logic or move to a dead-letter queue for failed saves
	}

	for _, candle := range batch {
		candleResponse := createCandleResponse(&candle)
		err := d.candleCache.SetCandles(context.Background(), candle.Symbol, "1m", []*dto.CandleResponse{candleResponse}, 5*time.Minute)
		if err != nil {
			log.Printf("Failed to update cache for %s at %d: %v\n", candle.Symbol, candle.StartTime.UnixMilli(), err)
		}
	}

	log.Printf("Flushed batch of %d candles to database successfully\n", len(batch))

	batch = batch[:0]
	return batch
}

func (d *DBIngestor) cleanUp() {
	close(d.saveChan)
}

func createCandleResponse(candle *entity.CandleEntity) *dto.CandleResponse {
	startTime := candle.StartTime.UnixMilli()
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
