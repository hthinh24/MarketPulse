CURRENT_DIR = $(shell pwd)
ROOT_DIR = $(subst \,/,$(CURDIR))
K6_IMAGE = grafana/k6:1.7.1
PORT = 5665

.PHONY: up run run-obs run-all down stop logs test-broadcaster test-api clean help build build-all run-all-go run-ingestor run-aggregator run-broadcaster run-orderbook run-server


# ============== Go Service Commands ==============
# Build all Go services
build:
	go build -o bin/ingestor cmd/ingestor/main.go
	go build -o bin/aggregator cmd/aggregator/main.go
	go build -o bin/broadcaster cmd/broadcaster/main.go
	go build -o bin/orderbook cmd/orderbook/main.go
	go build -o bin/server cmd/server/main.go
	@echo "All services built successfully!"

# Run individual Go services
run-ingestor:
	go run cmd/ingestor/main.go

run-aggregator:
	go run cmd/aggregator/main.go

run-broadcaster:
	go run cmd/broadcaster/main.go

run-orderbook:
	go run cmd/orderbook/main.go

run-server:
	go run cmd/server/main.go

# Run all Go services in parallel (requires docker services to be running)
run:
	@echo "Starting all MarketPulse services..."
	@echo "Make sure Docker services are running: make up"
	@echo ""
	start "ingestor"   cmd /k go run cmd/ingestor/main.go
	start "aggregator" cmd /k go run cmd/aggregator/main.go
	start "broadcaster" cmd /k go run cmd/broadcaster/main.go
	start "orderbook"  cmd /k go run cmd/orderbook/main.go
	start "server"     cmd /k go run cmd/server/main.go

# ============== Docker Commands ==============
# Start all core services (Kafka, Redis, TimescaleDB, etc.)
up:
	docker-compose up -d

# Start observability services (Prometheus, Grafana, OTel Collector)
run-obs:
	docker-compose -f docker-compose.obs.yml up -d

# Start all services (core + observability)
up-all: up run-obs
	@echo "All services started successfully!"
	@echo "Core services are running on:"
	@echo "  - Redis: localhost:6379"
	@echo "  - Kafka: localhost:9092"
	@echo "  - TimescaleDB: localhost:5432"
	@echo "Observability services:"
	@echo "  - Prometheus: http://localhost:9090"
	@echo "  - Grafana: http://localhost:3000"

# Stop all services
down:
	docker-compose down
	docker-compose -f docker-compose.obs.yml down

# View logs from all services
logs:
	docker-compose logs -f

# View logs from specific service (usage: make logs-service SERVICE=kafka)
logs-service:
	docker-compose logs -f $(SERVICE)

# ============== Test Commands ==============
test-broadcaster:
	docker run --rm \
		-v "$(ROOT_DIR)/k6/scripts:/scripts" \
		-p $(PORT):$(PORT) \
		-e K6_WEB_DASHBOARD_EXPORT=/scripts/report.html \
		$(K6_IMAGE) \
		run --out web-dashboard /scripts/broadcaster-test.js

test-api:
	docker run --rm --network host -v $(CURRENT_DIR)/k6/scripts:/scripts grafana/k6:1.7.1 run /scripts/broadcaster-test.js

# ============== Utility Commands ==============
# Display help information
help:
	@echo "MarketPulse Makefile Commands:"
	@echo ""
	@echo "Docker Services:"
	@echo "  make up          - Start all core services (Kafka, Redis, TimescaleDB)"
	@echo "  make run-obs     - Start observability services (Prometheus, Grafana, etc.)"
	@echo "  make up-all      - Start all Docker services (core + observability)"
	@echo "  make down        - Stop all Docker services"
	@echo "  make stop        - Alias for 'make down'"
	@echo "  make logs        - View logs from all Docker services"
	@echo "  make logs-service SERVICE=<name> - View logs from specific Docker service"
	@echo ""
	@echo "Go Services:"
	@echo "  make run              - Run all Go services in parallel"
	@echo "  make build            - Build all Go services"
	@echo "  make run-ingestor     - Run Ingestor service"
	@echo "  make run-aggregator   - Run Aggregator service"
	@echo "  make run-broadcaster  - Run Broadcaster service"
	@echo "  make run-orderbook    - Run Order Book service"
	@echo "  make run-server       - Run API Server service"
	@echo ""
	@echo "Testing:"
	@echo "  make test-broadcaster - Run K6 load test for broadcaster"
	@echo "  make test-api         - Run K6 API test with host network"
	@echo ""
	@echo "Quick Start Example:"
	@echo "  make up              # Start Docker services"
	@echo "  make run		      # Start all Go applications"
