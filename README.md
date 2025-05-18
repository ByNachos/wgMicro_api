# wgMicro API

---

**wgMicro API** — это микросервис на Go, предоставляющий HTTP API для управления конфигурациями пиров WireGuard. Он позволяет автоматизировать создание, получение, обновление, удаление и ротацию ключей пиров, а также генерировать конфигурационные файлы `.conf` для клиентов.

Сервис спроектирован для работы с утилитой `wg` командной строки и читает конфигурацию серверного интерфейса WireGuard из стандартного файла `wg0.conf` (или аналогичного).

### Оглавление (Русский)

- [Основные возможности](#основные-возможности-рус)
- [Технологический стек](#технологический-стек-рус)
- [Начало работы](#начало-работы-рус)
  - [Предварительные требования](#предварительные-требования-рус)
  - [Конфигурация](#конфигурация-рус)
  - [Локальный запуск](#локальный-запуск-рус)
  - [Запуск в Docker](#запуск-в-docker-рус)
- [API Эндпоинты](#api-эндпоинты-рус)
  - [Health Checks](#health-checks-рус)
  - [Управление пирами](#управление-пирами-рус)
- [Swagger Документация](#swagger-документация-рус)
- [Тестирование](#тестирование-рус)
- [Планы на будущее / TODO](#планы-на-будущее--todo-рус)
- [Лицензия](#лицензия-рус)

### <a name="основные-возможности-рус"></a>Основные возможности

*   **Управление ключами сервера**: Автоматически читает приватный ключ серверного интерфейса WireGuard из его конфигурационного файла и вычисляет публичный ключ.
*   **Полный CRUD для пиров WireGuard**:
    *   Создание новых пиров с автоматической генерацией ключей (публичный и приватный) на стороне сервера.
    *   Получение списка всех пиров.
    *   Получение конфигурации конкретного пира по его публичному ключу.
    *   Обновление списка разрешенных IP-адресов (`AllowedIPs`) для пира.
    *   Удаление пира.
*   **Генерация конфигурационных файлов**:
    *   Создание готовых к использованию `.conf` файлов для клиентов (на основе данных пира и приватного ключа, предоставленного внешним вызывающим приложением).
*   **Ротация ключей пиров**: Автоматическая генерация новой пары ключей для существующего пира с сохранением его основных настроек (например, `AllowedIPs`).
*   **Health Checks**: Эндпоинты `/healthz` (liveness) и `/readyz` (readiness) для интеграции с системами оркестрации.
*   **Логирование**: Структурированное логирование с использованием Zap, с поддержкой разных форматов для разработки и продакшена, и выводом времени в МСК.
*   **Конфигурация через переменные окружения**: Гибкая настройка через `.env` файл или системные переменные.
*   **Docker-совместимость**: Готовый `Dockerfile` и `docker-compose.yml` для легкого развертывания.
*   **Swagger Документация**: Автоматически генерируемая документация API доступна через `/swagger/index.html`.

### <a name="технологический-стек-рус"></a>Технологический стек

*   **Язык**: Go (версия 1.24+)
*   **Веб-фреймворк**: Gin
*   **Логирование**: Zap
*   **Работа с WireGuard**: Через системную утилиту `wg` (`wireguard-tools`)
*   **Конфигурация**: godotenv (для `.env` файлов)
*   **Документация API**: Swaggo (Swagger)
*   **Контейнеризация**: Docker, Docker Compose

### <a name="начало-работы-рус"></a>Начало работы

#### <a name="предварительные-требования-рус"></a>Предварительные требования

*   Go (версия 1.24 или выше)
*   Утилиты `wireguard-tools` (должны быть установлены и доступны в `PATH` на машине, где запускается API, или внутри Docker-контейнера)
*   Docker и Docker Compose (для запуска в контейнере)
*   Git

#### <a name="конфигурация-рус"></a>Конфигурация

Приложение конфигурируется через переменные окружения. Для локальной разработки можно создать файл `.env` в корне проекта.

**Основные переменные окружения:**

| Переменная             | Описание                                                                 | Пример значения             | Обязательно |
| ---------------------- | ------------------------------------------------------------------------ | --------------------------- | ----------- |
| `APP_ENV`              | Режим работы приложения (`development`, `production`, `test`)             | `development`               | Нет (dev)   |
| `PORT`                 | Порт, на котором будет слушать API                                        | `8080`                      | Нет (8080)  |
| `WG_INTERFACE`         | Имя серверного интерфейса WireGuard (например, `wg0`)                     | `wg0`                       | Да          |
| `WG_CONFIG_PATH`       | Путь к файлу конфигурации WireGuard сервера (например, `wg0.conf`)         | `/etc/wireguard/wg0.conf`   | Да          |
| `SERVER_ENDPOINT`      | Внешний `host:port` сервера для клиентских `.conf` файлов                | `your.vpn.server.com:51820` | Нет         |
| `WG_CMD_TIMEOUT_SECONDS`| Таймаут (в секундах) для выполнения команд `wg`                           | `5`                         | Нет (5)     |
| `KEY_GEN_TIMEOUT_SECONDS`| Таймаут (в секундах) для генерации ключей (`wg genkey`, `wg pubkey`)      | `5`                         | Нет (5)     |

**Пример `.env` файла:**
```dotenv
APP_ENV=development
PORT=8080
WG_INTERFACE=wg0
WG_CONFIG_PATH=/etc/wireguard/wg0.conf # Или /app/wg0.conf если копируется в Dockerfile
SERVER_ENDPOINT=myvpn.example.com:51820
WG_CMD_TIMEOUT_SECONDS=5
KEY_GEN_TIMEOUT_SECONDS=5
```

**Файл конфигурации WireGuard (`wg0.conf`):**
Приложение ожидает, что по пути, указанному в `WG_CONFIG_PATH`, будет находиться конфигурационный файл WireGuard, содержащий как минимум секцию `[Interface]` с валидным полем `PrivateKey` для серверного интерфейса. API будет читать этот ключ и добавлять/изменять секции `[Peer]` в этом файле.

Пример минимального `wg0.conf` (только для запуска, пиры будут добавляться через API):
```ini
[Interface]
Address = 10.0.0.1/24
ListenPort = 51820
PrivateKey = <ВАШ_СЕКРЕТНЫЙ_КЛЮЧ_СЕРВЕРА_WG>
```

#### <a name="локальный-запуск-рус"></a>Локальный запуск (для разработки)

1.  Убедитесь, что Go и `wireguard-tools` установлены.
2.  Создайте и настройте файл `.env`.
3.  Создайте файл `wg0.conf` по пути, указанному в `WG_CONFIG_PATH` в `.env`.
4.  Загрузите зависимости: `go mod tidy && go mod download`
5.  Соберите и запустите:
    ```bash
    go run ./cmd/wg-api/main.go
    ```
    Или:
    ```bash
    go build -o wg-api ./cmd/wg-api/main.go
    ./wg-api
    ```

#### <a name="запуск-в-docker-рус"></a>Запуск в Docker

1.  Убедитесь, что Docker и Docker Compose установлены.
2.  Создайте файл `.env` (см. выше).
3.  **Для монтирования `wg0.conf` с хоста (рекомендуется для гибкости):**
    *   Убедитесь, что `wg0.conf` существует на хост-машине (например, `/etc/wireguard/wg0.conf` или `./wg0.conf` для локального теста).
    *   В `Dockerfile` строка `COPY wg0.conf ...` должна быть удалена или закомментирована.
    *   В `docker-compose.yml` настройте монтирование `wg0.conf` в секции `volumes` (например, `- /etc/wireguard/wg0.conf:/etc/wireguard/wg0.conf`). Убедитесь, что путь назначения в контейнере соответствует `WG_CONFIG_PATH` из `.env`. **Уберите `:ro` (read-only) если API должен изменять этот файл.**
4.  Соберите и запустите:
    ```bash
    docker-compose build
    docker-compose up -d
    ```
5.  Просмотр логов:
    ```bash
    docker-compose logs -f wg-api
    ```

### <a name="api-эндпоинты-рус"></a>API Эндпоинты

Полная спецификация API доступна через Swagger UI по адресу `/swagger/index.html` после запуска приложения.

#### <a name="health-checks-рус"></a>Health Checks

*   **`GET /healthz`**: Liveness probe. Возвращает `200 OK` со статусом `"ok"`, если приложение запущено.
*   **`GET /readyz`**: Readiness probe. Возвращает `200 OK` со статусом `"ready"`, если приложение готово обрабатывать запросы (включая доступность утилиты `wg`). В случае проблем возвращает `503 Service Unavailable`.

#### <a name="управление-пирами-рус"></a>Управление пирами

*   **`GET /configs`**: Получить список всех конфигураций пиров.
    *   Ответ: `200 OK` с массивом `domain.Config`.
*   **`POST /configs`**: Создать нового пира. Сервер генерирует для него ключи.
    *   Тело запроса: `domain.CreatePeerRequest` (`allowed_ips`, опционально `preshared_key`, `persistent_keepalive`).
    *   Ответ: `201 Created` с `domain.Config` нового пира, **включая `privateKey`**. Клиент обязан сохранить этот `privateKey`.
*   **`GET /configs/{publicKey}`**: Получить конфигурацию конкретного пира.
    *   Ответ: `200 OK` с `domain.Config`.
*   **`PUT /configs/{publicKey}/allowed-ips`**: Обновить `AllowedIPs` для пира.
    *   Тело запроса: `domain.AllowedIpsUpdate`.
    *   Ответ: `200 OK` (или `204 No Content`).
*   **`DELETE /configs/{publicKey}`**: Удалить пира.
    *   Ответ: `204 No Content`.
*   **`POST /configs/{publicKey}/rotate`**: Ротировать ключи для существующего пира. Сервер генерирует новую пару ключей.
    *   Ответ: `200 OK` с `domain.Config` пира с новыми ключами, **включая новый `privateKey`**. Клиент обязан сохранить этот `privateKey`.
*   **`POST /configs/client-file`**: Сгенерировать `.conf` файл для клиента.
    *   Тело запроса: `domain.ClientFileRequest` (содержит `client_public_key` и `client_private_key`, предоставленные внешним приложением).
    *   Ответ: `200 OK` с содержимым файла `.conf` в формате `text/plain`. API не хранит переданный `client_private_key`.

### <a name="swagger-документация-рус"></a>Swagger Документация

После запуска приложения, интерактивная документация Swagger UI доступна по адресу:
`http://localhost:<PORT>/swagger/index.html`
(замените `<PORT>` на порт, указанный в конфигурации, по умолчанию `8080`).

### <a name="тестирование-рус"></a>Тестирование

Для запуска юнит-тестов:
```bash
go test ./... -v
```
Для интеграционных тестов (требуют доступности утилиты `wg` и правильной настройки):
```bash
go test ./internal/server/... -v
```
(Перед запуском интеграционных тестов убедитесь, что переменные окружения и тестовый `wg0.conf` настроены как описано в `internal/server/integration_test.go`).

### <a name="планы-на-будущее--todo-рус"></a>Планы на будущее / TODO

*   [ ] Более гранулярное управление правами доступа (если потребуется).
*   [ ] Расширенная валидация входных данных (форматы ключей, CIDR и т.д.).
*   [ ] Возможность хранения приватных ключей клиентов (сгенерированных сервером) в более персистентном хранилище (например, Vault или зашифрованная БД), если потребуется их повторное получение без участия клиента.
*   [ ] Добавить больше юнит и интеграционных тестов для полного покрытия.
*   [ ] Настройка CI/CD для автоматической сборки, тестирования и деплоя.


---
<br>
<br>
---

## Documentation

**wgMicro API** is a Go-based microservice that provides an HTTP API for managing WireGuard peer configurations. It allows automating the creation, retrieval, updating, deletion, and key rotation of peers, as well as generating `.conf` configuration files for clients.

The service is designed to work with the `wg` command-line utility and reads the server's WireGuard interface configuration from a standard `wg0.conf` file (or similar).

### Table of Contents (English)

- [Key Features](#key-features-en)
- [Tech Stack](#tech-stack-en)
- [Getting Started](#getting-started-en)
  - [Prerequisites](#prerequisites-en)
  - [Configuration](#configuration-en)
  - [Local Launch](#local-launch-en)
  - [Running with Docker](#running-with-docker-en)
- [API Endpoints](#api-endpoints-en)
  - [Health Checks](#health-checks-en)
  - [Peer Management](#peer-management-en)
- [Swagger Documentation](#swagger-documentation-en)
- [Testing](#testing-en)
- [Future Plans / TODO](#future-plans--todo-en)
- [License](#license-en)

### <a name="key-features-en"></a>Key Features

*   **Server Key Management**: Automatically reads the server's WireGuard interface private key from its configuration file and derives the public key.
*   **Full CRUD for WireGuard Peers**:
    *   Creation of new peers with server-side automatic key generation (public and private).
    *   Retrieval of a list of all peers.
    *   Retrieval of a specific peer's configuration by its public key.
    *   Updating the list of allowed IP addresses (`AllowedIPs`) for a peer.
    *   Deletion of a peer.
*   **Configuration File Generation**:
    *   Creation of ready-to-use `.conf` files for clients (based on peer data and a client private key provided by the external calling application).
*   **Peer Key Rotation**: Automatic generation of a new key pair for an existing peer while preserving its essential settings (e.g., `AllowedIPs`).
*   **Health Checks**: `/healthz` (liveness) and `/readyz` (readiness) endpoints for integration with orchestration systems.
*   **Logging**: Structured logging using Zap, with support for different formats for development and production, and timestamps in MSK.
*   **Environment Variable Configuration**: Flexible setup via `.env` file or system environment variables.
*   **Docker Compatibility**: Ready-to-use `Dockerfile` and `docker-compose.yml` for easy deployment.
*   **Swagger Documentation**: Automatically generated API documentation available via `/swagger/index.html`.

### <a name="tech-stack-en"></a>Tech Stack

*   **Language**: Go (version 1.24+)
*   **Web Framework**: Gin
*   **Logging**: Zap
*   **WireGuard Interaction**: Via the `wg` command-line utility (`wireguard-tools`)
*   **Configuration**: godotenv (for `.env` files)
*   **API Documentation**: Swaggo (Swagger)
*   **Containerization**: Docker, Docker Compose

### <a name="getting-started-en"></a>Getting Started

#### <a name="prerequisites-en"></a>Prerequisites

*   Go (version 1.24 or higher)
*   `wireguard-tools` utilities (must be installed and available in `PATH` on the machine running the API, or within the Docker container)
*   Docker and Docker Compose (for running in a container)
*   Git

#### <a name="configuration-en"></a>Configuration

The application is configured via environment variables. For local development, you can create a `.env` file in the project root.

**Key Environment Variables:**

| Variable                | Description                                                                 | Example Value               | Required    |
| ----------------------- | ------------------------------------------------------------------------- | --------------------------- | ----------- |
| `APP_ENV`               | Application environment (`development`, `production`, `test`)              | `development`               | No (dev)    |
| `PORT`                  | Port the API will listen on                                                 | `8080`                      | No (8080)   |
| `WG_INTERFACE`          | Name of the server's WireGuard interface (e.g., `wg0`)                      | `wg0`                       | Yes         |
| `WG_CONFIG_PATH`        | Path to the server's WireGuard configuration file (e.g., `wg0.conf`)          | `/etc/wireguard/wg0.conf`   | Yes         |
| `SERVER_ENDPOINT`       | External `host:port` of the server for client `.conf` files                 | `your.vpn.server.com:51820` | No          |
| `WG_CMD_TIMEOUT_SECONDS`| Timeout (in seconds) for `wg` command execution                             | `5`                         | No (5)      |
| `KEY_GEN_TIMEOUT_SECONDS`| Timeout (in seconds) for key generation (`wg genkey`, `wg pubkey`)        | `5`                         | No (5)      |

**Example `.env` file:**
```dotenv
APP_ENV=development
PORT=8080
WG_INTERFACE=wg0
WG_CONFIG_PATH=/etc/wireguard/wg0.conf # Or /app/wg0.conf if copied in Dockerfile
SERVER_ENDPOINT=myvpn.example.com:51820
WG_CMD_TIMEOUT_SECONDS=5
KEY_GEN_TIMEOUT_SECONDS=5
```

**WireGuard Configuration File (`wg0.conf`):**
The application expects a WireGuard configuration file at the path specified by `WG_CONFIG_PATH`. This file must contain at least an `[Interface]` section with a valid `PrivateKey` field for the server interface. The API will read this key and add/modify `[Peer]` sections in this file.

Example of a minimal `wg0.conf` (for startup only, peers will be added via API):
```ini
[Interface]
Address = 10.0.0.1/24
ListenPort = 51820
PrivateKey = <YOUR_WG_SERVER_PRIVATE_KEY>
```

#### <a name="local-launch-en"></a>Local Launch (for development)

1.  Ensure Go and `wireguard-tools` are installed.
2.  Create and configure the `.env` file.
3.  Create the `wg0.conf` file at the path specified by `WG_CONFIG_PATH` in `.env`.
4.  Fetch dependencies: `go mod tidy && go mod download`
5.  Build and run:
    ```bash
    go run ./cmd/wg-api/main.go
    ```
    Or:
    ```bash
    go build -o wg-api ./cmd/wg-api/main.go
    ./wg-api
    ```

#### <a name="running-with-docker-en"></a>Running with Docker

1.  Ensure Docker and Docker Compose are installed.
2.  Create the `.env` file (see above).
3.  **To mount `wg0.conf` from the host (recommended for flexibility):**
    *   Ensure `wg0.conf` exists on the host machine (e.g., `/etc/wireguard/wg0.conf` or `./wg0.conf` for local testing).
    *   In `Dockerfile`, the `COPY wg0.conf ...` line should be removed or commented out.
    *   In `docker-compose.yml`, configure the `wg0.conf` mount in the `volumes` section (e.g., `- /etc/wireguard/wg0.conf:/etc/wireguard/wg0.conf`). Ensure the destination path in the container matches `WG_CONFIG_PATH` from `.env`. **Remove `:ro` (read-only) if the API needs to modify this file.**
4.  Build and run:
    ```bash
    docker-compose build
    docker-compose up -d
    ```
5.  View logs:
    ```bash
    docker-compose logs -f wg-api
    ```

### <a name="api-endpoints-en"></a>API Endpoints

The full API specification is available via Swagger UI at `/swagger/index.html` after starting the application.

#### <a name="health-checks-en"></a>Health Checks

*   **`GET /healthz`**: Liveness probe. Returns `200 OK` with status `"ok"` if the application is running.
*   **`GET /readyz`**: Readiness probe. Returns `200 OK` with status `"ready"` if the application is ready to process requests (including `wg` utility accessibility). Returns `503 Service Unavailable` in case of issues.

#### <a name="peer-management-en"></a>Peer Management

*   **`GET /configs`**: Get a list of all peer configurations.
    *   Response: `200 OK` with an array of `domain.Config`.
*   **`POST /configs`**: Create a new peer. The server generates keys for it.
    *   Request Body: `domain.CreatePeerRequest` (`allowed_ips`, optional `preshared_key`, `persistent_keepalive`).
    *   Response: `201 Created` with the new peer's `domain.Config`, **including `privateKey`**. The client must store this `privateKey`.
*   **`GET /configs/{publicKey}`**: Get the configuration of a specific peer.
    *   Response: `200 OK` with `domain.Config`.
*   **`PUT /configs/{publicKey}/allowed-ips`**: Update `AllowedIPs` for a peer.
    *   Request Body: `domain.AllowedIpsUpdate`.
    *   Response: `200 OK` (or `204 No Content`).
*   **`DELETE /configs/{publicKey}`**: Delete a peer.
    *   Response: `204 No Content`.
*   **`POST /configs/{publicKey}/rotate`**: Rotate keys for an existing peer. The server generates a new key pair.
    *   Response: `200 OK` with the peer's `domain.Config` containing new keys, **including the new `privateKey`**. The client must store this `privateKey`.
*   **`POST /configs/client-file`**: Generate a `.conf` file for a client.
    *   Request Body: `domain.ClientFileRequest` (contains `client_public_key` and `client_private_key` provided by the external application).
    *   Response: `200 OK` with the `.conf` file content as `text/plain`. The API does not store the provided `client_private_key`.

### <a name="swagger-documentation-en"></a>Swagger Documentation

After starting the application, interactive Swagger UI documentation is available at:
`http://localhost:<PORT>/swagger/index.html`
(replace `<PORT>` with the configured port, default is `8080`).

### <a name="testing-en"></a>Testing

To run unit tests:
```bash
go test ./... -v
```
For integration tests (require `wg` utility availability and proper setup):
```bash
go test ./internal/server/... -v
```
(Before running integration tests, ensure environment variables and a test `wg0.conf` are configured as described in `internal/server/integration_test.go`).

### <a name="future-plans--todo-en"></a>Future Plans / TODO

*   [ ] More granular access control (if needed).
*   [ ] Enhanced input validation (key formats, CIDRs, etc.).
*   [ ] Option for persistent storage of client private keys (generated by the server) if re-issuance without client involvement is required (e.g., Vault or encrypted DB).
*   [ ] Add more unit and integration tests for full coverage.
*   [ ] Set up CI/CD for automated building, testing, and deployment.
