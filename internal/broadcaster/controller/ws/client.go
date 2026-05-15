package ws

import (
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

const (
	pongWait   = 10 * time.Second    // 60 seconds
	pingPeriod = (pongWait * 9) / 10 // 54 seconds (90% of pongWait)
	writeWait  = 10 * time.Second    // 10 seconds
)

// WSClient represents a single WebSocket connection.
// It should start by calling readPump() and writePump() in separate goroutines
// to handle incoming and outgoing messages from server and health check (heartbeat).
type WSClient struct {
	log         *logger.Logger
	conn        *websocket.Conn
	broadcaster IBroadcaster
	SendChan    chan []byte
	closeOne    sync.Once
}

func NewWSClient(log *logger.Logger, conn *websocket.Conn, broadcaster IBroadcaster) *WSClient {
	return &WSClient{
		log:         log,
		conn:        conn,
		broadcaster: broadcaster,
		SendChan:    make(chan []byte, 256),
	}
}

// readPump listens for incoming messages from websocket connection
// NOTE: In this implementation, we only handle connection health check (pong messages)
// and ignore user messages.
func (c *WSClient) readPump(ctx context.Context) {
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			c.broadcaster.RemoveClient(ctx, c, "disconnect")
			break
		}
		// Ignore user messages
	}
}

// writePump sends messages from SendChan to the WebSocket connection
// and also sends periodic ping messages for health check.
func (c *WSClient) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.SendChan:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg)

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WSClient) Close(ctx context.Context) {
	c.closeOne.Do(func() {
		c.log.Info(ctx, "closing client connection", logger.String("client", fmt.Sprintf("%p", c)))
		close(c.SendChan)
		c.conn.Close()
	})
}
