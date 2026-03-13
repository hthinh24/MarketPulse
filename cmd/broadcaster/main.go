package main

import (
	"MarketPulse/internal/controller/ws"
	"MarketPulse/internal/service"
	"MarketPulse/internal/worker"
	"context"
	"github.com/go-redis/redis/v8"
	"log"
	"net/http"
)

func main() {
	rdb := initRedisDB()
	defer rdb.Close()

	log.Print("Connected to Redis successfully!")

	broadcaster := service.NewBroadcasterService()

	go func() {
		log.Print("Starting Redis subscriber...")
		worker.StartRedisSubscriber(context.Background(), rdb, broadcaster)
	}()

	log.Print("Starting WebSocket server on :8081...")

	wsController := ws.NewWSController(broadcaster)

	http.HandleFunc("/ws", wsController.HandleConnection)
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		log.Fatalf("Failed to start WebSocket server: %v", err)
		return
	}
}

func initRedisDB() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
}
