#!/bin/sh
# Выход при любой ошибке
set -e

echo "Entrypoint script started..."

# Переменные окружения должны быть доступны из --env-file .env
# Проверим наличие ключевых переменных
if [ -z "$WG_INTERFACE" ]; then
  echo "Error: WG_INTERFACE environment variable is not set."
  exit 1
fi
if [ -z "$SERVER_PRIVATE_KEY" ]; then
  echo "Error: SERVER_PRIVATE_KEY environment variable is not set."
  exit 1
fi
if [ -z "$SERVER_INTERFACE_ADDRESSES" ]; then
  echo "Error: SERVER_INTERFACE_ADDRESSES environment variable is not set."
  exit 1
fi
if [ -z "$SERVER_LISTEN_PORT" ]; then
  echo "Error: SERVER_LISTEN_PORT environment variable is not set."
  exit 1
fi

echo "Configuring WireGuard interface: $WG_INTERFACE"

# 1. Создаем интерфейс, если его нет
if ! ip link show "$WG_INTERFACE" > /dev/null 2>&1; then
  echo "Creating WireGuard interface: $WG_INTERFACE"
  ip link add dev "$WG_INTERFACE" type wireguard
else
  echo "WireGuard interface $WG_INTERFACE already exists. Reconfiguring."
  # Если интерфейс уже существует, его нужно сначала "потушить" перед setconf,
  # чтобы избежать некоторых проблем с применением новой конфигурации.
  ip link set down dev "$WG_INTERFACE"
fi

# 2. Применяем базовую конфигурацию интерфейса (приватный ключ, порт)
# wg setconf требует, чтобы интерфейс был UP для некоторых операций, но не для setconf.
# Однако, после setconf он может "потухнуть", если не было ListenPort.
echo "Setting WireGuard configuration for $WG_INTERFACE (PrivateKey, ListenPort)"
echo "[Interface]
PrivateKey = $SERVER_PRIVATE_KEY
ListenPort = $SERVER_LISTEN_PORT
" | wg setconf "$WG_INTERFACE" /dev/stdin

# 3. Поднимаем интерфейс
echo "Bringing up interface $WG_INTERFACE"
ip link set up dev "$WG_INTERFACE"

# 4. Назначаем IP-адреса
# SERVER_INTERFACE_ADDRESSES может быть списком через запятую "10.0.0.1/24,fd00::1/64"
# Мы должны назначить каждый адрес.
echo "Assigning IP addresses to $WG_INTERFACE: $SERVER_INTERFACE_ADDRESSES"
# IFS - Internal Field Separator
OLD_IFS="$IFS" # Сохраняем старый разделитель
IFS=','        # Устанавливаем запятую как разделитель
for addr in $SERVER_INTERFACE_ADDRESSES; do
  # Убираем возможные пробелы вокруг адреса
  clean_addr=$(echo "$addr" | xargs) # xargs уберет начальные/конечные пробелы
  echo "Adding address: $clean_addr"
  ip address add "$clean_addr" dev "$WG_INTERFACE"
done
IFS="$OLD_IFS" # Восстанавливаем старый разделитель

# 5. Опционально: PostUp правила (если они нужны)
# Пример: iptables -A FORWARD -i $WG_INTERFACE -o eth0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
# Важно: eth0 может называться иначе в Alpine (например, eth0, если сеть bridge, или по-другому).
# Для простоты интеграционных тестов, где API работает внутри контейнера,
# сложные правила маршрутизации могут быть не нужны, если тесты обращаются к localhost.
# Если PostUp правила важны для твоего API, их нужно добавить сюда.

echo "WireGuard interface $WG_INTERFACE configured and up."
echo "Current $WG_INTERFACE configuration:"
wg show "$WG_INTERFACE"
echo "IP addresses for $WG_INTERFACE:"
ip addr show dev "$WG_INTERFACE"

echo "Starting API server: $@"
# exec "$@" запускает команду, переданную в CMD Dockerfile (т.е. /app/wg-api)
echo "--- Environment for Go app ---"
env
echo "------------------------------"
exec "$@"
exec "$@"
