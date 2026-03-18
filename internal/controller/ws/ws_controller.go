package ws

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type IBroadcaster interface {
	//SubscribeToRoom(room string, conn *websocket.Conn)
	//UnsubscribeFromRoom(room string, conn *websocket.Conn)
	//RemoveClient(conn *websocket.Conn)

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

	//	room := symbol + ":" + intervel
	//	c.broadcaster.SubscribeToRoom(room, conn)
	//
	//	defer func() {
	//		c.broadcaster.UnsubscribeFromRoom(room, conn)
	//		conn.Close()
	//	}()
	//
	//	for {
	//		_, _, err := conn.ReadMessage()
	//		if err != nil {
	//			break
	//		}
	//	}

	room := symbol + ":" + intervel
	client := NewWSClient(c.broadcaster, conn)
	defer c.broadcaster.RemoveClient(client)

	c.broadcaster.SubscribeToRoom(room, client)

	log.Printf("WSClient %p subscribed to room: %s\n", client, room)

	go client.readPump()
	client.writePump()
}

//func (c *WSController) HandleConnection(w http.ResponseWriter, r *http.Request) {
//	symbol := r.URL.Query().Get("symbol")
//	if symbol == "" {
//		log.Printf("Missing required query parameter: symbol\n")
//		return
//	}
//	intervel := r.URL.Query().Get("interval")
//	if intervel == "" {
//		intervel = "1m"
//	}
//
//	conn, err := c.upgrader.Upgrade(w, r, nil)
//	if err != nil {
//		log.Printf("Failed to upgrade to WebSocket: %v\n", err)
//		return
//	}
//
//	room := symbol + ":" + intervel
//	c.broadcaster.SubscribeToRoom(room, conn)
//
//	defer func() {
//		c.broadcaster.UnsubscribeFromRoom(room, conn)
//		conn.Close()
//	}()
//
//	for {
//		_, _, err := conn.ReadMessage()
//		if err != nil {
//			break
//		}
//	}
//}

//func (c *WSController) HandleConnection(w http.ResponseWriter, r *http.Request) {
//	ws, err := c.upgrader.Upgrade(w, r, nil)
//	if err != nil {
//		return
//	}
//
//	go func() {
//		defer c.broadcaster.RemoveClient(ws)
//		for {
//			_, message, err := ws.ReadMessage()
//			if err != nil {
//				break
//			}
//
//			var wsPayload dto.WSPayload
//			if err := json.Unmarshal(message, &wsPayload); err != nil {
//				log.Printf("Invalid message format: %v", err)
//				continue
//			}
//
//			if wsPayload.Room == "" {
//				log.Printf("Room is required for action: %s", wsPayload.Action)
//				continue
//			}
//
//			switch wsPayload.Action {
//			case dto.Subscribe:
//				c.broadcaster.SubscribeToRoom(wsPayload.Room, ws)
//			case dto.Unsubscribe:
//				c.broadcaster.UnsubscribeFromRoom(wsPayload.Room, ws)
//			default:
//				log.Printf("Unknown action: %s", wsPayload.Action)
//			}
//		}
//	}()
//}
