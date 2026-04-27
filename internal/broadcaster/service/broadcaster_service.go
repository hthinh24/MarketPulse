package service

import (
	"MarketPulse/internal/broadcaster/config"
	"MarketPulse/internal/broadcaster/controller/ws"
	"MarketPulse/internal/broadcaster/infrastructure/observation"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"log"
	"strings"
	"time"
)

type cmdType int

const (
	cmdSubscribe cmdType = iota
	cmdUnsubscribe
	cmdBroadcast
)

// roomCmd represents a command sent from Dispatcher to RoomWorker
type roomCmd struct {
	kind   cmdType
	client *ws.WSClient
	msg    []byte
	msgTs  time.Time
}

// doneSignal signals from RoomWorker to Dispatcher when room is empty
type doneSignal struct {
	room string
}

// dispatcherCmd represents a command sent from external callers to Dispatcher
type dispatcherCmd struct {
	kind   cmdType
	room   string
	client *ws.WSClient
	msg    []byte
	msgTs  time.Time
}

type roomWorker struct {
	room     string
	clients  map[*ws.WSClient]bool
	cmdChan  chan roomCmd
	doneChan chan<- doneSignal
}

func newRoomWorker(room string, doneChan chan<- doneSignal, cmdChanSize int) *roomWorker {
	return &roomWorker{
		room:     room,
		clients:  make(map[*ws.WSClient]bool),
		cmdChan:  make(chan roomCmd, cmdChanSize),
		doneChan: doneChan,
	}
}

func (w *roomWorker) run(ctx context.Context) {
	for cmd := range w.cmdChan {
		switch cmd.kind {
		case cmdSubscribe:
			w.clients[cmd.client] = true
			log.Printf("Client %p subscribed to room: %s\n", cmd.client, w.room)

		case cmdUnsubscribe:
			if _, exists := w.clients[cmd.client]; exists {
				delete(w.clients, cmd.client)
				log.Printf("Client %p unsubscribed from room: %s\n", cmd.client, w.room)
			}

			if len(w.clients) == 0 {
				w.doneChan <- doneSignal{room: w.room}
				return
			}

		case cmdBroadcast:
			w.handleBroadcast(ctx, cmd)

			if len(w.clients) == 0 {
				w.doneChan <- doneSignal{room: w.room}
				return
			}
		}
	}
}

func (w *roomWorker) handleBroadcast(ctx context.Context, cmd roomCmd) {
	latency := time.Since(cmd.msgTs).Milliseconds()
	observation.BroadcastLatencyMs.Record(ctx, float64(latency))

	streamType := w.extractStreamType()

	var slowClients []*ws.WSClient

	for client := range w.clients {
		select {
		case client.SendChan <- cmd.msg:
			observation.BroadcastMessagesTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("stream", streamType),
			))
		default:
			slowClients = append(slowClients, client)
			observation.ClientDropsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "slow_consumer"),
			))
		}
	}

	for _, client := range slowClients {
		delete(w.clients, client)
		go client.Close()
	}
}

func (w *roomWorker) extractStreamType() string {
	if strings.HasPrefix(w.room, "candles:") {
		return "candle"
	}
	if strings.HasPrefix(w.room, "orderbook:") {
		return "orderbook"
	}
	return "unknown"
}

type broadcasterService struct {
	cfg      *config.BroadcasterConfig
	cmdChan  chan dispatcherCmd
	doneChan chan doneSignal
	rooms    map[string]*roomWorker
}

func NewBroadcasterService() *broadcasterService {
	return NewBroadcasterServiceWithConfig(config.NewBroadcasterConfig())
}

func NewBroadcasterServiceWithConfig(cfg *config.BroadcasterConfig) *broadcasterService {
	return &broadcasterService{
		cmdChan:  make(chan dispatcherCmd, cfg.DispatcherCmdChanSize),
		doneChan: make(chan doneSignal, cfg.DoneChanSize),
		rooms:    make(map[string]*roomWorker),
		cfg:      cfg,
	}
}

func (s *broadcasterService) Start(ctx context.Context) {
	// Start gauge update ticker
	ticker := time.NewTicker(time.Duration(s.cfg.SnapshotIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return

		case cmd := <-s.cmdChan:
			s.handleCmd(ctx, cmd)

		case done := <-s.doneChan:
			// Room worker reported empty, remove from map
			if worker, exists := s.rooms[done.room]; exists {
				delete(s.rooms, done.room)
				close(worker.cmdChan)
				log.Printf("Room %s closed (empty)\n", done.room)
			}

		case <-ticker.C:
			s.updateCmdChanGauges(ctx)
			s.updateTotalClients(ctx)
			s.updateTotalRooms(ctx)
		}
	}
}

func (s *broadcasterService) handleCmd(ctx context.Context, cmd dispatcherCmd) {
	worker, exists := s.rooms[cmd.room]

	if !exists {
		if cmd.kind != cmdSubscribe {
			return
		}

		worker = newRoomWorker(cmd.room, s.doneChan, s.cfg.WorkerCmdChanSize)
		s.rooms[cmd.room] = worker
		go worker.run(ctx)
		log.Printf("Created new room: %s\n", cmd.room)
	}

	select {
	case worker.cmdChan <- roomCmd{
		kind:   cmd.kind,
		client: cmd.client,
		msg:    cmd.msg,
		msgTs:  cmd.msgTs,
	}:
	default:
		log.Printf("WARNING: Command channel full for room %s, dropping command of type %d\n", cmd.room, cmd.kind)
		observation.ClientDropsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "dispatcher_backpressure"),
		))
	}
}

func (s *broadcasterService) shutdown() {
	for _, worker := range s.rooms {
		close(worker.cmdChan)
	}
}

func (s *broadcasterService) SubscribeToRoom(ctx context.Context, topic string, client *ws.WSClient) {
	select {
	case s.cmdChan <- dispatcherCmd{
		kind:   cmdSubscribe,
		room:   topic,
		client: client,
		msgTs:  time.Now(),
	}:
	case <-ctx.Done():
		log.Printf("SubscribeToRoom cancelled for room %s\n", topic)
	}
}

func (s *broadcasterService) UnsubscribeFromRoom(ctx context.Context, topic string, client *ws.WSClient) {
	select {
	case s.cmdChan <- dispatcherCmd{
		kind:   cmdUnsubscribe,
		room:   topic,
		client: client,
		msgTs:  time.Now(),
	}:
	case <-ctx.Done():
		log.Printf("UnsubscribeFromRoom cancelled for room %s\n", topic)
	}
}

// RemoveClient removes a client from all rooms and closes the connection
func (s *broadcasterService) RemoveClient(ctx context.Context, client *ws.WSClient, reason string) {
	for room := range s.rooms {
		select {
		case s.cmdChan <- dispatcherCmd{
			kind:   cmdUnsubscribe,
			room:   room,
			client: client,
			msgTs:  time.Now(),
		}:
		case <-ctx.Done():
			return
		}
	}

	observation.ClientDropsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
	))

	log.Printf("Client %p removed with reason: %s\n", client, reason)
}

// BroadcastToRoom sends a message to all clients in a specific room
func (s *broadcasterService) BroadcastToRoom(ctx context.Context, topic string, msg []byte) {
	select {
	case <-ctx.Done():
		log.Printf("BroadcastToRoom cancelled for room %s\n", topic)
	case s.cmdChan <- dispatcherCmd{
		kind:  cmdBroadcast,
		room:  topic,
		msg:   msg,
		msgTs: time.Now(),
	}:
	}
}

// updateCmdChanGauges periodically updates command channel queue length gauges
func (s *broadcasterService) updateCmdChanGauges(ctx context.Context) {
	for room, worker := range s.rooms {
		queueLen := len(worker.cmdChan)
		observation.CmdChanQueueLength.Record(ctx, int64(queueLen), metric.WithAttributes(
			attribute.String("room", room),
		))
	}
}

func (s *broadcasterService) updateTotalClients(ctx context.Context) {
	totalClients := 0
	for _, worker := range s.rooms {
		totalClients += len(worker.clients)
	}
	observation.ActiveClients.Record(ctx, int64(totalClients))
}

func (s *broadcasterService) updateTotalRooms(ctx context.Context) {
	observation.ActiveRooms.Record(ctx, int64(len(s.rooms)))
}
