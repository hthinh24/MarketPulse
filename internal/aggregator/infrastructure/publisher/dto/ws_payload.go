package dto

type WSEvent struct {
	EventType string `json:"type"`
	Data      any    `json:"data"`
}
