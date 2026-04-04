package ws

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type IBroadcaster interface {
	SubscribeToRoom(topic string, client *WSClient)
	UnsubscribeFromRoom(topic string, client *WSClient)
	RemoveClient(client *WSClient)
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
	intervel := r.URL.Query().Get("interval")
	if intervel == "" {
		intervel = "1m"
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade to WebSocket: %v\n", err)
		return
	}

	room := exchange + ":" + symbol + ":" + intervel
	client := NewWSClient(c.broadcaster, conn)
	defer c.broadcaster.RemoveClient(client)

	c.broadcaster.SubscribeToRoom(room, client)

	log.Printf("WSClient %p subscribed to room: %s\n", client, room)

	go client.readPump()
	client.writePump()
}

func (c *WSController) validateWSRequest(r *http.Request) error {
	// TODO(refactor): Implement proper validation logic for WebSocket request security
	return nil
}
