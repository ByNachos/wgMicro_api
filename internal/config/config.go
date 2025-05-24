// internal/config/config.go
package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"wgMicro_api/internal/repository"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"

	DefaultAppEnv                 = EnvDevelopment
	DefaultPort                   = "8080"
	DefaultWGInterface            = "wg0"
	DefaultWgCmdTimeoutSeconds    = 5
	DefaultKeyGenTimeoutSeconds   = 5
	DefaultServerEndpointPort     = "51820"
	DefaultServerListenPort       = 51820 // Fallback if WG_ACTUAL_LISTEN_PORT is not set by entrypoint
	DefaultClientConfigDNSServers = ""
	DefaultClientConfigMTU        = 0 // Fallback if WG_ACTUAL_MTU is not set by entrypoint and CLIENT_CONFIG_MTU is not in .env
)

type Config struct {
	AppEnv      string
	Port        string
	WGInterface string

	Server struct {
		PrivateKey         string
		PublicKey          string   // Derived
		EndpointHost       string   // Always from .env
		EndpointPort       string   // Always from .env
		ListenPort         int      // Potentially from WG_ACTUAL_LISTEN_PORT or .env
		InterfaceAddresses []string // Potentially from WG_ACTUAL_INTERFACE_ADDRESSES or .env
	}

	ClientConfig struct {
		DNSServers string // Always from .env
		MTU        int    // Potentially from WG_ACTUAL_MTU or .env
	}

	Timeouts struct {
		WgCmdSeconds  int
		KeyGenSeconds int
	}

	DerivedWgCmdTimeout   time.Duration
	DerivedKeyGenTimeout  time.Duration
	DerivedServerEndpoint string // Derived from Server.EndpointHost and Server.EndpointPort
}

func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.AppEnv) == EnvDevelopment
}

// getEnvWithFallback first checks for a primary environment variable,
// then a secondary (fallback) one, and finally returns a default value if neither is found.
func getEnvWithFallback(primaryKey, secondaryKey, defaultValue string) string {
	if value, exists := os.LookupEnv(primaryKey); exists && value != "" {
		log.Printf("INFO: Using value from primary env var %s: '%s'", primaryKey, value)
		return value
	}
	if value, exists := os.LookupEnv(secondaryKey); exists && value != "" {
		log.Printf("INFO: Using value from secondary env var %s: '%s'", secondaryKey, value)
		return value
	}
	log.Printf("INFO: Using default value for %s/%s: '%s'", primaryKey, secondaryKey, defaultValue)
	return defaultValue
}

// getEnvIntWithFallback works similarly to getEnvWithFallback but for integers.
func getEnvIntWithFallback(primaryKey, secondaryKey string, defaultValue int) int {
	primaryValueStr, primaryExists := os.LookupEnv(primaryKey)
	if primaryExists && primaryValueStr != "" {
		valueInt, err := strconv.Atoi(primaryValueStr)
		if err == nil {
			log.Printf("INFO: Using integer value from primary env var %s: %d", primaryKey, valueInt)
			return valueInt
		}
		log.Printf("WARNING: Invalid integer value for primary env var %s: '%s'. Trying secondary. Error: %v", primaryKey, primaryValueStr, err)
	}

	secondaryValueStr, secondaryExists := os.LookupEnv(secondaryKey)
	if secondaryExists && secondaryValueStr != "" {
		valueInt, err := strconv.Atoi(secondaryValueStr)
		if err == nil {
			log.Printf("INFO: Using integer value from secondary env var %s: %d", secondaryKey, valueInt)
			return valueInt
		}
		log.Printf("WARNING: Invalid integer value for secondary env var %s: '%s'. Using default. Error: %v", secondaryKey, secondaryValueStr, err)
	}

	log.Printf("INFO: Using default integer value for %s/%s: %d", primaryKey, secondaryKey, defaultValue)
	return defaultValue
}

func LoadConfig() *Config {
	cfg := Config{}

	cfg.AppEnv = getEnvWithFallback("APP_ENV", "", DefaultAppEnv)                // No secondary for APP_ENV
	cfg.Port = getEnvWithFallback("PORT", "", DefaultPort)                       // No secondary for PORT
	cfg.WGInterface = getEnvWithFallback("WG_INTERFACE", "", DefaultWGInterface) // No secondary for WG_INTERFACE

	// --- Server Configurations ---
	// SERVER_PRIVATE_KEY, SERVER_ENDPOINT_HOST, SERVER_ENDPOINT_PORT always come from the original .env or system env
	cfg.Server.PrivateKey = os.Getenv("SERVER_PRIVATE_KEY")
	if cfg.Server.PrivateKey == "" {
		log.Fatal("FATAL: SERVER_PRIVATE_KEY environment variable is not set. This is mandatory.")
	}

	cfg.Server.EndpointHost = os.Getenv("SERVER_ENDPOINT_HOST") // Default handled by empty string if not set
	if cfg.Server.EndpointHost == "" {
		log.Println("WARNING: SERVER_ENDPOINT_HOST is not set. Client .conf files will not have an endpoint host.")
	}
	cfg.Server.EndpointPort = getEnvWithFallback("SERVER_ENDPOINT_PORT", "", DefaultServerEndpointPort)

	// ListenPort: Prefer WG_ACTUAL_LISTEN_PORT, fallback to SERVER_LISTEN_PORT, then default
	cfg.Server.ListenPort = getEnvIntWithFallback(
		"WG_ACTUAL_LISTEN_PORT",
		"SERVER_LISTEN_PORT",
		DefaultServerListenPort,
	)

	// InterfaceAddresses: Prefer WG_ACTUAL_INTERFACE_ADDRESSES, fallback to SERVER_INTERFACE_ADDRESSES
	// Default is empty list if neither is set.
	actualInterfaceAddressesStr, actualInterfaceAddressesExists := os.LookupEnv("WG_ACTUAL_INTERFACE_ADDRESSES")
	serverInterfaceAddressesStr, serverInterfaceAddressesExists := os.LookupEnv("SERVER_INTERFACE_ADDRESSES")

	finalInterfaceAddressesStr := ""
	if actualInterfaceAddressesExists && actualInterfaceAddressesStr != "" {
		log.Printf("INFO: Using interface addresses from WG_ACTUAL_INTERFACE_ADDRESSES: '%s'", actualInterfaceAddressesStr)
		finalInterfaceAddressesStr = actualInterfaceAddressesStr
	} else if serverInterfaceAddressesExists && serverInterfaceAddressesStr != "" {
		log.Printf("INFO: Using interface addresses from SERVER_INTERFACE_ADDRESSES: '%s'", serverInterfaceAddressesStr)
		finalInterfaceAddressesStr = serverInterfaceAddressesStr
	}

	if finalInterfaceAddressesStr != "" {
		cfg.Server.InterfaceAddresses = strings.Split(finalInterfaceAddressesStr, ",")
		for i, addr := range cfg.Server.InterfaceAddresses {
			cfg.Server.InterfaceAddresses[i] = strings.TrimSpace(addr)
		}
	} else {
		log.Println("WARNING: No interface addresses found from WG_ACTUAL_INTERFACE_ADDRESSES or SERVER_INTERFACE_ADDRESSES. Interface will have no IP addresses.")
		cfg.Server.InterfaceAddresses = []string{}
	}

	// --- ClientConfig Configurations ---
	// CLIENT_CONFIG_DNS_SERVERS always comes from .env
	cfg.ClientConfig.DNSServers = getEnvWithFallback("CLIENT_CONFIG_DNS_SERVERS", "", DefaultClientConfigDNSServers)

	// MTU: Prefer WG_ACTUAL_MTU, fallback to CLIENT_CONFIG_MTU, then default
	cfg.ClientConfig.MTU = getEnvIntWithFallback(
		"WG_ACTUAL_MTU",
		"CLIENT_CONFIG_MTU",
		DefaultClientConfigMTU,
	)
	if cfg.ClientConfig.MTU < 0 { // MTU can be 0 (omit) but not negative
		log.Printf("WARNING: Effective MTU is negative (%d). Using default %d.", cfg.ClientConfig.MTU, DefaultClientConfigMTU)
		cfg.ClientConfig.MTU = DefaultClientConfigMTU
	}

	// --- Timeouts Configurations (always from .env) ---
	cfg.Timeouts.WgCmdSeconds = getEnvIntWithFallback("WG_CMD_TIMEOUT_SECONDS", "", DefaultWgCmdTimeoutSeconds)
	cfg.Timeouts.KeyGenSeconds = getEnvIntWithFallback("KEY_GEN_TIMEOUT_SECONDS", "", DefaultKeyGenTimeoutSeconds)

	// --- Derive PublicKey from PrivateKey ---
	var errDeriveKey error
	keyGenTimeout := time.Duration(cfg.Timeouts.KeyGenSeconds) * time.Second
	if keyGenTimeout <= 0 {
		keyGenTimeout = time.Duration(DefaultKeyGenTimeoutSeconds) * time.Second
		log.Printf("WARNING: KEY_GEN_TIMEOUT_SECONDS was invalid or zero, using default %d for key derivation.", DefaultKeyGenTimeoutSeconds)
	}
	cfg.Server.PublicKey, errDeriveKey = derivePublicKey(cfg.Server.PrivateKey, keyGenTimeout)
	if errDeriveKey != nil {
		log.Fatalf("FATAL: Could not derive server public key from private key: %v", errDeriveKey)
	}
	log.Printf("INFO: Successfully derived server public key (from SERVER_PRIVATE_KEY): %s...", cfg.Server.PublicKey[:min(10, len(cfg.Server.PublicKey))])

	// --- Derive other fields ---
	cfg.DerivedWgCmdTimeout = time.Duration(cfg.Timeouts.WgCmdSeconds) * time.Second
	cfg.DerivedKeyGenTimeout = keyGenTimeout

	if cfg.DerivedWgCmdTimeout <= 0 {
		log.Printf("WARNING: WG_CMD_TIMEOUT_SECONDS is invalid, using default %d seconds.", DefaultWgCmdTimeoutSeconds)
		cfg.DerivedWgCmdTimeout = time.Duration(DefaultWgCmdTimeoutSeconds) * time.Second
	}

	if cfg.Server.EndpointHost != "" && cfg.Server.EndpointPort != "" {
		cfg.DerivedServerEndpoint = fmt.Sprintf("%s:%s", cfg.Server.EndpointHost, cfg.Server.EndpointPort)
	} else if cfg.Server.EndpointHost != "" {
		cfg.DerivedServerEndpoint = cfg.Server.EndpointHost
	}

	log.Printf("--- Effective Configuration for Go App ---")
	log.Printf("AppEnv: '%s', Port: '%s', WGInterface: '%s'", cfg.AppEnv, cfg.Port, cfg.WGInterface)
	log.Printf("Server ListenPort: %d", cfg.Server.ListenPort)
	log.Printf("Server InterfaceAddresses: %v", cfg.Server.InterfaceAddresses)
	log.Printf("Server Endpoint: '%s' (Host: '%s', Port: '%s')", cfg.DerivedServerEndpoint, cfg.Server.EndpointHost, cfg.Server.EndpointPort)
	log.Printf("Server PublicKey (derived): '%s...'", cfg.Server.PublicKey[:min(10, len(cfg.Server.PublicKey))])
	log.Printf("Client DNS Servers: '%s'", cfg.ClientConfig.DNSServers)
	log.Printf("Client MTU: %d (0 means omit)", cfg.ClientConfig.MTU)
	log.Printf("Timeouts: WG Cmd: %v, Key Gen: %v", cfg.DerivedWgCmdTimeout, cfg.DerivedKeyGenTimeout)
	log.Printf("-------------------------------------------")

	return &cfg
}

// derivePublicKey uses 'wg pubkey' to derive a public key from a private key.
func derivePublicKey(privateKey string, timeout time.Duration) (string, error) {
	if privateKey == "" {
		return "", errors.New("private key is empty, cannot derive public key")
	}
	if timeout <= 0 {
		timeout = time.Duration(DefaultKeyGenTimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", "pubkey")
	cmd.Stdin = strings.NewReader(privateKey)
	pubKeyBytes, err := cmd.Output()

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("wg pubkey timed out after %v: %w", timeout, repository.ErrWgTimeout)
	}
	if err != nil {
		var execErr *exec.ExitError
		var errMsgBuilder strings.Builder
		errMsgBuilder.WriteString(fmt.Sprintf("wg pubkey command failed: %s.", err.Error()))
		if errors.As(err, &execErr) {
			if len(execErr.Stderr) > 0 {
				errMsgBuilder.WriteString(fmt.Sprintf(" Stderr: %s", string(execErr.Stderr)))
			}
		}
		return "", errors.New(errMsgBuilder.String())
	}

	publicKey := strings.TrimSpace(string(pubKeyBytes))
	if publicKey == "" {
		return "", errors.New("derived public key is empty")
	}
	return publicKey, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
