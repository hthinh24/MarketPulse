package application

import (
	"MarketPulse/internal/aggregator/domain"
)

type TickDataHandler struct {
	candleService *domain.CandleService
	inbox         <-chan *domain.TickModel
	saveChan      chan<- *domain.CandleModel
	broadcastChan chan<- *domain.CandleModel
}

func NewTickDataHandler(candleService *domain.CandleService, inbox <-chan *domain.TickModel, saveChan chan<- *domain.CandleModel, broadcastChan chan<- *domain.CandleModel) *TickDataHandler {
	return &TickDataHandler{
		candleService: candleService,
		inbox:         inbox,
		saveChan:      saveChan,
		broadcastChan: broadcastChan,
	}
}

func (t *TickDataHandler) Start() {
	for tick := range t.inbox {
		processResult := t.candleService.ProcessTick(tick)

		for _, closedCandle := range processResult.ClosedCandles {
			t.saveChan <- closedCandle
		}

		for _, updatedCandle := range processResult.UpdatedCandles {
			t.broadcastChan <- updatedCandle
		}
	}
}
