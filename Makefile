.PHONY: build build-web build-go dev dev-go dev-web clean test

BINARY := phoenix
WEB_DIR := web
FRONTEND_DIST := internal/frontend/dist

## build: build the full application (web + go) into a single binary
build: build-web build-go

## build-web: compile the React frontend
build-web:
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

## test: run all Go tests
test:
	go test ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf $(WEB_DIR)/dist
	rm -rf $(FRONTEND_DIST)
