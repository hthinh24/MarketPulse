package event

import "sync"

type OrderLevel struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type OrderBookEvent struct {
	Exchange     string
	Symbol       string
	IsSnapshot   bool
	UpdateID     int64
	PrevUpdateID int64
	Timestamp    int64
	Bids         []OrderLevel
	Asks         []OrderLevel
}

const (
	EventSnapshot = "snapshot"
	EventUpdate   = "update"
	EventClear    = "clear"
)

type OrderBookSnapshot struct {
	EventType string       `json:"eventType"`
	Exchange  string       `json:"exchange"`
	Symbol    string       `json:"symbol"`
	Timestamp int64        `json:"timestamp"`
	Bids      []OrderLevel `json:"bids"`
	Asks      []OrderLevel `json:"asks"`
}

var SnapshotPool = sync.Pool{
	New: func() interface{} {
		return &OrderBookSnapshot{
			Bids: make([]OrderLevel, 0, 50),
			Asks: make([]OrderLevel, 0, 50),
		}
	},
}
