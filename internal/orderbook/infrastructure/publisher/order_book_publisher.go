package publisher

import (
	"MarketPulse/internal/orderbook/domain"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/go-redis/redis/v8"
	"hash/fnv"
	"log"
	"time"
)

var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "orderbook:%s:%s"
var maxBatchSize = 1000
var tickDuration = 20 * time.Millisecond

type OrderBookPublisher struct {
	redisClient *redis.Client
}

func NewOrderBookPublisher(redisClient *redis.Client) *OrderBookPublisher {
	return &OrderBookPublisher{
		redisClient: redisClient,
	}
}

func (p *OrderBookPublisher) Start(ctx context.Context, publishChan <-chan *domain.OrderBookSnapshot, numOfPublishChannel int) {
	dispatcherChans := make([]chan *domain.OrderBookSnapshot, numOfPublishChannel)
	for i := 0; i < numOfPublishChannel; i++ {
		dispatcherChans[i] = make(chan *domain.OrderBookSnapshot, maxBatchSize)
	}

	for i := 0; i < numOfPublishChannel; i++ {
		go p.worker(ctx, dispatcherChans[i])
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("OrderBookPublisher received shutdown signal, exiting...")
			for _, ch := range dispatcherChans {
				close(ch)
			}
			return
		case snapshot, ok := <-publishChan:
			if !ok {
				log.Println("OrderBookPublisher publish channel closed, exiting...")
				for _, ch := range dispatcherChans {
					close(ch)
				}
				return
			}

			// Hash-based routing: distribute by symbol
			hash := p.hashSymbol(snapshot.Symbol)
			workerIdx := hash % uint32(numOfPublishChannel)
			select {
			case dispatcherChans[workerIdx] <- snapshot:
			default:
				log.Printf("Worker channel %d is full, dropping snapshot for %s:%s", workerIdx, snapshot.Exchange, snapshot.Symbol)
			}
		}
	}
}

func (p *OrderBookPublisher) hashSymbol(symbol string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(symbol))
	return h.Sum32()
}

func (p *OrderBookPublisher) worker(ctx context.Context, workerChan <-chan *domain.OrderBookSnapshot) {
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	batch := make([]*domain.OrderBookSnapshot, 0, maxBatchSize)

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				p.flush(ctx, batch)
			}
			return
		case <-ticker.C:
			if len(batch) > 0 {
				batch = p.flush(ctx, batch)
				ticker.Reset(tickDuration)
			}
		case snapshot, ok := <-workerChan:
			if !ok {
				if len(batch) > 0 {
					p.flush(ctx, batch)
				}
				return
			}

			batch = append(batch, snapshot)

			if len(batch) >= maxBatchSize {
				batch = p.flush(ctx, batch)
			}
		}
	}
}

func (p *OrderBookPublisher) flush(ctx context.Context, batch []*domain.OrderBookSnapshot) []*domain.OrderBookSnapshot {
	pipe := p.redisClient.Pipeline()

	for _, snapshot := range batch {
		room := fmt.Sprintf(channelFormat, snapshot.Exchange, snapshot.Symbol)
		payload, err := sonic.Marshal(snapshot)
		if err != nil {
			log.Printf("Error marshaling snapshot for batch publish %s: %v", room, err)
			continue
		}
		pipe.Publish(ctx, room, payload)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		log.Printf("Error executing Redis pipeline for batch publish: %v", err)
	}

	for _, snapshot := range batch {
		p.cleanupPool(snapshot)
	}

	batch = batch[:0]
	return batch
}

func (p *OrderBookPublisher) cleanupPool(snapshot *domain.OrderBookSnapshot) {
	snapshot.Bids = snapshot.Bids[:0]
	snapshot.Asks = snapshot.Asks[:0]
	domain.SnapshotPool.Put(snapshot)
}
