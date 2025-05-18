// internal/service/config.go
package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

// DefaultKeyGenTimeout is used if no timeout is specified for CLIENT key generation.
const DefaultKeyGenTimeoutService = 5 * time.Second // Renamed to avoid conflict if config also has one

// ConfigService encapsulates business logic for managing WireGuard peer configurations.
type ConfigService struct {
	repo                   repository.Repo
	serverBasePublicKey    string        // Public key of THIS server's WireGuard interface
	serverBaseEndpoint     string        // External endpoint of THIS server (host:port) for client configs
	clientKeyGenTimeout    time.Duration // Timeout for client key generation commands ('wg genkey', 'wg pubkey')
	clientConfigDNSServers []string      // DNS servers for client .conf files (from app config)
}

// NewConfigService creates a new instance of ConfigService.
func NewConfigService(
	repo repository.Repo,
	serverInterfacePublicKey string, // Public key of this server's WG interface
	serverExternalEndpoint string, // Public endpoint of this server (for clients)
	clientKeyGenCmdTimeout time.Duration, // Timeout for 'wg genkey', 'wg pubkey' for client keys
	dnsServersForClient []string, // DNS servers for client .conf files
) *ConfigService {
	if repo == nil {
		logger.Logger.Fatal("Repository cannot be nil for ConfigService")
	}
	if serverInterfacePublicKey == "" {
		logger.Logger.Fatal("Server public key is empty in ConfigService initialization.")
	}

	if clientKeyGenCmdTimeout <= 0 {
		logger.Logger.Warn("Provided client key generation timeout is invalid, using default",
			zap.Duration("providedTimeout", clientKeyGenCmdTimeout),
			zap.Duration("defaultTimeout", DefaultKeyGenTimeoutService))
		clientKeyGenCmdTimeout = DefaultKeyGenTimeoutService
	}

	s := &ConfigService{
		repo:                   repo,
		serverBasePublicKey:    serverInterfacePublicKey,
		serverBaseEndpoint:     serverExternalEndpoint,
		clientKeyGenTimeout:    clientKeyGenCmdTimeout,
		clientConfigDNSServers: dnsServersForClient,
	}

	logger.Logger.Info("ConfigService initialized",
		zap.String("serverPublicKeyFirstChars", s.serverBasePublicKey[:min(10, len(s.serverBasePublicKey))]+"..."),
		zap.String("serverEndpointForClients", s.serverBaseEndpoint), // Changed from Bool to actual string
		zap.Duration("clientKeyGenTimeout", s.clientKeyGenTimeout),
		zap.Strings("clientConfigDNSServers", s.clientConfigDNSServers), // Changed from string to Strings
	)
	return s
}

// min is a helper function.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetAll retrieves all peer configurations.
func (s *ConfigService) GetAll() ([]domain.Config, error) {
	configs, err := s.repo.ListConfigs()
	if err != nil {
		logger.Logger.Error("Service: Failed to get all configs from repository", zap.Error(err))
		return nil, err
	}
	logger.Logger.Debug("Service: Successfully retrieved all configs", zap.Int("count", len(configs)))
	return configs, nil
}

// Get retrieves a single peer's configuration by its public key.
func (s *ConfigService) Get(publicKey string) (*domain.Config, error) {
	if publicKey == "" {
		logger.Logger.Warn("Service: Get config called with empty public key")
		return nil, errors.New("public key cannot be empty for Get operation")
	}
	config, err := s.repo.GetConfig(publicKey)
	if err != nil {
		if errors.Is(err, repository.ErrPeerNotFound) {
			logger.Logger.Info("Service: Peer not found in repository", zap.String("publicKey", publicKey))
		} else {
			logger.Logger.Error("Service: Failed to get config by public key from repository", zap.String("publicKey", publicKey), zap.Error(err))
		}
		return nil, err
	}
	logger.Logger.Debug("Service: Successfully retrieved config by public key", zap.String("publicKey", publicKey))
	return config, nil
}

// CreateWithNewKeys generates a new key pair, creates the peer, and returns its configuration including the private key.
func (s *ConfigService) CreateWithNewKeys(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
	if len(allowedIPs) == 0 {
		logger.Logger.Info("Service: Creating new peer with empty AllowedIPs. This might be acceptable depending on WG configuration.")
	}

	newPrivKey, newPubKey, err := s.generateKeyPair() // Uses s.clientKeyGenTimeout
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair for new peer: %w", err)
	}

	newPeerCfg := domain.Config{
		PublicKey:           newPubKey,
		PrivateKey:          newPrivKey, // Important to return to the client!
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}

	repoPeerCfg := domain.Config{
		PublicKey:           newPubKey,
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}
	if err := s.repo.CreateConfig(repoPeerCfg); err != nil {
		return nil, fmt.Errorf("failed to add new peer %s to WireGuard: %w", newPubKey, err)
	}

	logger.Logger.Info("Service: Successfully created new peer with generated keys.",
		zap.String("newPublicKey", newPeerCfg.PublicKey))
	return &newPeerCfg, nil
}

// UpdateAllowedIPs updates the allowed IPs for an existing peer.
func (s *ConfigService) UpdateAllowedIPs(publicKey string, ips []string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: UpdateAllowedIPs called with empty public key")
		return errors.New("public key is required for updating allowed IPs")
	}
	err := s.repo.UpdateAllowedIPs(publicKey, ips)
	if err != nil {
		logger.Logger.Error("Service: Failed to update allowed IPs in repository",
			zap.String("publicKey", publicKey),
			zap.Strings("newIPs", ips),
			zap.Error(err))
		return err
	}
	logger.Logger.Info("Service: Successfully updated allowed IPs", zap.String("publicKey", publicKey), zap.Strings("newIPs", ips))
	return nil
}

// Delete removes a peer.
func (s *ConfigService) Delete(publicKey string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: Delete config called with empty public key")
		return errors.New("public key is required for deleting a peer")
	}
	err := s.repo.DeleteConfig(publicKey)
	if err != nil {
		logger.Logger.Error("Service: Failed to delete config in repository", zap.String("publicKey", publicKey), zap.Error(err))
		return err
	}
	logger.Logger.Info("Service: Successfully deleted config", zap.String("publicKey", publicKey))
	return nil
}

// BuildClientConfig generates the .conf file content for a client.
// peerCfg: Peer configuration from the server (usually from 'wg show dump').
// clientPrivateKey: Client's private key, provided by the external application.
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

	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", clientPrivateKey))
	if len(peerCfg.AllowedIps) > 0 {
		clientAddress := peerCfg.AllowedIps[0] // Usually the first IP server allows for this peer
		b.WriteString(fmt.Sprintf("Address = %s\n", clientAddress))
	} else {
		logger.Logger.Info("Service: Building client config for peer with no server-side AllowedIPs. Client Address field will be omitted.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}

	if len(s.clientConfigDNSServers) > 0 {
		b.WriteString(fmt.Sprintf("DNS = %s\n", strings.Join(s.clientConfigDNSServers, ", ")))
	}

	b.WriteString("\n")
	b.WriteString("[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", s.serverBasePublicKey))

	if s.serverBaseEndpoint != "" {
		b.WriteString(fmt.Sprintf("Endpoint = %s\n", s.serverBaseEndpoint))
	} else {
		logger.Logger.Warn("Service: Server endpoint is not configured. Client .conf file will be missing the Endpoint field.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}

	if peerCfg.PreSharedKey != "" {
		b.WriteString(fmt.Sprintf("PresharedKey = %s\n", peerCfg.PreSharedKey))
	}

	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n") // Route all traffic through VPN

	if peerCfg.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", peerCfg.PersistentKeepalive))
	}

	logger.Logger.Info("Service: Successfully built client config content using provided client private key.",
		zap.String("peerPublicKey", peerCfg.PublicKey))
	return b.String(), nil
}

// generateKeyPair generates a new key pair (private/public).
func (s *ConfigService) generateKeyPair() (privKey, pubKey string, err error) {
	logger.Logger.Debug("Service: Generating new key pair for a client", zap.Duration("timeout", s.clientKeyGenTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), s.clientKeyGenTimeout)
	defer cancel()

	genCmd := exec.CommandContext(ctx, "wg", "genkey")
	privKeyBytes, err := genCmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg genkey' for client key.")
		return "", "", fmt.Errorf("wg genkey timed out: %w", repository.ErrWgTimeout)
	}
	if err != nil {
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg genkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg genkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate client private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg)
	}
	privKey = strings.TrimSpace(string(privKeyBytes))
	if privKey == "" {
		logger.Logger.Error("Service: 'wg genkey' produced an empty private key for client.")
		return "", "", errors.New("wg genkey produced empty private key")
	}

	pubCmd := exec.CommandContext(ctx, "wg", "pubkey")
	pubCmd.Stdin = strings.NewReader(privKey)
	pubKeyBytes, err := pubCmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg pubkey' for client key.")
		return "", "", fmt.Errorf("wg pubkey timed out: %w", repository.ErrWgTimeout)
	}
	if err != nil {
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg pubkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg pubkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate client public key from private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg)
	}
	pubKey = strings.TrimSpace(string(pubKeyBytes))
	if pubKey == "" {
		logger.Logger.Error("Service: 'wg pubkey' produced an empty public key for client.")
		return "", "", errors.New("wg pubkey produced empty public key")
	}

	logger.Logger.Info("Service: Successfully generated new client key pair.")
	return privKey, pubKey, nil
}

// RotatePeerKey rotates keys for an existing peer.
func (s *ConfigService) RotatePeerKey(oldPublicKey string) (*domain.Config, error) {
	if oldPublicKey == "" {
		logger.Logger.Warn("Service: RotatePeerKey called with empty old public key")
		return nil, errors.New("old public key cannot be empty for key rotation")
	}
	logger.Logger.Info("Service: Attempting to rotate peer key", zap.String("oldPublicKey", oldPublicKey))

	oldCfg, err := s.repo.GetConfig(oldPublicKey)
	if err != nil {
		logger.Logger.Error("Service (Rotate): Failed to get old peer config", zap.String("oldPublicKey", oldPublicKey), zap.Error(err))
		if errors.Is(err, repository.ErrPeerNotFound) {
			return nil, fmt.Errorf("cannot rotate key for peer %s: %w", oldPublicKey, repository.ErrPeerNotFound)
		}
		return nil, fmt.Errorf("failed to retrieve config for peer %s before rotation: %w", oldPublicKey, err)
	}

	newPrivKey, newPubKey, err := s.generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("key pair generation failed during rotation for %s: %w", oldPublicKey, err)
	}
	logger.Logger.Debug("Service (Rotate): Generated new key pair",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	newPeerDomainCfg := domain.Config{
		PublicKey:           newPubKey,
		PrivateKey:          newPrivKey, // For client response
		AllowedIps:          oldCfg.AllowedIps,
		PreSharedKey:        oldCfg.PreSharedKey,
		PersistentKeepalive: oldCfg.PersistentKeepalive,
	}

	repoPeerCfgForCreate := domain.Config{
		PublicKey:           newPubKey,
		AllowedIps:          oldCfg.AllowedIps,
		PreSharedKey:        oldCfg.PreSharedKey,
		PersistentKeepalive: oldCfg.PersistentKeepalive,
	}
	if err := s.repo.CreateConfig(repoPeerCfgForCreate); err != nil {
		logger.Logger.Error("Service (Rotate): Failed to create new peer config with rotated keys",
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		return nil, fmt.Errorf("failed to apply new configuration for rotated peer %s (new key %s): %w", oldPublicKey, newPubKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully applied new config for peer with new keys",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	if err := s.repo.DeleteConfig(oldPublicKey); err != nil {
		logger.Logger.Error("CRITICAL (Rotate): New peer config applied, but FAILED TO DELETE OLD PEER CONFIG. Manual cleanup may be needed.",
			zap.String("oldPublicKey", oldPublicKey),
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		return &newPeerDomainCfg, fmt.Errorf("new peer %s (rotated from %s) created, but failed to delete old peer: %w; the new peer configuration is still valid and returned", newPubKey, oldPublicKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully deleted old peer config", zap.String("oldPublicKey", oldPublicKey))

	return &newPeerDomainCfg, nil
}
