# wgMicro API

🔒 **Микросервис для управления конфигурациями WireGuard**

WireGuard Configuration Management API - это высокопроизводительный REST API сервис, построенный на Go, который обеспечивает полное управление пирами WireGuard через HTTP интерфейс.

## 🚀 Особенности

- **REST API** для управления конфигурациями WireGuard
- **Автоматическая генерация** клиентских конфигурационных файлов
- **Ротация ключей** для существующих пиров
- **Health Check** эндпоинты для мониторинга
- **Swagger документация** с интерактивным UI
- **Clean Architecture** с разделением слоев ответственности
- **Структурированное логирование** с Zap
- **Docker поддержка** с multi-platform сборкой
- **CI/CD** через GitHub Actions

## 📋 Требования

- **Go 1.24+**
- **WireGuard tools** (`wg` команды)
- **Docker** (опционально)
- **Linux** с поддержкой WireGuard

## 🛠 Установка и запуск

### Локальная разработка

```bash
# Клонирование репозитория
git clone <repository-url>
cd wgMicro_api

# Установка зависимостей
go mod tidy

# Создание .env файла с настройками
cp .env.example .env
# Отредактируйте .env файл, установив необходимые переменные

# Запуск приложения
go run ./cmd/wg-micro-api
```

### Docker

```bash
# Сборка образа
docker build -t wg-micro-api .

# Запуск контейнера
docker run -p 8080:8080 --cap-add=NET_ADMIN \
  -e SERVER_PRIVATE_KEY="ваш_приватный_ключ" \
  wg-micro-api
```

### Docker Compose (рекомендуется)

```yaml
version: '3.8'
services:
  wg-micro-api:
    build: .
    ports:
      - "8080:8080"
    cap_add:
      - NET_ADMIN
    environment:
      - APP_ENV=production
      - SERVER_PRIVATE_KEY=${SERVER_PRIVATE_KEY}
      - SERVER_ENDPOINT_HOST=${SERVER_ENDPOINT_HOST}
    volumes:
      - ./wg0.conf:/etc/wireguard/wg0.conf
```

## ⚙️ Конфигурация

### Переменные окружения

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `APP_ENV` | Окружение приложения (development/production) | `development` |
| `PORT` | Порт HTTP сервера | `8080` |
| `WG_INTERFACE` | Имя интерфейса WireGuard | `wg0` |
| `SERVER_PRIVATE_KEY` | Приватный ключ сервера WireGuard | **обязательно** |
| `SERVER_ENDPOINT_HOST` | Публичный IP адрес сервера | **обязательно** |
| `SERVER_ENDPOINT_PORT` | Порт WireGuard сервера | `51820` |

### Пример .env файла

```env
APP_ENV=production
PORT=8080
WG_INTERFACE=wg0
SERVER_PRIVATE_KEY=your_server_private_key_here
SERVER_ENDPOINT_HOST=203.0.113.1
SERVER_ENDPOINT_PORT=51820
```

## 📡 API Эндпоинты

### Health Check

```http
GET /healthz          # Проверка жизнеспособности
GET /readyz           # Проверка готовности
```

### Управление конфигурациями

```http
GET    /configs                           # Получить все конфигурации
POST   /configs                           # Создать новую конфигурацию
GET    /configs/{publicKey}               # Получить конфигурацию по публичному ключу
PUT    /configs/{publicKey}/allowed-ips   # Обновить разрешенные IP
DELETE /configs/{publicKey}               # Удалить конфигурацию
POST   /configs/client-file               # Сгенерировать клиентский .conf файл
POST   /configs/{publicKey}/rotate        # Ротация ключей пира
```

### Документация

```http
GET /swagger/index.html    # Swagger UI документация
```

## 📝 Примеры использования

### Создание нового пира

```bash
curl -X POST http://localhost:8080/configs \
  -H "Content-Type: application/json" \
  -d '{
    "allowedIPs": ["10.0.0.2/32"],
    "presharedKey": "",
    "persistentKeepalive": 25
  }'
```

### Получение всех конфигураций

```bash
curl http://localhost:8080/configs
```

### Генерация клиентского файла конфигурации

```bash
curl -X POST http://localhost:8080/configs/client-file \
  -H "Content-Type: application/json" \
  -d '{
    "peerPublicKey": "peer_public_key_here",
    "clientPrivateKey": "client_private_key_here"
  }'
```

## 🏗 Архитектура

Проект следует принципам **Clean Architecture**:

```
internal/
├── domain/      # Доменные модели и бизнес-логика
├── service/     # Сервисный слой (use cases)
├── repository/  # Слой доступа к данным (WireGuard)
├── handler/     # HTTP контроллеры (презентационный слой)
├── server/      # Настройка HTTP сервера
├── config/      # Управление конфигурацией
└── logger/      # Структурированное логирование
```

### Принципы проектирования

- **SOLID принципы**
- **Dependency Injection**
- **Interface Segregation**
- **Repository Pattern**
- **Clean Architecture**

## 🧪 Тестирование

### Запуск тестов

```bash
# Все тесты
go test ./...

# Тесты с покрытием
go test -cover ./...

# Интеграционные тесты
go test ./internal/server -tags=integration

# Тесты конкретного пакета
go test ./internal/handler
go test ./internal/service
```

### Структура тестов

- **Unit тесты**: Тестирование отдельных компонентов
- **Integration тесты**: Тестирование HTTP API
- **Mock объекты**: Изоляция от внешних зависимостей
- **Test Coverage**: Проверка покрытия кода тестами

## 🚀 Развертывание

### GitHub Actions CI/CD

Автоматическая сборка и публикация происходит через GitHub Actions:

- **Multi-platform сборка** (AMD64, ARM64)
- **Автоматическая публикация** в Docker Hub
- **Семантическое версионирование**
- **Кэширование** для ускорения сборки

### Production развертывание

1. **Настройте переменные окружения**
2. **Обеспечьте NET_ADMIN права** для контейнера
3. **Настройте мониторинг** health check эндпоинтов
4. **Используйте HTTPS** в production
5. **Настройте логирование** и мониторинг

## 🔧 Разработка

### Генерация Swagger документации

```bash
# После изменения API
swag init -g cmd/wg-micro-api/main.go -o docs
```

### Полезные команды

```bash
# Форматирование кода
go fmt ./...

# Статический анализ
go vet ./...

# Обновление зависимостей
go mod tidy

# Сборка бинарника
go build -o bin/wg-micro-api ./cmd/wg-micro-api
```

## 🛡 Безопасность

- **Неprivileged пользователь** в Docker контейнере
- **Безопасная обработка** приватных ключей
- **CORS защита** для web запросов
- **Структурированное логирование** без sensitive данных
- **Валидация входных данных**

## 📊 Мониторинг

### Health Check эндпоинты

- `/healthz` - Liveness probe (проверка работы приложения)
- `/readyz` - Readiness probe (готовность к обработке запросов)
