package ws

import (
	"github.com/gorilla/websocket"
	"log"
	"sync"
	"time"
)

const (
	pongWait   = 10 * time.Second    // 60 seconds
	pingPeriod = (pongWait * 9) / 10 // 54 seconds (90% of pongWait)
	writeWait  = 10 * time.Second    // 10 seconds
)

type Hub interface {
	RemoveClient(client *WSClient)
}

// WSClient represents a single WebSocket connection to provided hub.
// It should start by calling readPump() and writePump() in separate goroutines
// to handle incoming and outgoing messages from server and health check (heartbeat).
type WSClient struct {
	hub      Hub
	conn     *websocket.Conn
	SendChan chan []byte
	closeOne sync.Once
}

func NewWSClient(hub Hub, conn *websocket.Conn) *WSClient {
	return &WSClient{
		hub:      hub,
		conn:     conn,
		SendChan: make(chan []byte, 256),
	}
}

// readPump listens for incoming messages from websocket connection
// NOTE: In this implementation, we only handle connection health check (pong messages)
// and ignore user messages.
func (c *WSClient) readPump() {
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			c.hub.RemoveClient(c)
			break
		}
		// Ignore user messages
	}
}

// writePump sends messages from SendChan to the WebSocket connection
// and also sends periodic ping messages for health check.
func (c *WSClient) writePump() {
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

func (c *WSClient) Close() {
	c.closeOne.Do(func() {
		log.Printf("Close client connection: %p\n", c)
		close(c.SendChan)
		c.conn.Close()
	})
}
