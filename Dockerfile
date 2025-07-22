# Этап 1: Сборка приложения (Builder Stage)
# Используем официальный образ Go с Alpine Linux для уменьшения размера сборщика.
# Alpine выбран, так как он маленький. Версия Go соответствует твоему go.mod.
FROM golang:1.24.0-alpine AS builder

# Устанавливаем зависимости, необходимые для сборки:
# git - если твои Go модули или go mod download требуют его (например, для приватных репозиториев).
# ca-certificates - для безопасных HTTPS соединений во время скачивания модулей.
RUN apk add --no-cache git ca-certificates

# Устанавливаем рабочую директорию внутри образа сборщика.
WORKDIR /app

# Копируем только go.mod и go.sum сначала.
# Это позволяет Docker кэшировать слой со скачанными зависимостями,
# если сами зависимости (go.mod, go.sum) не менялись, даже если исходный код изменился.
COPY go.mod go.sum ./

# Скачиваем зависимости и проверяем их
RUN go mod download && go mod verify

# Копируем весь остальной исходный код проекта в рабочую директорию /app в образе сборщика.
COPY . .

# Объявляем аргументы сборки, которые будут переданы Docker Buildx автоматически
ARG TARGETARCH
ARG TARGETOS

# Собираем приложение.
# CGO_ENABLED=0 - отключает Cgo, что позволяет создавать статически связанные бинарники,
#                 которые не зависят от системных C-библиотек и проще переносятся.
# GOOS=${TARGETOS:-linux} - используем TARGETOS от Buildx или linux по умолчанию.
# GOARCH=${TARGETARCH:-amd64} - используем TARGETARCH от Buildx или amd64 по умолчанию.
# -ldflags="-s -w" - флаги компоновщика:
#   -s: Убирает таблицу символов (уменьшает размер бинарника).
#   -w: Убирает отладочную информацию DWARF (также уменьшает размер).
# -tags netgo - включает использование Go-реализации DNS resolver'а вместо системного (cgo).
#               Полезно для статических бинарников и избежания проблем с DNS в некоторых Docker-сетях.
# -installsuffix netgo - связано с -tags netgo.
# -o wg-micro-api - указывает имя выходного исполняемого файла.
# ./cmd/wg-micro-api - путь к main пакету твоего приложения.

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-s -w" \
    -tags netgo \
    -installsuffix netgo \
    -o wg-micro-api ./cmd/wg-micro-api


# Этап 2: Создание минимального конечного образа (Final Stage)
# ... остальная часть Dockerfile без изменений ...
FROM alpine:3.19 AS final

# Устанавливаем пакеты, необходимые для работы приложения в runtime:
# ca-certificates - если твое приложение делает HTTPS-запросы к внешним сервисам.
# wireguard-tools - содержит утилиту 'wg', которая используется твоим приложением
#                   (например, ServerKeyManager для 'wg pubkey' и WGRepository для 'wg show/set').
# tzdata - данные часовых поясов, необходимы для корректного отображения времени в логах
#          (например, для time.LoadLocation("Europe/Moscow")).
# iptables - для настройки правил маршрутизации и NAT для VPN трафика
# iproute2 - для управления сетевыми интерфейсами и маршрутизацией
# wget - для автоопределения внешнего IP адреса
RUN apk add --no-cache ca-certificates wireguard-tools tzdata iptables iproute2 wget

# Устанавливаем рабочую директорию внутри конечного образа.
WORKDIR /app

# Копируем ТОЛЬКО скомпилированный бинарник из образа сборщика (builder stage).
COPY --from=builder /app/wg-micro-api .

# Копируем файл wg0.conf из контекста сборки (корня твоего проекта)
# в директорию /app/wg0.conf внутри образа.
# Этот файл будет использоваться по умолчанию, если WG_CONFIG_PATH в .env
# будет установлен в /app/wg0.conf и если ты не будешь монтировать
# другой wg0.conf через volumes в docker-compose.yml.

# Создаем пользователя и группу без root-привилегий для запуска приложения.
# -S (system user/group) - создает пользователя/группу без домашней директории и без пароля,
#                          подходящего для системных служб.
RUN addgroup -S appgroup && adduser -S -G appgroup appuser

# Устанавливаем владельца для директории /app и ее содержимого (включая wg-micro-api и wg0.conf)
# на созданного пользователя appuser. Это важно, чтобы приложение, запущенное от appuser,
# имело права на чтение wg0.conf (если он используется из образа) и на исполнение wg-micro-api.
RUN chown -R appuser:appgroup /app

# Не переключаемся на непривилегированного пользователя, так как entrypoint.sh требует
# права root для настройки iptables, IP forwarding и создания WireGuard интерфейсов.
# VPN сервер должен работать с сетевыми привилегиями.
# USER appuser  # Закомментировано для работы с правами root
EXPOSE 8080

# Копируем и делаем исполняемым наш entrypoint скрипт
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
# Команда по умолчанию, которая будет передана в entrypoint.sh как "$@"
CMD ["/app/wg-micro-api"]
