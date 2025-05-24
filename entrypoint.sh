#!/bin/sh
# Выход при любой ошибке
set -e

echo "Entrypoint script started..."

# --- Проверка базовых переменных из .env ---
if [ -z "$WG_INTERFACE" ]; then
  echo "Error: WG_INTERFACE environment variable is not set."
  exit 1
fi
if [ -z "$SERVER_PRIVATE_KEY" ]; then
  echo "Error: SERVER_PRIVATE_KEY environment variable is not set."
  exit 1
fi
# Эти переменные будут использоваться, если интерфейс НЕ существует или для сравнения
if [ -z "$SERVER_LISTEN_PORT" ]; then
  echo "Error: SERVER_LISTEN_PORT environment variable is not set (needed as fallback or for new interface)."
  exit 1
fi
if [ -z "$SERVER_INTERFACE_ADDRESSES" ]; then
  echo "Error: SERVER_INTERFACE_ADDRESSES environment variable is not set (needed as fallback or for new interface)."
  exit 1
fi
# CLIENT_CONFIG_MTU может быть не задан, config.go обработает это (DefaultClientConfigMTU)

# --- Логика определения актуальных параметров WG ---

# Эти переменные будут установлены либо из существующего интерфейса, либо из .env
export WG_ACTUAL_LISTEN_PORT=""
export WG_ACTUAL_INTERFACE_ADDRESSES=""
export WG_ACTUAL_MTU="" # "" будет означать, что Go использует свой default или значение из CLIENT_CONFIG_MTU

# Вычисляем ожидаемый публичный ключ из SERVER_PRIVATE_KEY (из .env)
EXPECTED_SERVER_PUBLIC_KEY=$(echo "$SERVER_PRIVATE_KEY" | wg pubkey)
if [ -z "$EXPECTED_SERVER_PUBLIC_KEY" ]; then
  echo "Error: Failed to derive public key from SERVER_PRIVATE_KEY."
  exit 1
fi
echo "Expected server public key (from .env SERVER_PRIVATE_KEY): $EXPECTED_SERVER_PUBLIC_KEY"

if ip link show "$WG_INTERFACE" > /dev/null 2>&1; then
  echo "WireGuard interface $WG_INTERFACE already exists. Checking its configuration..."
  CURRENT_SERVER_PUBLIC_KEY=$(wg show "$WG_INTERFACE" public-key 2>/dev/null || echo "not_found")

  if [ "$CURRENT_SERVER_PUBLIC_KEY" = "not_found" ]; then
    echo "Error: Could not retrieve public key from existing interface $WG_INTERFACE. It might be down or not a WireGuard interface."
    echo "Please ensure $WG_INTERFACE is properly configured or remove it to allow recreation."
    exit 1
  fi

  if [ "$CURRENT_SERVER_PUBLIC_KEY" != "$EXPECTED_SERVER_PUBLIC_KEY" ]; then
    echo "Error: Public key mismatch for $WG_INTERFACE!"
    echo "  Current public key on interface: $CURRENT_SERVER_PUBLIC_KEY"
    echo "  Expected public key (from .env): $EXPECTED_SERVER_PUBLIC_KEY"
    echo "Application cannot use this interface. Ensure SERVER_PRIVATE_KEY in .env matches the interface's key, or remove/reconfigure $WG_INTERFACE."
    exit 1
  fi

  echo "Public key on $WG_INTERFACE matches .env. Using existing interface's parameters."

  # Извлекаем ListenPort
  WG_ACTUAL_LISTEN_PORT=$(wg show "$WG_INTERFACE" listen-port 2>/dev/null || echo "$SERVER_LISTEN_PORT") # Фоллбэк на .env, если не удалось получить
  echo "Using Listen Port from interface: $WG_ACTUAL_LISTEN_PORT"

  # Извлекаем IP-адреса
  # Собираем IPv4 и IPv6 адреса в одну строку через запятую
  # Убираем /prefixlen и берем только сам IP
  V4_ADDRS=$(ip -4 addr show dev "$WG_INTERFACE" | grep -oP 'inet \K[\d.]+' | tr '\n' ',' | sed 's/,$//')
  V6_ADDRS=$(ip -6 addr show dev "$WG_INTERFACE" | grep -oP 'inet6 \K[0-9a-fA-F:]+' | grep -v '^fe80' | tr '\n' ',' | sed 's/,$//') # Исключаем link-local

  if [ -n "$V4_ADDRS" ] && [ -n "$V6_ADDRS" ]; then
    WG_ACTUAL_INTERFACE_ADDRESSES="${V4_ADDRS},${V6_ADDRS}"
  elif [ -n "$V4_ADDRS" ]; then
    WG_ACTUAL_INTERFACE_ADDRESSES="$V4_ADDRS"
  elif [ -n "$V6_ADDRS" ]; then
    WG_ACTUAL_INTERFACE_ADDRESSES="$V6_ADDRS"
  else
    echo "Warning: No IP addresses found on existing interface $WG_INTERFACE. Falling back to SERVER_INTERFACE_ADDRESSES from .env."
    WG_ACTUAL_INTERFACE_ADDRESSES="$SERVER_INTERFACE_ADDRESSES" # Фоллбэк
  fi
  # Примечание: Go-приложение ожидает адреса с префиксами (CIDR).
  # Команды `ip addr show` показывают их. Нам нужно сохранить их.
  # Исправленная логика для извлечения IP с CIDR:
  ADDR_LIST=$(ip addr show dev "$WG_INTERFACE" | grep -oP 'inet6? \K[\da-fA-F.:/]+' | grep -v '^fe80' | tr '\n' ',' | sed 's/,$//')
  if [ -n "$ADDR_LIST" ]; then
      WG_ACTUAL_INTERFACE_ADDRESSES="$ADDR_LIST"
      echo "Using IP Addresses from interface: $WG_ACTUAL_INTERFACE_ADDRESSES"
  else
      echo "Warning: No IP addresses found on existing interface $WG_INTERFACE. Falling back to SERVER_INTERFACE_ADDRESSES from .env."
      WG_ACTUAL_INTERFACE_ADDRESSES="$SERVER_INTERFACE_ADDRESSES"
  fi


  # Извлекаем MTU
  CURRENT_MTU=$(ip link show "$WG_INTERFACE" | grep -oP 'mtu \K\d+' || echo "")
  if [ -n "$CURRENT_MTU" ]; then
    WG_ACTUAL_MTU="$CURRENT_MTU"
    echo "Using MTU from interface: $WG_ACTUAL_MTU"
  else
    echo "Warning: Could not retrieve MTU from existing interface $WG_INTERFACE. Go app will use CLIENT_CONFIG_MTU from .env or default."
    # WG_ACTUAL_MTU остается пустым, Go-приложение возьмет из CLIENT_CONFIG_MTU
  fi

  # Убедимся, что интерфейс поднят (на случай, если он был down)
  echo "Ensuring interface $WG_INTERFACE is up."
  ip link set up dev "$WG_INTERFACE" # Эта команда безопасна, если интерфейс уже поднят

else
  echo "WireGuard interface $WG_INTERFACE does not exist. Creating and configuring from .env settings..."
  ip link add dev "$WG_INTERFACE" type wireguard

  echo "Applying WireGuard configuration (PrivateKey, ListenPort) via setconf"
  echo "[Interface]
PrivateKey = $SERVER_PRIVATE_KEY
ListenPort = $SERVER_LISTEN_PORT
" | wg setconf "$WG_INTERFACE" /dev/stdin

  # Установка MTU для нового интерфейса из .env (если CLIENT_CONFIG_MTU задан и > 0)
  # Переменная CLIENT_CONFIG_MTU должна быть доступна здесь из .env файла Docker
  if [ -n "$CLIENT_CONFIG_MTU" ] && [ "$CLIENT_CONFIG_MTU" -gt 0 ]; then
    echo "Setting MTU for new interface $WG_INTERFACE to $CLIENT_CONFIG_MTU (from .env CLIENT_CONFIG_MTU)"
    ip link set dev "$WG_INTERFACE" mtu "$CLIENT_CONFIG_MTU"
    export WG_ACTUAL_MTU="$CLIENT_CONFIG_MTU"
  else
    echo "CLIENT_CONFIG_MTU not set or is 0 in .env. MTU for new interface $WG_INTERFACE will use system default."
    # WG_ACTUAL_MTU остается пустым, Go-приложение возьмет свой default 0
  fi

  echo "Assigning IP addresses from .env: $SERVER_INTERFACE_ADDRESSES"
  OLD_IFS="$IFS"
  IFS=','
  for addr_cidr in $SERVER_INTERFACE_ADDRESSES; do
    clean_addr_cidr=$(echo "$addr_cidr" | xargs)
    echo "Adding address: $clean_addr_cidr"
    ip address add "$clean_addr_cidr" dev "$WG_INTERFACE"
  done
  IFS="$OLD_IFS"

  echo "Bringing up interface $WG_INTERFACE"
  ip link set up dev "$WG_INTERFACE"

  # Для случая создания интерфейса, "актуальные" значения - это те, что из .env
  export WG_ACTUAL_LISTEN_PORT="$SERVER_LISTEN_PORT"
  export WG_ACTUAL_INTERFACE_ADDRESSES="$SERVER_INTERFACE_ADDRESSES"
  # WG_ACTUAL_MTU уже установлен выше, если CLIENT_CONFIG_MTU был задан
fi

echo "--- Final effective WireGuard server parameters for Go application ---"
echo "WG_ACTUAL_LISTEN_PORT: $WG_ACTUAL_LISTEN_PORT"
echo "WG_ACTUAL_INTERFACE_ADDRESSES: $WG_ACTUAL_INTERFACE_ADDRESSES"
echo "WG_ACTUAL_MTU: $WG_ACTUAL_MTU"
echo "SERVER_PRIVATE_KEY: (from .env - not echoed)"
echo "SERVER_PUBLIC_KEY (derived): $EXPECTED_SERVER_PUBLIC_KEY" # Этот ключ будет использоваться Go-приложением
echo "SERVER_ENDPOINT_HOST: $SERVER_ENDPOINT_HOST (from .env)"
echo "SERVER_ENDPOINT_PORT: $SERVER_ENDPOINT_PORT (from .env)"
echo "WG_INTERFACE: $WG_INTERFACE (from .env)"
echo "--------------------------------------------------------------------"


echo "Current $WG_INTERFACE state (after entrypoint.sh script):"
wg show "$WG_INTERFACE"
echo "IP addresses for $WG_INTERFACE (after entrypoint.sh script):"
ip addr show dev "$WG_INTERFACE"
echo "MTU for $WG_INTERFACE (after entrypoint.sh script):"
ip link show "$WG_INTERFACE" | grep -o 'mtu [0-9]*'

echo "Starting API server: $@"
# Передаем все переменные окружения (включая наши WG_ACTUAL_*) Go-приложению
exec "$@"
