package event

import "time"

type TickEnvelop struct {
	ProducedAt int64
	Payload    TickEvent
}

type TickEvent struct {
	Exchange   string `json:"exchange"`
	Symbol     string `json:"symbol"`
	Price      string `json:"price"`
	Volume     string `json:"volume"`
	EventTime  int64  `json:"eventTime"`
	IsTakerBuy bool   `json:"isTakerBuy"`
}

func NewTickEnvelop(tick TickEvent) TickEnvelop {
	return TickEnvelop{
		ProducedAt: time.Now().UnixMilli(),
		Payload:    tick,
	}
}
