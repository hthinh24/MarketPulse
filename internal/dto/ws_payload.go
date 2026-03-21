package dto

type EventType string

const (
	CandleUpdated EventType = "candle_update"
)

type WSEvent struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
}

type WSPayload struct {
	Symbol   string `json:"symbol"`
	Interval string `json:"interval,omitempty" binding:"oneof=1m 5m 15m 30m 1h 4h 1d"`
}
