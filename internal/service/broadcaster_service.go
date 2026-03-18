package service

import (
	"MarketPulse/internal/controller/ws"
	"sync"
)

type broadcasterService struct {
	rooms map[string]map[*ws.WSClient]bool
	mu    sync.RWMutex
}

func NewBroadcasterService() *broadcasterService {
	return &broadcasterService{
		rooms: make(map[string]map[*ws.WSClient]bool),
	}
}

func (s *broadcasterService) SubscribeToRoom(topic string, client *ws.WSClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rooms[topic] == nil {
		s.rooms[topic] = make(map[*ws.WSClient]bool)
	}
	s.rooms[topic][client] = true
}

func (s *broadcasterService) UnsubscribeFromRoom(topic string, client *ws.WSClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rooms[topic] != nil {
		delete(s.rooms[topic], client)

		if len(s.rooms[topic]) == 0 {
			delete(s.rooms, topic)
		}
	}
}

func (s *broadcasterService) RemoveClient(client *ws.WSClient) {
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
