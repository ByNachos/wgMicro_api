#!/bin/sh
# Enhanced WireGuard VPN server entrypoint with auto-configuration
set -e

echo "WireGuard VPN Server Entrypoint started..."

# --- Auto-detect external IP ---
detect_external_ip() {
    local ip=""
    
    # Try multiple services for reliability
    for service in "ifconfig.me" "ipinfo.io/ip" "icanhazip.com" "ipecho.net/plain"; do
        echo "Trying to detect external IP via $service..."
        ip=$(wget -qO- --timeout=10 "http://$service" 2>/dev/null | tr -d '\n\r' || true)
        
        # Validate IP format
        if echo "$ip" | grep -qE '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$'; then
            echo "Detected external IP: $ip"
            echo "$ip"
            return 0
        fi
    done
    
    echo "Failed to auto-detect external IP. Please set SERVER_ENDPOINT_HOST manually."
    return 1
}

# --- Auto-generate server keys if not provided ---
generate_server_keys() {
    echo "SERVER_PRIVATE_KEY not provided. Generating new server key pair..."
    
    local private_key=$(wg genkey)
    local public_key=$(echo "$private_key" | wg pubkey)
    
    export SERVER_PRIVATE_KEY="$private_key"
    export SERVER_PUBLIC_KEY="$public_key"
    
    echo "Generated server keys:"
    echo "  Public Key: $SERVER_PUBLIC_KEY"
    echo "  Private Key: [HIDDEN FOR SECURITY]"
    echo ""
    echo "IMPORTANT: Save these keys! Add this to your .env file:"
    echo "SERVER_PRIVATE_KEY=$SERVER_PRIVATE_KEY"
    echo "SERVER_PUBLIC_KEY=$SERVER_PUBLIC_KEY"
    echo ""
}

# --- Enable IP forwarding ---
enable_ip_forwarding() {
    echo "Enabling IP forwarding..."
    echo 1 > /proc/sys/net/ipv4/ip_forward
    echo 1 > /proc/sys/net/ipv6/conf/all/forwarding
    
    # Make it persistent (if possible in container)
    echo "net.ipv4.ip_forward=1" >> /etc/sysctl.conf 2>/dev/null || true
    echo "net.ipv6.conf.all.forwarding=1" >> /etc/sysctl.conf 2>/dev/null || true
}

# --- Setup iptables rules for NAT and forwarding ---
setup_iptables_rules() {
    local wg_interface="$1"
    local server_port="$2"
    
    echo "Setting up iptables rules for VPN traffic routing..."
    
    # Clear existing rules
    iptables -F || true
    iptables -t nat -F || true
    
    # Allow established and related connections
    iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
    
    # Allow loopback
    iptables -A INPUT -i lo -j ACCEPT
    
    # Allow WireGuard port
    iptables -A INPUT -p udp --dport "$server_port" -j ACCEPT
    
    # Allow HTTP API port
    iptables -A INPUT -p tcp --dport 8080 -j ACCEPT
    
    # Allow SSH (if needed for maintenance)
    iptables -A INPUT -p tcp --dport 22 -j ACCEPT || true
    
    # Allow WireGuard interface traffic
    iptables -A INPUT -i "$wg_interface" -j ACCEPT
    iptables -A FORWARD -i "$wg_interface" -j ACCEPT
    iptables -A FORWARD -o "$wg_interface" -j ACCEPT
    
    # NAT rules for internet access through VPN
    # Get the default route interface (typically eth0 in Docker)
    local default_iface=$(ip route | grep '^default' | awk '{print $5}' | head -1)
    
    if [ -n "$default_iface" ]; then
        echo "Setting up NAT rules for interface: $default_iface"
        iptables -t nat -A POSTROUTING -s 10.0.0.0/8 -o "$default_iface" -j MASQUERADE
        iptables -t nat -A POSTROUTING -s 172.16.0.0/12 -o "$default_iface" -j MASQUERADE
        iptables -t nat -A POSTROUTING -s 192.168.0.0/16 -o "$default_iface" -j MASQUERADE
    fi
    
    # Default policies
    iptables -P INPUT DROP
    iptables -P FORWARD ACCEPT
    iptables -P OUTPUT ACCEPT
    
    echo "iptables rules configured successfully"
}

# --- Set default values ---
WG_INTERFACE="${WG_INTERFACE:-wg0}"
SERVER_LISTEN_PORT="${SERVER_LISTEN_PORT:-51820}"
SERVER_INTERFACE_ADDRESSES="${SERVER_INTERFACE_ADDRESSES:-10.0.0.1/24}"
SERVER_ENDPOINT_PORT="${SERVER_ENDPOINT_PORT:-${SERVER_LISTEN_PORT}}"

# --- Auto-detect external IP if not provided ---
if [ -z "$SERVER_ENDPOINT_HOST" ]; then
    echo "SERVER_ENDPOINT_HOST not set. Attempting to auto-detect..."
    if DETECTED_IP=$(detect_external_ip); then
        export SERVER_ENDPOINT_HOST="$DETECTED_IP"
        echo "Using auto-detected IP: $SERVER_ENDPOINT_HOST"
    else
        echo "Warning: Could not auto-detect external IP. VPN clients may not connect properly."
        export SERVER_ENDPOINT_HOST="127.0.0.1"
    fi
fi

# --- Generate keys if not provided ---
if [ -z "$SERVER_PRIVATE_KEY" ]; then
    generate_server_keys
else
    # Calculate public key from provided private key
    export SERVER_PUBLIC_KEY=$(echo "$SERVER_PRIVATE_KEY" | wg pubkey)
    echo "Using provided server private key"
    echo "Calculated server public key: $SERVER_PUBLIC_KEY"
fi

# --- Enable IP forwarding ---
enable_ip_forwarding

# --- Configure WireGuard interface ---
echo "Configuring WireGuard interface: $WG_INTERFACE"

# Remove existing interface if it exists
if ip link show "$WG_INTERFACE" > /dev/null 2>&1; then
    echo "Removing existing WireGuard interface..."
    ip link delete "$WG_INTERFACE" 2>/dev/null || true
fi

# Create new WireGuard interface
echo "Creating WireGuard interface..."
ip link add dev "$WG_INTERFACE" type wireguard

# Configure WireGuard
echo "Configuring WireGuard parameters..."
echo "[Interface]
PrivateKey = $SERVER_PRIVATE_KEY
ListenPort = $SERVER_LISTEN_PORT
" | wg setconf "$WG_INTERFACE" /dev/stdin

# Set MTU if specified
if [ -n "$CLIENT_CONFIG_MTU" ] && [ "$CLIENT_CONFIG_MTU" -gt 0 ]; then
    echo "Setting MTU to $CLIENT_CONFIG_MTU"
    ip link set dev "$WG_INTERFACE" mtu "$CLIENT_CONFIG_MTU"
fi

# Assign IP addresses
echo "Assigning IP addresses: $SERVER_INTERFACE_ADDRESSES"
OLD_IFS="$IFS"
IFS=','
for addr_cidr in $SERVER_INTERFACE_ADDRESSES; do
    clean_addr_cidr=$(echo "$addr_cidr" | xargs)
    echo "Adding address: $clean_addr_cidr"
    ip address add "$clean_addr_cidr" dev "$WG_INTERFACE"
done
IFS="$OLD_IFS"

# Bring up the interface
echo "Bringing up WireGuard interface..."
ip link set up dev "$WG_INTERFACE"

# --- Setup iptables rules ---
setup_iptables_rules "$WG_INTERFACE" "$SERVER_LISTEN_PORT"

# --- Set environment variables for the Go application ---
export WG_ACTUAL_LISTEN_PORT="$SERVER_LISTEN_PORT"
export WG_ACTUAL_INTERFACE_ADDRESSES="$SERVER_INTERFACE_ADDRESSES"
export WG_ACTUAL_MTU="$CLIENT_CONFIG_MTU"

echo ""
echo "=== WireGuard VPN Server Configuration Complete ==="
echo "Interface: $WG_INTERFACE"
echo "Listen Port: $SERVER_LISTEN_PORT"
echo "Server Endpoint: $SERVER_ENDPOINT_HOST:$SERVER_ENDPOINT_PORT"
echo "Server Addresses: $SERVER_INTERFACE_ADDRESSES"
echo "Server Public Key: $SERVER_PUBLIC_KEY"
if [ -n "$CLIENT_CONFIG_MTU" ]; then
    echo "MTU: $CLIENT_CONFIG_MTU"
fi
echo ""
echo "WireGuard interface status:"
wg show "$WG_INTERFACE"
echo ""
echo "Interface IP addresses:"
ip addr show dev "$WG_INTERFACE"
echo ""
echo "iptables NAT rules:"
iptables -t nat -L -n
echo ""
echo "Starting API server..."
echo "=================================================="

# Start the Go application
exec "$@"