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
	RemoveClient(client *Client)
}

type Client struct {
	hub      Hub
	conn     *websocket.Conn
	SendChan chan []byte
	closeOne sync.Once
}

func NewClient(hub Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:      hub,
		conn:     conn,
		SendChan: make(chan []byte, 256),
	}
}

func (c *Client) readPump() {
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

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)

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

func (c *Client) Close() {
	c.closeOne.Do(func() {
		log.Printf("Close client connection: %p\n", c)
		close(c.SendChan)
		c.conn.Close()
	})
}
