package domain

// Config описывает WireGuard-peer со всеми полями из “wg show dump”
type Config struct {
	PrivateKey string `json:"privateKey,omitempty"`
	// PublicKey – публичный ключ peer’а
	PublicKey string `json:"publicKey"`

	// PreSharedKey – опциональный pre-shared ключ
	PreSharedKey string `json:"preSharedKey,omitempty"`

	// Endpoint – адрес (IP:port) к которому peer коннектится
	Endpoint string `json:"endpoint,omitempty"`

	// AllowedIps – список разрешённых IP/Netmask для этого peer’а
	AllowedIps []string `json:"allowedIps"`

	// LatestHandshake – время последнего рукопожатия в UNIX-секундах
	LatestHandshake int64 `json:"latestHandshake,omitempty"`

	// ReceiveBytes – сколько байт получено
	ReceiveBytes uint64 `json:"receiveBytes,omitempty"`

	// TransmitBytes – сколько байт передано
	TransmitBytes uint64 `json:"transmitBytes,omitempty"`

	// PersistentKeepalive – интервал keepalive в секундах (0 или off)
	PersistentKeepalive int `json:"persistentKeepalive,omitempty"`
}

// AllowedIpsUpdate представляет запрос на замену списка allowedIps
type AllowedIpsUpdate struct {
	AllowedIps []string `json:"allowedIps" example:"10.0.0.2/32"`
}
