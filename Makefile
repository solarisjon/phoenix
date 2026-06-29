.PHONY: build build-web build-go build-linux deploy deploy-remote deploy-server server-setup dev dev-go dev-web clean test

BINARY := phoenix
WEB_DIR := web
FRONTEND_DIST := internal/frontend/dist

## build: build the full application (web + go) into a single binary
build: build-web build-go

## build-web: compile the React frontend
build-web:
	@echo "→ Installing frontend dependencies..."
	cd $(WEB_DIR) && npm install
	@echo "→ Building frontend..."
	cd $(WEB_DIR) && npm run build
	@echo "→ Copying dist to embed path..."
	rm -rf $(FRONTEND_DIST)
	cp -r $(WEB_DIR)/dist $(FRONTEND_DIST)

## build-go: compile the Go binary (requires build-web first)
build-go:
	@echo "→ Building Go binary..."
	go build -o $(BINARY) ./cmd/phoenix/...
	@echo "✓ Binary: ./$(BINARY)"

## build-linux: cross-compile a static linux/amd64 binary (for manual server deploys)
build-linux: build-web
	@echo "→ Cross-compiling for Linux amd64..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-linux ./cmd/phoenix/...
	@echo "✓ Binary: ./$(BINARY)-linux"


## deploy: build everything, kill the running instance, and restart
deploy: build
	@echo "→ Stopping running phoenix..."
	@pkill -x phoenix 2>/dev/null || true
	@sleep 0.5
	@echo "→ Starting phoenix..."
	@nohup ./$(BINARY) >> /tmp/phoenix.log 2>&1 &
	@sleep 1.5
	@curl -sf http://localhost:8080/api/agents > /dev/null && echo "✓ Phoenix is up at http://localhost:8080" || echo "✗ Phoenix did not start — check /tmp/phoenix.log"

## dev: run frontend and backend dev servers concurrently
dev:
	@echo "→ Starting dev servers (Go :8080, Vite :5173)..."
	@$(MAKE) dev-go & $(MAKE) dev-web

## dev-go: run Go backend with hot reload (requires air)
dev-go:
	air -c .air.toml

## dev-web: run Vite dev server
dev-web:
	cd $(WEB_DIR) && npm run dev

## deploy-remote: deploy latest pushed commit to production server (requires server-setup.sh first run)
deploy-remote:
	@bash scripts/deploy-remote.sh

## server-setup: one-time bootstrap of production server
server-setup:
	@bash scripts/server-setup.sh

## deploy-server: run ON the server after git pull — builds container and restarts phoenix
deploy-server:
	@bash scripts/deploy-server.sh

## test: run all Go tests
test:
	go test ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(FRONTEND_DIST)
