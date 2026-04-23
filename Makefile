.DEFAULT_GOAL := help
SHELL := /bin/bash

GO          ?= go
COMPOSE     ?= docker compose
BIN_DIR     := bin
SERVICES    := ingest playback

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy

.PHONY: build
build: $(addprefix build-,$(SERVICES)) ## Build all service binaries

.PHONY: build-%
build-%:
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -o $(BIN_DIR)/$* ./cmd/$*

.PHONY: run-ingest
run-ingest: ## Run ingest locally
	$(GO) run ./cmd/ingest

.PHONY: run-playback
run-playback: ## Run playback locally
	$(GO) run ./cmd/playback

.PHONY: test
test: ## Run tests with race detector
	$(GO) test -race -count=1 ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format code
	$(GO) fmt ./...

.PHONY: up
up: ## Start infra (postgres, redis)
	$(COMPOSE) up -d postgres redis

.PHONY: migrate
migrate: ## Apply SQL migrations
	@for f in migrations/*.sql; do \
	  echo "applying $$f"; \
	  docker exec -i streaming-postgres-1 psql -U streaming -d streaming < $$f; \
	done

.PHONY: proto
proto: ## Generate gRPC stubs from proto files
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       proto/*.proto

.PHONY: up-all
up-all: ## Build and start all services
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop all services
	$(COMPOSE) down

.PHONY: clean
clean: ## Remove build artifacts and volumes
	$(COMPOSE) down -v
	rm -rf $(BIN_DIR)

.PHONY: logs
logs: ## Tail compose logs
	$(COMPOSE) logs -f

.PHONY: docker-build
docker-build: ## Build service images
	$(COMPOSE) build

.PHONY: demo
demo: ## Run end-to-end demo (upload + transcode + URL)
	./scripts/demo.sh

.PHONY: web
web: ## Serve web/ on http://localhost:9000
	cd web && python3 -m http.server 9000
