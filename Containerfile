# Phoenix — Multi-stage container build
#
# Stage 1: Build React frontend
# Stage 2: Build Go binary (embeds the frontend)
# Stage 3: Minimal alpine runtime
#
# Usage:
#   podman build -t phoenix:latest .
#   podman run -d --name phoenix -p 8090:8090 -v /path/to/data:/data:z phoenix:latest

# ── Stage 1: Frontend ────────────────────────────────────────────────────────
FROM docker.io/node:20-alpine AS frontend-builder

WORKDIR /app/web

COPY web/package*.json ./
RUN npm ci --silent

COPY web/ ./
RUN npm run build

# ── Stage 2: Go binary ───────────────────────────────────────────────────────
FROM docker.io/golang:1.26-alpine AS go-builder

RUN apk add --no-cache git

WORKDIR /src

# Download deps first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and embed the built frontend
COPY . .
COPY --from=frontend-builder /app/web/dist ./internal/frontend/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o phoenix ./cmd/phoenix/...

# ── Stage 3: Runtime ─────────────────────────────────────────────────────────
FROM docker.io/alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=go-builder /src/phoenix /app/phoenix

# Data volume — SQLite database lives here
VOLUME /data

ENV PHOENIX_PORT=8090
ENV PHOENIX_DB_PATH=/data/phoenix.db

EXPOSE 8090

ENTRYPOINT ["/app/phoenix"]
