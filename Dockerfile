# Этап сборки
FROM golang:1.24 AS builder
RUN apt-get update && apt-get install -y git ca-certificates
WORKDIR /app
RUN mkdir -p logs
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o wg-api ./cmd/wg-api

# Минимальный Ubuntu-образ
FROM ubuntu:24.04
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates wireguard-tools && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /root/
RUN mkdir -p logs
COPY --from=builder /app/wg-api .
ENTRYPOINT ["./wg-api"]
EXPOSE 8080
