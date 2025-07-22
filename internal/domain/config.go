package domain

// Config describes a WireGuard peer configuration, mirroring fields from "wg show <interface> dump"
// and including the client's private key for config generation purposes.
type Config struct {
	// PrivateKey is the client's private key. This is NOT part of 'wg show dump' output
	// but is essential for generating a client .conf file.
	// It's populated by the service when new keys are generated (e.g., during Rotate)
	// or expected from the user if they manage keys externally and provide it for config generation.
	// omitempty is used so it's not shown in API responses unless explicitly set (e.g. on Rotate response).
	PrivateKey string `json:"privateKey,omitempty"`

	// PublicKey is the peer's public key. This is a mandatory field for identifying a peer.
	// Example: "a1b2c3d4...+Z0="
	PublicKey string `json:"publicKey"` // Mandatory

	// PreSharedKey is an optional pre-shared key for an extra layer of security.
	// If "(none)" is shown by 'wg show dump', this will be an empty string.
	// omitempty is used as it's optional.
	// Example: "s1t2u3v4...+Y9="
	PreSharedKey string `json:"preSharedKey,omitempty"`

	// Endpoint is the remote IP address and port to which this peer connects (if this config represents a client)
	// or the public IP and port of this peer (if this config represents a remote peer from server's perspective).
	// If "(none)" is shown by 'wg show dump', this will be an empty string.
	// omitempty is used as it might not always be set or known.
	// Example: "192.0.2.1:51820"
	Endpoint string `json:"endpoint,omitempty"`

	// AllowedIps is a list of IP networks (CIDR notation) from which this peer is allowed to send traffic
	// and to which traffic will be routed through this peer.
	// Example: ["10.0.0.2/32", "192.168.2.0/24"]
	AllowedIps []string `json:"allowedIps"` // Can be empty if no IPs are allowed (though unusual for a functional peer)

	// LatestHandshake is the timestamp (UNIX seconds) of the most recent handshake with this peer.
	// A value of 0 indicates no handshake has occurred.
	// omitempty is used as it's state information.
	LatestHandshake int64 `json:"latestHandshake,omitempty"`

	// ReceiveBytes is the total number of bytes received from this peer.
	// omitempty is used as it's state information.
	ReceiveBytes uint64 `json:"receiveBytes,omitempty"`

	// TransmitBytes is the total number of bytes transmitted to this peer.
	// omitempty is used as it's state information.
	TransmitBytes uint64 `json:"transmitBytes,omitempty"`

	// PersistentKeepalive is the interval in seconds for sending keepalive packets to the peer.
	// "off" from 'wg show dump' is represented as 0.
	// omitempty is used as it might not be set.
	// Example: 25
	PersistentKeepalive int `json:"persistentKeepalive,omitempty"`
}

// AllowedIpsUpdate represents the request body for updating a peer's allowed IPs.
type AllowedIpsUpdate struct {
	// AllowedIps is the new list of IP networks (CIDR notation) to set for the peer.
	// This will replace the existing list. An empty list might remove all allowed IPs.
	// Example: ["10.0.0.3/32"]
	AllowedIps []string `json:"allowedIps" example:"10.0.0.2/32"` // example tag for swagger
}

// ClientFileRequest represents the request body for generating a client's .conf file.
// It requires the client's public key (to identify the peer on the server)
// and the client's private key (to be included in the generated .conf file).
type ClientFileRequest struct {
	ClientPublicKey  string `json:"client_public_key" binding:"required"`  // Client's public key, base64 encoded
	ClientPrivateKey string `json:"client_private_key" binding:"required"` // Client's private key, base64 encoded
}

// CreatePeerRequest represents the request body for creating a new peer
// where the server generates the cryptographic keys.
type CreatePeerRequest struct {
	// AllowedIps is a list of IP networks (CIDR notation) for the new peer. Can be empty.
	AllowedIps []string `json:"allowed_ips"`
	// PreSharedKey is an optional pre-shared key for the new peer.
	PreSharedKey string `json:"preshared_key,omitempty"`
	// PersistentKeepalive is an optional interval in seconds for keepalive packets.
	PersistentKeepalive int `json:"persistent_keepalive,omitempty"`
	// You_can_add_other_fields_here_if_needed_by_client_for_new_peer_creation,
	// e.g., a friendly name or description for the peer, though these are not standard WG fields.
}

// GetConfigRequest represents the request body for getting a peer configuration by public key.
type GetConfigRequest struct {
	// PublicKey is the peer's public key to retrieve configuration for.
	PublicKey string `json:"public_key" binding:"required"`
}

// DeleteConfigRequest represents the request body for deleting a peer configuration.
type DeleteConfigRequest struct {
	// PublicKey is the peer's public key to delete.
	PublicKey string `json:"public_key" binding:"required"`
}

// RotatePeerRequest represents the request body for rotating a peer's keys.
type RotatePeerRequest struct {
	// PublicKey is the peer's current public key to rotate.
	PublicKey string `json:"public_key" binding:"required"`
}

// UpdateAllowedIpsRequest represents the request body for updating a peer's allowed IPs.
type UpdateAllowedIpsRequest struct {
	// PublicKey is the peer's public key to update.
	PublicKey string `json:"public_key" binding:"required"`
	// AllowedIps is the new list of IP networks (CIDR notation) to set for the peer.
	// This will replace the existing list. An empty list might remove all allowed IPs.
	AllowedIps []string `json:"allowed_ips"`
}
