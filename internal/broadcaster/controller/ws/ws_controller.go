package ws

import (
	"MarketPulse/pkg/logger"
	"context"
	"github.com/gorilla/websocket"
	"net/http"
)

type IBroadcaster interface {
	SubscribeToRoom(ctx context.Context, topic string, client *WSClient)
	UnsubscribeFromRoom(ctx context.Context, topic string, client *WSClient)
	RemoveClient(ctx context.Context, client *WSClient, reason string)
}

type WSController struct {
	log         *logger.Logger
	broadcaster IBroadcaster
	upgrader    websocket.Upgrader
}

func NewWSController(log *logger.Logger, b IBroadcaster) *WSController {
	return &WSController{
		log:         log,
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
		c.log.Warn(ctx, "invalid websocket request", logger.Error(err))
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	exchange := r.URL.Query().Get("exchange")
	if exchange == "" {
		c.log.Warn(ctx, "missing required query parameter", logger.String("param", "exchange"))
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		c.log.Warn(ctx, "missing required query parameter", logger.String("param", "symbol"))
		return
	}
	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1m"
	}

	streamType := r.URL.Query().Get("stream")
	if streamType == "" {
		c.log.Warn(ctx, "missing required query parameter", logger.String("param", "stream"))
		return
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log.Error(ctx, "failed to upgrade to websocket", err)
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

	client := NewWSClient(c.log, conn, c.broadcaster)
	defer c.broadcaster.RemoveClient(ctx, client, "disconnect")

	c.broadcaster.SubscribeToRoom(ctx, room, client)

	go client.readPump(ctx)
	client.writePump(ctx)
}

func (c *WSController) validateWSRequest(r *http.Request) error {
	// TODO(refactor): Implement proper validation logic for WebSocket request security
	return nil
}
