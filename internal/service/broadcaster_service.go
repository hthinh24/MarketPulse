package service

import (
	"MarketPulse/internal/controller/ws"
	"sync"
)

type broadcasterService struct {
	// Room map[topic]map[client]bool
	//rooms map[string]map[*websocket.Conn]bool
	rooms map[string]map[*ws.Client]bool
	mu    sync.RWMutex
}

func NewBroadcasterService() *broadcasterService {
	return &broadcasterService{
		rooms: make(map[string]map[*ws.Client]bool),
	}
}

func (s *broadcasterService) SubscribeToRoom(topic string, client *ws.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rooms[topic] == nil {
		s.rooms[topic] = make(map[*ws.Client]bool)
	}
	s.rooms[topic][client] = true
}

func (s *broadcasterService) UnsubscribeFromRoom(topic string, client *ws.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rooms[topic] != nil {
		delete(s.rooms[topic], client)

		if len(s.rooms[topic]) == 0 {
			delete(s.rooms, topic)
		}
	}
}

func (s *broadcasterService) RemoveClient(client *ws.Client) {
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		client.Close()
	}()

	for topic := range s.rooms {
		if s.rooms[topic][client] {
			delete(s.rooms[topic], client)

			if len(s.rooms[topic]) == 0 {
				delete(s.rooms, topic)
			}
		}
	}
}

func (s *broadcasterService) BroadcastToRoom(topic string, msg []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for client := range s.rooms[topic] {
		select {
		case client.SendChan <- msg:
			// Message sent successfully
		default:
			// drop message when client send channel is full to avoid blocking the broadcaster
			// For now just skip sending to this client
			s.RemoveClient(client)
		}
	}
}

//func NewBroadcasterService() *broadcasterService {
//	return &broadcasterService{
//		rooms: make(map[string]map[*websocket.Conn]bool),
//	}
//}
//
//func (s *broadcasterService) SubscribeToRoom(topic string, conn *websocket.Conn) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	if s.rooms[topic] == nil {
//		s.rooms[topic] = make(map[*websocket.Conn]bool)
//	}
//	s.rooms[topic][conn] = true
//}
//
//func (s *broadcasterService) UnsubscribeFromRoom(topic string, conn *websocket.Conn) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	if s.rooms[topic] != nil {
//		delete(s.rooms[topic], conn)
//
//		if len(s.rooms[topic]) == 0 {
//			delete(s.rooms, topic)
//		}
//	}
//}
//
//func (s *broadcasterService) RemoveClient(conn *websocket.Conn) {
//	s.mu.Lock()
//	defer func() {
//		s.mu.Unlock()
//		conn.Close()
//	}()
//
//	for topic := range s.rooms {
//		if s.rooms[topic][conn] {
//			delete(s.rooms[topic], conn)
//
//			if len(s.rooms[topic]) == 0 {
//				delete(s.rooms, topic)
//			}
//		}
//	}
//}
//
//func (s *broadcasterService) BroadcastToRoom(topic string, msg []byte) {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	for conn := range s.rooms[topic] {
//		conn.WriteMessage(websocket.TextMessage, msg)
//	}
//}
