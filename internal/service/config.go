package service

import (
	"context"
	"errors" // Imported for errors.Is and errors.As
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository" // Now used for repository.ErrWgTimeout and repository.ErrPeerNotFound
)

// DefaultKeyGenTimeout defines the default timeout for 'wg genkey' and 'wg pubkey' commands
// if a valid timeout is not provided during ConfigService initialization.
const DefaultKeyGenTimeout = 5 * time.Second

// ConfigService encapsulates the business logic for managing WireGuard peer configurations.
// It acts as an intermediary between handlers (HTTP layer) and the repository (data access layer).
type ConfigService struct {
	repo        repository.Repo
	ifacePubKey string // Public key of the server's WireGuard interface (obtained from ServerKeyManager)
	// ifacePrivKey      string // REMOVED - Server's private key is managed by ServerKeyManager and not directly needed by service methods here
	serverEndpoint       string
	keyGenerationTimeout time.Duration // Timeout for 'wg genkey' for *client* keys
	// clientKeyStore    *InMemoryClientKeyStore // Будет добавлено в Шаге 3.3
}

// NewConfigService creates a new instance of ConfigService.
// Parameters:
//
//	repo: An implementation of the repository.Repo interface.
//	serverPublicKey: The public key of this WireGuard server's interface.
//	serverEndpoint: The public-facing endpoint (e.g., "vpn.example.com:51820") for clients.
//	clientKeyGenTimeout: The timeout for cryptographic key generation commands (for client keys).
func NewConfigService(
	repo repository.Repo,
	serverPublicKey string, // Changed from ifacePubKey, and no more ifacePrivKey
	serverEndpoint string,
	clientKeyGenTimeout time.Duration, // Renamed for clarity, was keyGenTimeout
) *ConfigService {
	if repo == nil {
		logger.Logger.Fatal("Repository cannot be nil for ConfigService")
	}
	if serverPublicKey == "" {
		logger.Logger.Fatal("Server public key is empty in ConfigService initialization.")
	}

	if clientKeyGenTimeout <= 0 {
		logger.Logger.Warn("Provided client key generation timeout is invalid, using default",
			zap.Duration("providedTimeout", clientKeyGenTimeout),
			zap.Duration("defaultTimeout", DefaultKeyGenTimeout))
		clientKeyGenTimeout = DefaultKeyGenTimeout
	}

	// Store serverPublicKey as ifacePubKey internally as before, or rename field in struct
	// For consistency with existing field name in ConfigService:
	s := &ConfigService{
		repo:        repo,
		ifacePubKey: serverPublicKey, // This field in ConfigService now holds server's public key
		// ifacePrivKey:      "", // Server's private key no longer passed to/stored by service directly
		serverEndpoint:       serverEndpoint,
		keyGenerationTimeout: clientKeyGenTimeout, // This is for 'wg genkey' for client keys
	}

	logger.Logger.Info("ConfigService initialized",
		zap.String("serverPublicKeyFirstChars", s.ifacePubKey[:min(10, len(s.ifacePubKey))]+"..."),
		zap.Bool("serverEndpointSet", s.serverEndpoint != ""),
		zap.Duration("clientKeyGenTimeout", s.keyGenerationTimeout),
	)
	return s
}

// min is a helper function to find the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetAll retrieves all peer configurations from the repository.
// Returns a slice of domain.Config and an error if the repository fails.
func (s *ConfigService) GetAll() ([]domain.Config, error) {
	configs, err := s.repo.ListConfigs()
	if err != nil {
		logger.Logger.Error("Service: Failed to get all configs from repository", zap.Error(err))
		// Propagate the error; the handler will decide on the HTTP response.
		// Could wrap error here if service layer adds more context, e.g.,
		// return nil, fmt.Errorf("failed to retrieve peer list: %w", err)
		return nil, err
	}
	logger.Logger.Debug("Service: Successfully retrieved all configs", zap.Int("count", len(configs)))
	return configs, nil
}

// Get retrieves a single peer configuration by its public key.
// Returns the domain.Config if found, or an error.
// Specifically, it can return repository.ErrPeerNotFound if the peer doesn't exist.
func (s *ConfigService) Get(publicKey string) (*domain.Config, error) {
	if publicKey == "" {
		logger.Logger.Warn("Service: Get config called with empty public key")
		return nil, errors.New("public key cannot be empty for Get operation") // Or a domain-specific validation error
	}
	config, err := s.repo.GetConfig(publicKey)
	if err != nil {
		// Log based on the type of error from the repository
		if errors.Is(err, repository.ErrPeerNotFound) {
			logger.Logger.Info("Service: Peer not found in repository", zap.String("publicKey", publicKey))
		} else {
			logger.Logger.Error("Service: Failed to get config by public key from repository", zap.String("publicKey", publicKey), zap.Error(err))
		}
		return nil, err // Propagate error (could be ErrPeerNotFound or other repo error)
	}
	logger.Logger.Debug("Service: Successfully retrieved config by public key", zap.String("publicKey", publicKey))
	return config, nil
}

// Create persists a new peer configuration using the repository.
// cfg: The peer configuration to create. PublicKey and AllowedIps are typically required.
func (s *ConfigService) Create(cfg domain.Config) error {
	// Service layer can perform validation before calling repository.
	if cfg.PublicKey == "" {
		logger.Logger.Warn("Service: Create config called with empty public key")
		return errors.New("public key is required for creating a peer")
	}
	// Add more validation as needed (e.g., format of PublicKey, CIDRs in AllowedIps).

	err := s.repo.CreateConfig(cfg)
	if err != nil {
		logger.Logger.Error("Service: Failed to create config in repository", zap.String("publicKey", cfg.PublicKey), zap.Error(err))
		return err // Propagate error
	}
	logger.Logger.Info("Service: Successfully created config", zap.String("publicKey", cfg.PublicKey))
	return nil
}

// UpdateAllowedIPs updates the 'allowed_ips' field for a peer identified by its public key.
// publicKey: The public key of the peer to update.
// ips: A slice of strings representing the new allowed IP networks in CIDR format.
//
//	An empty slice typically means removing all allowed IPs.
func (s *ConfigService) UpdateAllowedIPs(publicKey string, ips []string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: UpdateAllowedIPs called with empty public key")
		return errors.New("public key is required for updating allowed IPs")
	}
	// Further validation for IP CIDR formats can be added here.

	err := s.repo.UpdateAllowedIPs(publicKey, ips)
	if err != nil {
		logger.Logger.Error("Service: Failed to update allowed IPs in repository",
			zap.String("publicKey", publicKey),
			zap.Strings("newIPs", ips), // Log the IPs being set
			zap.Error(err))
		return err // Propagate error
	}
	logger.Logger.Info("Service: Successfully updated allowed IPs", zap.String("publicKey", publicKey), zap.Strings("newIPs", ips))
	return nil
}

// Delete removes a peer configuration identified by its public key from the repository.
func (s *ConfigService) Delete(publicKey string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: Delete config called with empty public key")
		return errors.New("public key is required for deleting a peer")
	}

	// Before deleting, one might want to check if the peer exists to return a more specific
	// error or status, but 'wg set ... remove' often doesn't fail for non-existent peers.
	// The repository's DeleteConfig might not return ErrPeerNotFound unless explicitly implemented.
	err := s.repo.DeleteConfig(publicKey)
	if err != nil {
		logger.Logger.Error("Service: Failed to delete config in repository", zap.String("publicKey", publicKey), zap.Error(err))
		return err // Propagate error
	}
	logger.Logger.Info("Service: Successfully deleted config", zap.String("publicKey", publicKey))
	return nil
}

// BuildClientConfig generates a WireGuard .conf file content for a given client peer.
// peerCfg: The configuration of the peer for whom the .conf file is being generated.
//
//	Must include at least PrivateKey and AllowedIps.
//
// Returns the .conf file content as a string, or an error.
func (s *ConfigService) BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
	if peerCfg == nil {
		return "", errors.New("peer configuration cannot be nil for BuildClientConfig")
	}
	if clientPrivateKey == "" {
		logger.Logger.Warn("Service: BuildClientConfig called with empty clientPrivateKey", zap.String("peerPublicKey", peerCfg.PublicKey))
		return "", errors.New("client private key cannot be empty for building .conf file")
	}
	if peerCfg.PublicKey == "" {
		return "", errors.New("peer public key is missing from peerCfg, cannot build client config")
	}

	var b strings.Builder

	// [Interface] section: Client's own settings
	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", clientPrivateKey)) // Use the provided clientPrivateKey

	if len(peerCfg.AllowedIps) > 0 {
		clientAddress := peerCfg.AllowedIps[0]
		b.WriteString(fmt.Sprintf("Address = %s\n", clientAddress))
	} else {
		logger.Logger.Info("Service: Building client config for peer with no server-side AllowedIPs. Client Address field will be omitted.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}
	// Optionally, add DNS servers for the client to use when connected.
	// b.WriteString("DNS = 1.1.1.1, 8.8.8.8\n")
	b.WriteString("\n")

	// [Peer] section: Server's details (how the client connects to this server)
	b.WriteString("[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", s.ifacePubKey)) // This server's public key (from ServerKeyManager)

	if s.serverEndpoint != "" {
		b.WriteString(fmt.Sprintf("Endpoint = %s\n", s.serverEndpoint))
	} else {
		logger.Logger.Warn("Service: Server endpoint is not configured. Client .conf file will be missing the Endpoint field.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}

	// If the peer has a PresharedKey configured on the server side for this client, include it.
	// Note: If PSK is managed by the client and needs to be *provided* by the client for .conf generation,
	// then clientPresharedKey should also be a parameter to this function.
	// For now, we use peerCfg.PreSharedKey (which comes from 'wg show dump' for that peer).
	if peerCfg.PreSharedKey != "" {
		b.WriteString(fmt.Sprintf("PresharedKey = %s\n", peerCfg.PreSharedKey))
	}

	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n") // Default: route all traffic through VPN

	if peerCfg.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", peerCfg.PersistentKeepalive))
	}

	logger.Logger.Info("Service: Successfully built client config content using provided client private key.",
		zap.String("peerPublicKey", peerCfg.PublicKey))
	return b.String(), nil
}

// generateKeyPair is an internal helper to generate a new WireGuard private/public key pair
// by shelling out to the 'wg' utility.
// Returns the private key, public key, and an error if generation fails or times out.
// Uses s.keyGenerationTimeout for command execution.
func (s *ConfigService) generateKeyPair() (privKey, pubKey string, err error) {
	logger.Logger.Debug("Service: Generating new key pair", zap.Duration("timeout", s.keyGenerationTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), s.keyGenerationTimeout)
	defer cancel()

	// Generate private key: wg genkey
	genCmd := exec.CommandContext(ctx, "wg", "genkey")
	privKeyBytes, err := genCmd.Output() // CombinedOutput might be better if 'wg genkey' can write to stderr on success
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg genkey' execution")
		return "", "", fmt.Errorf("wg genkey timed out: %w", repository.ErrWgTimeout) // Use the specific timeout error from repository
	}
	if err != nil {
		// Attempt to get more detailed error info if it's an ExitError
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg genkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg genkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg) // Return the detailed error message
	}
	privKey = strings.TrimSpace(string(privKeyBytes))
	if privKey == "" {
		logger.Logger.Error("Service: 'wg genkey' produced an empty private key.")
		return "", "", errors.New("wg genkey produced empty private key")
	}

	// Generate public key from private key: wg pubkey
	pubCmd := exec.CommandContext(ctx, "wg", "pubkey")
	pubCmd.Stdin = strings.NewReader(privKey) // Pipe the generated private key to stdin of 'wg pubkey'
	pubKeyBytes, err := pubCmd.Output()       // Again, CombinedOutput could be useful
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg pubkey' execution")
		return "", "", fmt.Errorf("wg pubkey timed out: %w", repository.ErrWgTimeout)
	}
	if err != nil {
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg pubkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg pubkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate public key from private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg)
	}
	pubKey = strings.TrimSpace(string(pubKeyBytes))
	if pubKey == "" {
		logger.Logger.Error("Service: 'wg pubkey' produced an empty public key.")
		return "", "", errors.New("wg pubkey produced empty public key")
	}

	logger.Logger.Info("Service: Successfully generated new key pair.")
	return privKey, pubKey, nil
}

// RotatePeerKey removes an existing peer, generates a new key pair for it,
// and re-adds the peer with the new keys but preserving its previous AllowedIPs
// and PersistentKeepalive interval.
// oldPublicKey: The public key of the peer whose keys need to be rotated.
// Returns the domain.Config of the newly re-configured peer (including its new private key), or an error.
func (s *ConfigService) RotatePeerKey(oldPublicKey string) (*domain.Config, error) {
	if oldPublicKey == "" {
		logger.Logger.Warn("Service: RotatePeerKey called with empty old public key")
		return nil, errors.New("old public key cannot be empty for key rotation")
	}
	logger.Logger.Info("Service: Attempting to rotate peer key", zap.String("oldPublicKey", oldPublicKey))

	// 1. Retrieve current configuration of the peer to preserve settings.
	oldCfg, err := s.repo.GetConfig(oldPublicKey)
	if err != nil {
		logger.Logger.Error("Service (Rotate): Failed to get old peer config", zap.String("oldPublicKey", oldPublicKey), zap.Error(err))
		if errors.Is(err, repository.ErrPeerNotFound) {
			return nil, fmt.Errorf("cannot rotate key for peer %s: %w", oldPublicKey, repository.ErrPeerNotFound)
		}
		return nil, fmt.Errorf("failed to retrieve config for peer %s before rotation: %w", oldPublicKey, err)
	}

	// 2. Generate a new private/public key pair.
	newPrivKey, newPubKey, err := s.generateKeyPair()
	if err != nil {
		// Error already logged by generateKeyPair
		return nil, fmt.Errorf("key pair generation failed during rotation for %s: %w", oldPublicKey, err)
	}
	logger.Logger.Debug("Service (Rotate): Generated new key pair",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	// 3. Construct the new peer configuration.
	// Preserve AllowedIPs and PersistentKeepalive. PreSharedKey is typically regenerated separately if used.
	newPeerDomainCfg := domain.Config{
		PublicKey:           newPubKey,                  // New public key
		PrivateKey:          newPrivKey,                 // New private key (important for client)
		AllowedIps:          oldCfg.AllowedIps,          // Preserve from old config
		PersistentKeepalive: oldCfg.PersistentKeepalive, // Preserve from old config
		PreSharedKey:        oldCfg.PreSharedKey,        // Option: could clear this, or require new, or keep old. Keeping old for now.
		// Endpoint and traffic stats are state, not config to be preserved this way.
	}

	// 4. Add the new peer configuration to WireGuard.
	// This might effectively update the peer if one with the new public key (unlikely) already existed,
	// or create a new one.
	if err := s.repo.CreateConfig(newPeerDomainCfg); err != nil {
		logger.Logger.Error("Service (Rotate): Failed to create new peer config with rotated keys",
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		// Rollback is complex here. If new keys are generated but not applied, they are lost unless returned.
		// If create fails, the old peer is still active.
		return nil, fmt.Errorf("failed to apply new configuration for rotated peer %s (new key %s): %w", oldPublicKey, newPubKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully applied new config for peer with new keys",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	// 5. Remove the old peer configuration.
	// This is done *after* the new configuration is successfully applied to minimize downtime/issues.
	if err := s.repo.DeleteConfig(oldPublicKey); err != nil {
		// This is a problematic state: new peer is active, but old one couldn't be removed.
		logger.Logger.Error("CRITICAL (Rotate): New peer config applied, but FAILED TO DELETE OLD PEER CONFIG. Manual cleanup may be needed.",
			zap.String("oldPublicKey", oldPublicKey),
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		// Return the new config as it's active, but also an error indicating the cleanup failure.
		return &newPeerDomainCfg, fmt.Errorf("new peer %s (rotated from %s) created, but failed to delete old peer: %w", newPubKey, oldPublicKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully deleted old peer config", zap.String("oldPublicKey", oldPublicKey))

	// Return the new configuration, including the new PrivateKey for the client.
	return &newPeerDomainCfg, nil
}
