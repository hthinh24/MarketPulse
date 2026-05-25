package dto

type WSEvent[T any] struct {
	TraceParent string `json:"traceparent"`
	Timestamp   int64  `json:"ts"`
	EventType   string `json:"type"`
	Data        T      `json:"data"`
}
