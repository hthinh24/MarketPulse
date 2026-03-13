package ws

import (
	"MarketPulse/internal/dto"
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type IBroadcaster interface {
	SubscribeToRoom(room string, conn *websocket.Conn)
	UnsubscribeFromRoom(room string, conn *websocket.Conn)
	RemoveClient(conn *websocket.Conn)
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

//func (c *WSController) HandleConnection(ctx *gin.Context) {
//	ws, err := c.upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
//	if err != nil {
//		log.Printf("Failed to upgrade to WebSocket: %v", err)
//		return
//	}
//
//	var wsPayload dto.WSPayload
//	if err := ctx.ShouldBindQuery(&wsPayload); err != nil {
//		return
//	}
//
//	log.Printf("Received WebSocket connection with payload: %+v", wsPayload)
//	defer c.broadcaster.UnsubscribeFromRoom(wsPayload.Symbol, ws)
//
//	if wsPayload.Symbol == "" {
//		log.Printf("Missing required query parameter: symbol")
//		return
//	}
//
//	if wsPayload.Interval == "" {
//		wsPayload.Interval = "1m"
//	}
//
//	channel := wsPayload.Symbol + ":" + wsPayload.Interval
//	c.broadcaster.SubscribeToRoom(channel, ws)
//
//	log.Printf("New WebSocket connection established: %s", ws.RemoteAddr())
//
//	for {
//		_, _, err := ws.ReadMessage()
//		if err != nil {
//			break
//		}
//	}
//}

// Bỏ mẹ cái *gin.Context đi, xài chuẩn net/http của Go
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

	room := symbol + ":" + intervel
	c.broadcaster.SubscribeToRoom(room, conn)

	defer func() {
		c.broadcaster.UnsubscribeFromRoom(room, conn)
		conn.Close()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

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
