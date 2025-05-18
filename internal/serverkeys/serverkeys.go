package serverkeys

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"

	"go.uber.org/zap"
)

const (
	wgKeyGenTimeout = 5 * time.Second // Timeout for 'wg pubkey' command
)

// ServerKeyManager is responsible for loading and providing the WireGuard server's
// own private and public keys by parsing the server's configuration file.
type ServerKeyManager struct {
	mu               sync.RWMutex
	configPath       string
	serverPrivateKey string
	serverPublicKey  string
	// wgToolPath     string // Optional: path to 'wg' utility if not in PATH
}

// NewServerKeyManager creates a new ServerKeyManager and loads the server keys.
// wgConfigPath: Path to the WireGuard server configuration file (e.g., /etc/wireguard/wg0.conf).
// keyGenCmdTimeout: Timeout for the 'wg pubkey' command used to derive the public key.
func NewServerKeyManager(wgConfigPath string, keyGenCmdTimeout time.Duration) (*ServerKeyManager, error) {
	if wgConfigPath == "" {
		return nil, fmt.Errorf("WireGuard server configuration path cannot be empty")
	}
	if keyGenCmdTimeout <= 0 {
		keyGenCmdTimeout = wgKeyGenTimeout // Use default if invalid
	}

	skm := &ServerKeyManager{
		configPath: wgConfigPath,
	}

	if err := skm.loadServerKeys(keyGenCmdTimeout); err != nil {
		return nil, fmt.Errorf("failed to load server keys from %s: %w", wgConfigPath, err)
	}

	logger.Logger.Info("ServerKeyManager initialized successfully.",
		zap.String("configFile", wgConfigPath),
		zap.String("serverPublicKey", skm.serverPublicKey),
	)
	return skm, nil
}

// loadServerKeys reads the [Interface] section of the wgConfigPath to find the PrivateKey,
// and then derives the PublicKey.
func (skm *ServerKeyManager) loadServerKeys(keyGenCmdTimeout time.Duration) error {
	skm.mu.Lock()
	defer skm.mu.Unlock()

	file, err := os.Open(skm.configPath)
	if err != nil {
		logger.Logger.Error("Failed to open WireGuard server config file", zap.String("path", skm.configPath), zap.Error(err))
		return fmt.Errorf("could not open WireGuard config file %s: %w", skm.configPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inInterfaceSection := false
	foundPrivateKey := ""

	logger.Logger.Debug("Parsing server config file for [Interface] PrivateKey", zap.String("path", skm.configPath))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[Interface]") {
			inInterfaceSection = true
			continue
		}
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[Interface]") {
			// Moved out of [Interface] section or into another section
			inInterfaceSection = false
			if foundPrivateKey != "" { // If we found private key, no need to scan further for it.
				break
			}
			continue
		}

		if inInterfaceSection {
			if strings.HasPrefix(strings.ToLower(line), "privatekey") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					foundPrivateKey = strings.TrimSpace(parts[1])
					logger.Logger.Info("Found server PrivateKey in config file.", zap.String("path", skm.configPath))
					break // Found the private key, exit loop
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Logger.Error("Error while scanning WireGuard server config file", zap.String("path", skm.configPath), zap.Error(err))
		return fmt.Errorf("error scanning WireGuard config file %s: %w", skm.configPath, err)
	}

	if foundPrivateKey == "" {
		logger.Logger.Error("Server PrivateKey not found in [Interface] section of config file.", zap.String("path", skm.configPath))
		return fmt.Errorf("PrivateKey not found in [Interface] section of %s", skm.configPath)
	}
	skm.serverPrivateKey = foundPrivateKey

	// Derive PublicKey from PrivateKey using 'wg pubkey'
	ctx, cancel := context.WithTimeout(context.Background(), keyGenCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", "pubkey")
	cmd.Stdin = strings.NewReader(skm.serverPrivateKey)
	pubKeyBytes, err := cmd.Output()

	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Timeout during 'wg pubkey' for server key derivation.", zap.Error(ctx.Err()))
		return fmt.Errorf("wg pubkey timed out: %w", repository.ErrWgTimeout) // Assuming repository.ErrWgTimeout is accessible or define a similar one
	}
	if err != nil {
		var execErr *exec.ExitError
		errMsg := fmt.Sprintf("wg pubkey command failed: %s", err.Error())
		if errors.As(err, &execErr) {
			errMsg = fmt.Sprintf("wg pubkey command failed with exit code %d: %s. Stderr: %s", execErr.ExitCode(), err.Error(), string(execErr.Stderr))
		}
		logger.Logger.Error("Failed to derive server public key using 'wg pubkey'.", zap.String("details", errMsg), zap.Error(err))
		return fmt.Errorf(errMsg)
	}
	skm.serverPublicKey = strings.TrimSpace(string(pubKeyBytes))

	if skm.serverPublicKey == "" {
		logger.Logger.Error("Derived server public key is empty.")
		return fmt.Errorf("derived server public key is empty")
	}

	return nil
}

// GetServerPublicKey returns the loaded/derived public key of the WireGuard server interface.
func (skm *ServerKeyManager) GetServerPublicKey() (string, error) {
	skm.mu.RLock()
	defer skm.mu.RUnlock()
	if skm.serverPublicKey == "" {
		// This should not happen if initialization was successful.
		return "", fmt.Errorf("server public key is not loaded")
	}
	return skm.serverPublicKey, nil
}

// GetServerPrivateKey returns the loaded private key of the WireGuard server interface.
// Use with caution, as exposing private keys should be minimized.
func (skm *ServerKeyManager) GetServerPrivateKey() (string, error) {
	skm.mu.RLock()
	defer skm.mu.RUnlock()
	if skm.serverPrivateKey == "" {
		// This should not happen if initialization was successful.
		return "", fmt.Errorf("server private key is not loaded")
	}
	return skm.serverPrivateKey, nil
}

// ReloadServerKeys re-reads the server keys from the configuration file.
func (skm *ServerKeyManager) ReloadServerKeys(keyGenCmdTimeout time.Duration) error {
	logger.Logger.Info("Reloading server keys from configuration file.", zap.String("configFile", skm.configPath))
	if keyGenCmdTimeout <= 0 {
		keyGenCmdTimeout = wgKeyGenTimeout
	}
	return skm.loadServerKeys(keyGenCmdTimeout)
}
