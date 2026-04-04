package main

import (
	"MarketPulse/internal/broadcaster"
	"MarketPulse/internal/broadcaster/controller/ws"
	"MarketPulse/internal/broadcaster/service"
	"context"
	"github.com/go-redis/redis/v8"
	"log"
	"net/http"
)

func main() {
	rdb := initRedisDB()
	defer rdb.Close()

	log.Print("Connected to Redis successfully!")

	broadcasterService := service.NewBroadcasterService()

	go func() {
		log.Print("Starting Redis subscriber...")
		broadcaster.StartRedisSubscriber(context.Background(), rdb, broadcasterService)
	}()

	log.Print("Starting WebSocket server on :8081...")

	wsController := ws.NewWSController(broadcasterService)

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
