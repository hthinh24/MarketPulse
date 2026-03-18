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
}

type ICandleCache interface {
	SetCandles(ctx context.Context, symbol string, interval string, candles []*dto.CandleResponse, ttl time.Duration) error
}

type DBIngester struct {
	saveChan    chan entity.CandleEntity
	candleCache ICandleCache
	repository  ICandleRepository
}

func NewDBIngestor(bufferSize int, candleCache ICandleCache, repository ICandleRepository) *DBIngester {
	return &DBIngester{
		saveChan:    make(chan entity.CandleEntity, bufferSize),
		candleCache: candleCache,
		repository:  repository,
	}
}

func (d *DBIngester) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	log.Println("DB Ingestor started")
	// TODO(Optimize): Add logic to handle backpressure when saveChan is full
	// such as dropping old candles or implementing a retry mechanism

	// TODO(Refactor): Consider batching saves to the database for better performance
	for candle := range d.saveChan {
		select {
		case <-ctx.Done():
			log.Println("DB Ingestor stopping due to context cancellation")
			d.cleanUp()
			return
		default:
			if err := d.repository.SaveCandle(&candle); err != nil {
				log.Printf("Failed to save candle for %s at %d: %v\n", candle.Symbol, candle.StartTime.UnixMilli(), err)
				// TODO: Implement retry logic or move to a dead-letter queue for failed saves
			}

			candleResponse := createCandleResponse(&candle)
			if err := d.candleCache.SetCandles(context.Background(), candle.Symbol, "1m", []*dto.CandleResponse{candleResponse}, 5*time.Minute); err != nil {
				log.Printf("Failed to update cache for %s at %d: %v\n", candle.Symbol, candle.StartTime.UnixMilli(), err)
			}
		}
	}
}

func (d *DBIngester) cleanUp() {
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
