package ws

import (
	"context"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type IBroadcaster interface {
	SubscribeToRoom(ctx context.Context, topic string, client *WSClient)
	UnsubscribeFromRoom(ctx context.Context, topic string, client *WSClient)
	RemoveClient(ctx context.Context, client *WSClient, reason string)
}

type WSController struct {
	broadcaster IBroadcaster
	upgrader    websocket.Upgrader
}

func NewWSController(b IBroadcaster) *WSController {
	return &WSController{
		broadcaster: b,
		upgrader: websocket.Upgrader{
			// TODO(refactor): Implement proper origin checking in production
			// Currently allowing all origins for simplicity
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (c *WSController) HandleConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := c.validateWSRequest(r)
	if err != nil {
		log.Printf("Invalid WebSocket request: %v\n", err)
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	exchange := r.URL.Query().Get("exchange")
	if exchange == "" {
		log.Printf("Missing required query parameter: exchange\n")
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		log.Printf("Missing required query parameter: symbol\n")
		return
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1m"
	}

	streamType := r.URL.Query().Get("stream")
	if streamType == "" {
		log.Printf("Missing required query parameter: stream\n")
		return
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v\n", err)
		return
	}

	var room string
	if streamType == "candle" {
		room = "candles:" + exchange + ":" + symbol + ":" + interval
	} else if streamType == "orderbook" {
		room = "orderbook:" + exchange + ":" + symbol
	} else {
		http.Error(w, "Invalid stream type", http.StatusBadRequest)
		return
	}

	client := NewWSClient(conn, c.broadcaster)
	defer c.broadcaster.RemoveClient(ctx, client, "disconnect")

	c.broadcaster.SubscribeToRoom(ctx, room, client)

	go client.readPump(ctx)
	client.writePump()
}

func (c *WSController) validateWSRequest(r *http.Request) error {
	// TODO(refactor): Implement proper validation logic for WebSocket request security
	return nil
}
