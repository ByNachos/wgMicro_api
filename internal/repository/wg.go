package repository

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
)

// ErrWgTimeout is returned when a command sent to the 'wg' utility
// does not complete within the allocated timeout period. This indicates a potential
// issue with the WireGuard process or system load.
var ErrWgTimeout = errors.New("wireguard command timed out")

// ErrPeerNotFound is returned by GetConfig when a peer with the specified public key
// does not exist on the WireGuard interface.
var ErrPeerNotFound = errors.New("peer not found")

// DefaultWgCmdTimeout defines the default timeout for 'wg' commands if not specified
// during WGRepository initialization. This serves as a fallback.
const DefaultWgCmdTimeout = 5 * time.Second

// Repo is an interface that defines methods for interacting with a WireGuard interface.
// This abstraction allows for different implementations, such as a real one using 'wg' commands
// or a fake one for testing.
type Repo interface {
	// ListConfigs retrieves all current peer configurations from the WireGuard interface.
	ListConfigs() ([]domain.Config, error)
	// GetConfig retrieves a specific peer configuration by its public key.
	// Returns ErrPeerNotFound if the peer does not exist.
	GetConfig(publicKey string) (*domain.Config, error)
	// CreateConfig adds a new peer to the WireGuard interface with the specified configuration.
	// This typically involves setting the public key, allowed IPs, and optionally preshared key
	// and persistent keepalive.
	CreateConfig(cfg domain.Config) error
	// UpdateAllowedIPs replaces the list of allowed IP networks for an existing peer.
	UpdateAllowedIPs(publicKey string, allowedIps []string) error
	// DeleteConfig removes a peer from the WireGuard interface using its public key.
	DeleteConfig(publicKey string) error
}

// WGRepository implements the Repo interface by interacting with the 'wg' command-line utility.
type WGRepository struct {
	iface      string        // Name of the WireGuard interface (e.g., "wg0") to manage.
	cmdTimeout time.Duration // Timeout duration for executing 'wg' commands.
}

// NewWGRepository creates a new instance of WGRepository.
// It requires the WireGuard interface name and a timeout for 'wg' commands.
// iface: The name of the WireGuard interface (e.g., "wg0").
// cmdTimeout: The maximum duration to wait for 'wg' commands to complete.
//
//	If non-positive, DefaultWgCmdTimeout is used.
func NewWGRepository(iface string, cmdTimeout time.Duration) *WGRepository {
	if iface == "" {
		// Interface name is critical for all operations.
		logger.Logger.Fatal("WireGuard interface name cannot be empty for WGRepository")
	}
	if cmdTimeout <= 0 {
		logger.Logger.Warn("Provided command timeout is invalid, using default",
			zap.Duration("providedTimeout", cmdTimeout),
			zap.Duration("defaultTimeout", DefaultWgCmdTimeout),
			zap.String("interface", iface))
		cmdTimeout = DefaultWgCmdTimeout
	}
	logger.Logger.Info("WGRepository initialized",
		zap.String("interface", iface),
		zap.Duration("commandTimeout", cmdTimeout))
	return &WGRepository{
		iface:      iface,
		cmdTimeout: cmdTimeout,
	}
}

// runWgCommand executes a 'wg' utility command with the configured timeout and arguments.
// It centralizes common logic for command execution, context handling, timeout, and error logging.
// The 'args' parameter should contain all arguments to 'wg' *after* the 'wg' command itself
// (e.g., "show", "wg0", "dump").
// Returns the combined output (stdout and stderr) of the command and an error if one occurred.
func (r *WGRepository) runWgCommand(args ...string) ([]byte, error) {
	fullArgs := strings.Join(args, " ")
	logger.Logger.Debug("Executing 'wg' command",
		zap.String("interface", r.iface), // Though r.iface is often part of args, logging it here is for consistency
		zap.String("commandArgs", fullArgs),
		zap.Duration("timeout", r.cmdTimeout))

	ctx, cancel := context.WithTimeout(context.Background(), r.cmdTimeout)
	defer cancel()

	// The command is always 'wg'.
	cmd := exec.CommandContext(ctx, "wg", args...)
	out, err := cmd.CombinedOutput() // Captures both stdout and stderr.

	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("WireGuard command timed out",
			zap.String("commandArgs", fullArgs),
			zap.Duration("timeout", r.cmdTimeout),
			zap.String("interface", r.iface))
		return nil, ErrWgTimeout // Return the specific timeout error
	}
	if err != nil {
		// Error from exec.Command (e.g., command not found, permission issues, or non-zero exit code)
		logger.Logger.Error("WireGuard command execution failed",
			zap.String("commandArgs", fullArgs),
			zap.Error(err),                    // Original error from exec
			zap.String("output", string(out)), // Output from the command, which might contain error details from 'wg' itself
			zap.String("interface", r.iface))
		// Wrap the original error to provide more context.
		return out, fmt.Errorf("wg %s: execution failed: %w; output: %s", fullArgs, err, string(out))
	}

	logger.Logger.Debug("WireGuard command executed successfully",
		zap.String("commandArgs", fullArgs),
		zap.String("interface", r.iface) /*, zap.String("output", string(out)) // Output might be too verbose for successful debug log */)
	return out, nil
}

// ListConfigs retrieves all current peer configurations by executing 'wg show <interface> dump'.
// It parses the tab-separated output from the command.
func (r *WGRepository) ListConfigs() ([]domain.Config, error) {
	// Construct arguments for 'wg show <interface> dump'
	commandArgs := []string{"show", r.iface, "dump"}
	out, err := r.runWgCommand(commandArgs...)
	if err != nil {
		// If it's a timeout, runWgCommand already returned ErrWgTimeout.
		// Otherwise, it's a different execution error.
		// No need to wrap ErrWgTimeout again, just return it.
		if errors.Is(err, ErrWgTimeout) {
			return nil, ErrWgTimeout
		}
		// For other errors, wrap them to indicate context of ListConfigs.
		return nil, fmt.Errorf("failed to list peer configurations for interface %s: %w", r.iface, err)
	}

	outputStr := strings.TrimSpace(string(out))
	if outputStr == "" {
		// This can happen if the interface is down or has no peers and 'wg show <iface> dump' returns nothing.
		logger.Logger.Debug("`wg show dump` returned empty output, assuming no peers or interface data.", zap.String("interface", r.iface))
		return []domain.Config{}, nil
	}

	lines := strings.Split(outputStr, "\n")
	var configs []domain.Config

	// The first line of "wg show <iface> dump" output is usually the server's own interface details.
	// We are interested in peer configurations, which appear on subsequent lines.
	// A peer line starts with the peer's public key.
	// An interface line (the first one) starts with the server's private key, then public key, listen port, fwmark.
	// We skip the first line if it doesn't parse as a peer or if it matches server details.
	// A more robust way is to check if the number of fields matches a peer or an interface.
	// For `wg show <iface> dump`, the first line is always the interface itself if it's up.
	isFirstLine := true
	for _, line := range lines {
		if line == "" {
			continue // Skip any empty lines
		}
		if isFirstLine {
			isFirstLine = false
			// Heuristic: if the line contains the interface name or doesn't look like a peer, skip.
			// A peer public key is typically 44 chars, base64.
			// An interface line for 'wg show <iface> dump' has 4 fields: privkey, pubkey, listen_port, fwmark
			// A peer line has 8 fields: pubkey, psk, endpoint,  allowed_ips, handshake, rx, tx, keepalive
			// The provided parsing logic below expects peer data.
			parts := strings.Fields(line)
			if len(parts) != 8 { // If it's not 8 fields, it's likely the interface line or malformed.
				logger.Logger.Debug("Skipping non-peer line from `wg show dump` output", zap.String("line", line), zap.String("interface", r.iface))
				continue
			}
		}

		parts := strings.Fields(line)
		// Expected fields for a peer: PublicKey, PreSharedKey, Endpoint, AllowedIPs, LatestHandshake, RxBytes, TxBytes, PersistentKeepalive
		if len(parts) < 8 {
			logger.Logger.Warn("Skipping malformed peer line in 'wg show dump' output",
				zap.String("line", line),
				zap.Int("numParts", len(parts)),
				zap.String("interface", r.iface))
			continue
		}

		publicKey := parts[0]
		preSharedKey := parts[1]
		endpoint := parts[2]
		allowedIPsStr := parts[3]
		latestHandshakeStr := parts[4]
		receiveBytesStr := parts[5]
		transmitBytesStr := parts[6]
		persistentKeepaliveStr := parts[7]

		if preSharedKey == "(none)" {
			preSharedKey = ""
		}
		if endpoint == "(none)" {
			endpoint = ""
		}

		var allowedIPsList []string
		if allowedIPsStr != "(none)" {
			allowedIPsList = strings.Split(allowedIPsStr, ",")
		} else {
			allowedIPsList = []string{}
		}

		latestHandshake, errLH := strconv.ParseInt(latestHandshakeStr, 10, 64)
		if errLH != nil {
			logger.Logger.Warn("Failed to parse LatestHandshake for peer, using 0", zap.String("value", latestHandshakeStr), zap.String("peerKey", publicKey), zap.Error(errLH))
			latestHandshake = 0
		}

		receiveBytes, errRx := strconv.ParseUint(receiveBytesStr, 10, 64)
		if errRx != nil {
			logger.Logger.Warn("Failed to parse ReceiveBytes for peer, using 0", zap.String("value", receiveBytesStr), zap.String("peerKey", publicKey), zap.Error(errRx))
			receiveBytes = 0
		}

		transmitBytes, errTx := strconv.ParseUint(transmitBytesStr, 10, 64)
		if errTx != nil {
			logger.Logger.Warn("Failed to parse TransmitBytes for peer, using 0", zap.String("value", transmitBytesStr), zap.String("peerKey", publicKey), zap.Error(errTx))
			transmitBytes = 0
		}

		var persistentKeepaliveVal int
		if persistentKeepaliveStr != "off" {
			pkVal, errPK := strconv.Atoi(persistentKeepaliveStr)
			if errPK == nil {
				persistentKeepaliveVal = pkVal
			} else {
				logger.Logger.Warn("Failed to parse PersistentKeepalive for peer, using 0", zap.String("value", persistentKeepaliveStr), zap.String("peerKey", publicKey), zap.Error(errPK))
			}
		}

		configs = append(configs, domain.Config{
			PublicKey:           publicKey,
			PreSharedKey:        preSharedKey,
			Endpoint:            endpoint,
			AllowedIps:          allowedIPsList,
			LatestHandshake:     latestHandshake,
			ReceiveBytes:        receiveBytes,
			TransmitBytes:       transmitBytes,
			PersistentKeepalive: persistentKeepaliveVal,
		})
	}
	logger.Logger.Debug("Successfully parsed peer configurations", zap.Int("count", len(configs)), zap.String("interface", r.iface))
	return configs, nil
}

// GetConfig retrieves a specific peer's configuration.
// It iterates through all configurations obtained from ListConfigs.
// Returns ErrPeerNotFound if no peer matches the given publicKey.
func (r *WGRepository) GetConfig(publicKey string) (*domain.Config, error) {
	if publicKey == "" {
		return nil, errors.New("public key cannot be empty when fetching peer config") // Or a more specific validation error
	}
	allConfigs, err := r.ListConfigs()
	if err != nil {
		// Error is already contextualized by ListConfigs or runWgCommand.
		return nil, err
	}

	for _, cfg := range allConfigs {
		if cfg.PublicKey == publicKey {
			logger.Logger.Debug("Peer config found", zap.String("publicKey", publicKey), zap.String("interface", r.iface))
			return &cfg, nil
		}
	}
	logger.Logger.Warn("Peer config not found", zap.String("publicKey", publicKey), zap.String("interface", r.iface))
	return nil, ErrPeerNotFound // Specific error for "not found"
}

// CreateConfig adds a new peer to the WireGuard interface.
// It constructs and executes 'wg set <interface> peer <publicKey> [preshared-key <file|/dev/stdin>] [allowed-ips <ip1,ip2...>] [persistent-keepalive <interval>]'.
// The preshared-key, if provided, is passed via stdin for security.
func (r *WGRepository) CreateConfig(cfg domain.Config) error {
	if cfg.PublicKey == "" {
		return errors.New("public key is required to create peer config")
	}
	// AllowedIPs can be empty, 'wg' might interpret this as no IPs, or it might be an error depending on 'wg' version.
	// For consistency, we require it for new peer creation as per previous logic, though 'wg set' itself is flexible.
	if len(cfg.AllowedIps) == 0 {
		logger.Logger.Warn("Creating peer config with no AllowedIPs specified.", zap.String("publicKey", cfg.PublicKey), zap.String("interface", r.iface))
		// return errors.New("allowedIps are required to create peer config") // Re-enable if strictness is desired
	}

	// Base arguments: wg set <interface> peer <publicKey>
	args := []string{"set", r.iface, "peer", cfg.PublicKey}

	// Handle preshared-key separately due to stdin piping.
	if cfg.PreSharedKey != "" {
		// Create a temporary list of args for the PSK command.
		pskArgs := append(args, "preshared-key", "/dev/stdin")
		// Append other non-PSK related args that should be part of this same 'wg set' command
		if len(cfg.AllowedIps) > 0 {
			pskArgs = append(pskArgs, "allowed-ips", strings.Join(cfg.AllowedIps, ","))
		}
		if cfg.PersistentKeepalive > 0 {
			pskArgs = append(pskArgs, "persistent-keepalive", strconv.Itoa(cfg.PersistentKeepalive))
		}
		// Endpoint is not typically set on the server for a peer this way.

		logger.Logger.Debug("Executing 'wg set peer' with PresharedKey (via stdin)",
			zap.String("args", strings.Join(pskArgs, " ")), zap.String("interface", r.iface))

		ctx, cancel := context.WithTimeout(context.Background(), r.cmdTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "wg", pskArgs...)
		cmd.Stdin = strings.NewReader(cfg.PreSharedKey) // Pipe PSK to stdin

		out, err := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			logger.Logger.Error("WireGuard 'set peer' (with PSK) command timed out", zap.String("publicKey", cfg.PublicKey), zap.String("interface", r.iface))
			return ErrWgTimeout
		}
		if err != nil {
			logger.Logger.Error("WireGuard 'set peer' (with PSK) command failed",
				zap.String("publicKey", cfg.PublicKey), zap.Error(err), zap.String("output", string(out)), zap.String("interface", r.iface))
			return fmt.Errorf("wg set peer %s with PSK: failed: %w; output: %s", cfg.PublicKey, err, string(out))
		}
		logger.Logger.Info("Successfully created/updated peer with PSK", zap.String("publicKey", cfg.PublicKey), zap.String("interface", r.iface))
		return nil // Successfully created with PSK
	}

	// If no PSK, build args for a single command
	if len(cfg.AllowedIps) > 0 {
		args = append(args, "allowed-ips", strings.Join(cfg.AllowedIps, ","))
	} else {
		// If AllowedIps is empty and we want to explicitly remove them (or set to none)
		// wg set <dev> peer <key> allowed-ips ""
		// or for some versions/contexts 'allowed-ips "(none)"'. Empty string usually works.
		args = append(args, "allowed-ips", "")
		logger.Logger.Debug("Setting empty AllowedIPs for peer", zap.String("publicKey", cfg.PublicKey), zap.String("interface", r.iface))
	}

	if cfg.PersistentKeepalive > 0 {
		args = append(args, "persistent-keepalive", strconv.Itoa(cfg.PersistentKeepalive))
	}

	_, err := r.runWgCommand(args...)
	if err != nil {
		// runWgCommand already logged the error. Wrap it for context.
		return fmt.Errorf("failed to create peer config for %s on interface %s: %w", cfg.PublicKey, r.iface, err)
	}
	logger.Logger.Info("Successfully created/updated peer (no PSK)", zap.String("publicKey", cfg.PublicKey), zap.String("interface", r.iface))
	return nil
}

// UpdateAllowedIPs replaces the list of allowed IP networks for an existing peer.
// Executes 'wg set <interface> peer <publicKey> allowed-ips <ip1,ip2...>'.
// An empty 'allowedIps' slice will attempt to remove all allowed IPs for the peer.
func (r *WGRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	if publicKey == "" {
		return errors.New("public key is required to update peer's allowed IPs")
	}

	// `strings.Join` on an empty slice results in an empty string.
	// `wg set <dev> peer <key> allowed-ips ""` typically removes all allowed IPs.
	ipsString := strings.Join(allowedIps, ",")
	if ipsString == "" {
		logger.Logger.Info("Updating peer with empty allowed IPs (will remove existing).",
			zap.String("publicKey", publicKey), zap.String("interface", r.iface))
	}

	args := []string{"set", r.iface, "peer", publicKey, "allowed-ips", ipsString}

	_, err := r.runWgCommand(args...)
	if err != nil {
		// runWgCommand already logged the error. Wrap it for context.
		return fmt.Errorf("failed to update allowed IPs for peer %s on interface %s: %w", publicKey, r.iface, err)
	}
	logger.Logger.Info("Successfully updated allowed IPs", zap.String("publicKey", publicKey), zap.Strings("newAllowedIPs", allowedIps), zap.String("interface", r.iface))
	return nil
}

// DeleteConfig removes a peer from the WireGuard interface.
// Executes 'wg set <interface> peer <publicKey> remove'.
func (r *WGRepository) DeleteConfig(publicKey string) error {
	if publicKey == "" {
		return errors.New("public key is required to delete peer config")
	}
	args := []string{"set", r.iface, "peer", publicKey, "remove"}

	_, err := r.runWgCommand(args...)
	if err != nil {
		// 'wg set ... remove' on a non-existent peer usually does not result in an error code,
		// but if 'runWgCommand' returns an error, it's likely a more fundamental issue.
		// runWgCommand already logged the error. Wrap it for context.
		return fmt.Errorf("failed to delete peer %s from interface %s: %w", publicKey, r.iface, err)
	}
	logger.Logger.Info("Successfully deleted peer", zap.String("publicKey", publicKey), zap.String("interface", r.iface))
	return nil
}
