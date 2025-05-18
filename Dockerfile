# Этап 1: Сборка приложения
FROM golang:1.24.0-alpine AS builder
# alpine - для меньшего размера образа сборщика
RUN apk add --no-cache git ca-certificates # Нужны для go mod download, если есть приватные репо или для HTTPS

WORKDIR /app

# Копируем только go.mod и go.sum для кэширования зависимостей Docker
COPY go.mod go.sum ./
RUN go mod download
# RUN go mod verify # Можно добавить для проверки целостности

# Копируем остальной исходный код
COPY . .

# Собираем приложение статически, без CGO, с удалением отладочной информации
# GOARCH=amd64 - для совместимости с большинством серверов x86_64
# -tags netgo - для статической линковки DNS resolver'а
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -tags netgo \
    -installsuffix netgo \
    -o wg-api ./cmd/wg-api

# Этап 2: Создание минимального конечного образа для production
FROM alpine:3.19 AS final
# Используем alpine для минимального размера. Ubuntu 24.04 LTS будет хостом,
# но контейнер может быть на Alpine для эффективности.

# Устанавливаем только необходимые пакеты
# ca-certificates - для HTTPS-соединений из приложения (если оно их делает)
# wireguard-tools - для утилиты 'wg', используемой ServerKeyManager и WGRepository
# tzdata - для корректной работы с часовыми поясами (например, для логгера)
RUN apk add --no-cache ca-certificates wireguard-tools tzdata

WORKDIR /app
# Директория для логов, если приложение пишет в файлы внутри контейнера
# и эти логи не монтируются как volume наружу.
# Если логи монтируются, эту директорию можно не создавать здесь,
# Docker создаст ее при монтировании volume, если она не существует на хосте.
RUN mkdir -p logs

# Копируем собранный бинарник из builder'а
COPY --from=builder /app/wg-api .

# Создаем пользователя и группу без привилегий для запуска приложения
RUN addgroup -S appgroup && adduser -S -G appgroup appuser
# Убедимся, что у пользователя есть права на директорию приложения и логов, если они нужны для записи
RUN chown -R appuser:appgroup /app

# Переключаемся на пользователя без привилегий
USER appuser

# Порт, который слушает приложение внутри контейнера
EXPOSE 8080

# Команда для запуска приложения
ENTRYPOINT ["/app/wg-api"]
# CMD можно использовать для передачи аргументов по умолчанию, если они есть
# CMD ["--config", "/app/config.yaml"] # Пример
