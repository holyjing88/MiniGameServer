# Multi-stage build for MiniGameServer (RankServer).
FROM golang:1.22-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/minigameserver ./cmd/minigameserver

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/minigameserver /app/minigameserver

EXPOSE 8000 8001
USER nobody
ENTRYPOINT ["/app/minigameserver"]
