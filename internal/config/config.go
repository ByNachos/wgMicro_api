// internal/config/config.go
package config

import (
	"context"
	"errors"
	"fmt"
	"log" // Standard log for initial messages, Viper might use its own logging for loading issues
	"os/exec"
	"strings"
	"time"

	"wgMicro_api/internal/repository" // For repository.ErrWgTimeout

	"github.com/spf13/viper" // Assuming logger.Logger is initialized before LoadConfig if used here
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
	DefaultServerEndpointPort     = "51820" // Common WireGuard port
	DefaultServerListenPort       = 51820   // Common WireGuard port
	DefaultClientConfigDNSServers = ""      // No DNS by default
)

// Config holds all configuration for the application, loaded via Viper.
type Config struct {
	AppEnv      string `mapstructure:"APP_ENV"`
	Port        string `mapstructure:"PORT"`
	WGInterface string `mapstructure:"WG_INTERFACE"` // e.g., "wg0"

	Server struct {
		PrivateKey         string   `mapstructure:"PRIVATE_KEY"` // Server's own WireGuard private key
		PublicKey          string   // Derived from PrivateKey, not directly from env
		EndpointHost       string   `mapstructure:"ENDPOINT_HOST"`       // Publicly reachable hostname/IP for clients
		EndpointPort       string   `mapstructure:"ENDPOINT_PORT"`       // Publicly reachable port for clients
		ListenPort         int      `mapstructure:"LISTEN_PORT"`         // Port WireGuard server listens on (informational)
		InterfaceAddresses []string `mapstructure:"INTERFACE_ADDRESSES"` // Server's WireGuard interface IP addresses (e.g., ["10.0.0.1/24", "fd00::1/64"])
	} `mapstructure:"SERVER"`

	ClientConfig struct {
		DNSServers []string `mapstructure:"DNS_SERVERS"` // DNS servers for client .conf files (e.g., ["1.1.1.1", "8.8.8.8"])
	} `mapstructure:"CLIENT_CONFIG"`

	Timeouts struct {
		WgCmdSeconds  int `mapstructure:"WG_CMD_TIMEOUT_SECONDS"`
		KeyGenSeconds int `mapstructure:"KEY_GEN_TIMEOUT_SECONDS"`
	} `mapstructure:"TIMEOUTS"`

	// Derived fields (not from mapstructure, but set after loading)
	DerivedWgCmdTimeout   time.Duration
	DerivedKeyGenTimeout  time.Duration
	DerivedServerEndpoint string // Combined Server.EndpointHost and Server.EndpointPort
}

// IsDevelopment checks if the application is running in development environment.
func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.AppEnv) == EnvDevelopment
}

// LoadConfig loads configuration using Viper from .env file and environment variables.
func LoadConfig() *Config {
	v := viper.New()

	// --- 1. Set up Viper to read .env file ---
	// Viper will look for a file named ".env" in the current working directory.
	v.SetConfigFile(".env")
	v.SetConfigType("env") // or "dotenv"
	v.AutomaticEnv()       // Read in environment variables that match
	v.AllowEmptyEnv(true)  // Allow empty env vars to be set (Viper might treat them as unset otherwise)

	// Attempt to read the .env file.
	// If it's not found, Viper will rely solely on environment variables and defaults.
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Println("INFO: No .env file found, relying on environment variables and defaults.")
		} else {
			// Some other error occurred reading the .env file
			log.Printf("WARNING: Could not read .env file: %v. Relying on environment variables and defaults.", err)
		}
	} else {
		log.Println("INFO: Successfully loaded configuration from .env file.")
	}

	// --- 2. Set default values ---
	// These defaults will be used if the corresponding variable is not found in .env or environment.
	v.SetDefault("APP_ENV", DefaultAppEnv)
	v.SetDefault("PORT", DefaultPort)
	v.SetDefault("WG_INTERFACE", DefaultWGInterface)
	v.SetDefault("TIMEOUTS.WG_CMD_TIMEOUT_SECONDS", DefaultWgCmdTimeoutSeconds)
	v.SetDefault("TIMEOUTS.KEY_GEN_TIMEOUT_SECONDS", DefaultKeyGenTimeoutSeconds)

	// Server defaults
	// SERVER.PRIVATE_KEY must be provided, no default.
	v.SetDefault("SERVER.ENDPOINT_PORT", DefaultServerEndpointPort)
	v.SetDefault("SERVER.LISTEN_PORT", DefaultServerListenPort)
	// SERVER.INTERFACE_ADDRESSES and SERVER.ENDPOINT_HOST are better set explicitly.

	// ClientConfig defaults
	v.SetDefault("CLIENT_CONFIG.DNS_SERVERS", DefaultClientConfigDNSServers) // Expecting comma-separated string in .env, will be split

	// --- 3. Unmarshal configuration into struct ---
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		log.Fatalf("FATAL: Unable to decode configuration into struct: %v", err)
	}

	// --- 4. Handle specific parsing for slice/map types from string env vars ---
	// Viper's direct unmarshaling of complex types from simple string env vars can be tricky.
	// It's often more reliable to read them as strings and then parse them manually.

	// SERVER.INTERFACE_ADDRESSES (expecting comma-separated string)
	if interfaceAddressesStr := v.GetString("SERVER.INTERFACE_ADDRESSES"); interfaceAddressesStr != "" {
		cfg.Server.InterfaceAddresses = strings.Split(interfaceAddressesStr, ",")
		for i, addr := range cfg.Server.InterfaceAddresses {
			cfg.Server.InterfaceAddresses[i] = strings.TrimSpace(addr)
		}
	} else {
		// It's good practice to have at least one address for the server interface.
		// However, this API's direct responsibility for this is now informational.
		// Log a warning if it's not set, as it's unusual.
		log.Println("WARNING: SERVER.INTERFACE_ADDRESSES is not set. This is unusual for a WireGuard server.")
		cfg.Server.InterfaceAddresses = []string{}
	}

	// CLIENT_CONFIG.DNS_SERVERS (expecting comma-separated string)
	if dnsServersStr := v.GetString("CLIENT_CONFIG.DNS_SERVERS"); dnsServersStr != "" {
		cfg.ClientConfig.DNSServers = strings.Split(dnsServersStr, ",")
		for i, srv := range cfg.ClientConfig.DNSServers {
			cfg.ClientConfig.DNSServers[i] = strings.TrimSpace(srv)
		}
	} else {
		cfg.ClientConfig.DNSServers = []string{} // Ensure it's an empty slice not nil
	}

	// --- 5. Validate critical configurations ---
	if cfg.Server.PrivateKey == "" {
		log.Fatal("FATAL: SERVER.PRIVATE_KEY is not set in the environment or .env file. This is mandatory.")
	}
	if cfg.Server.EndpointHost == "" {
		log.Println("WARNING: SERVER.ENDPOINT_HOST is not set. Client .conf files will not have an endpoint host.")
	}

	// --- 6. Derive PublicKey from PrivateKey ---
	var errDeriveKey error
	cfg.Server.PublicKey, errDeriveKey = derivePublicKey(cfg.Server.PrivateKey, time.Duration(cfg.Timeouts.KeyGenSeconds)*time.Second)
	if errDeriveKey != nil {
		log.Fatalf("FATAL: Could not derive server public key from private key: %v", errDeriveKey)
	}
	log.Printf("INFO: Successfully derived server public key: %s...", cfg.Server.PublicKey[:min(10, len(cfg.Server.PublicKey))])

	// --- 7. Derive other fields ---
	cfg.DerivedWgCmdTimeout = time.Duration(cfg.Timeouts.WgCmdSeconds) * time.Second
	cfg.DerivedKeyGenTimeout = time.Duration(cfg.Timeouts.KeyGenSeconds) * time.Second

	if cfg.DerivedWgCmdTimeout <= 0 {
		log.Printf("WARNING: WG_CMD_TIMEOUT_SECONDS is invalid (%d), using default %d seconds.", cfg.Timeouts.WgCmdSeconds, DefaultWgCmdTimeoutSeconds)
		cfg.DerivedWgCmdTimeout = time.Duration(DefaultWgCmdTimeoutSeconds) * time.Second
	}
	if cfg.DerivedKeyGenTimeout <= 0 {
		log.Printf("WARNING: KEY_GEN_TIMEOUT_SECONDS is invalid (%d), using default %d seconds.", cfg.Timeouts.KeyGenSeconds, DefaultKeyGenTimeoutSeconds)
		cfg.DerivedKeyGenTimeout = time.Duration(DefaultKeyGenTimeoutSeconds) * time.Second
	}

	// Combine EndpointHost and EndpointPort for easy use
	if cfg.Server.EndpointHost != "" && cfg.Server.EndpointPort != "" {
		cfg.DerivedServerEndpoint = fmt.Sprintf("%s:%s", cfg.Server.EndpointHost, cfg.Server.EndpointPort)
	} else if cfg.Server.EndpointHost != "" { // Port might be optional if host includes it or uses default
		cfg.DerivedServerEndpoint = cfg.Server.EndpointHost
	}
	// If only port is set, it's not enough for a full endpoint.

	// --- 8. Final logging of loaded configuration ---
	// Use logger.Logger if initialized and available, otherwise standard log.
	// For simplicity in this standalone LoadConfig, using standard log.
	// In main.go, you'd initialize your Zap logger before calling LoadConfig.
	log.Printf("INFO: Configuration loaded successfully. AppEnv: '%s', Port: '%s', WGInterface: '%s'", cfg.AppEnv, cfg.Port, cfg.WGInterface)
	log.Printf("INFO: Server Endpoint for clients: '%s'", cfg.DerivedServerEndpoint)
	log.Printf("INFO: Client DNS Servers: %v", cfg.ClientConfig.DNSServers)
	log.Printf("INFO: WG Command Timeout: %v, Key Gen Timeout: %v", cfg.DerivedWgCmdTimeout, cfg.DerivedKeyGenTimeout)

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
	pubKeyBytes, err := cmd.Output() // cmd.Output() captures only stdout

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("wg pubkey timed out after %v: %w", timeout, repository.ErrWgTimeout)
	}
	if err != nil {
		// Try to get more detailed error from Stderr if exec.ExitError
		var execErr *exec.ExitError
		var errMsgBuilder strings.Builder
		errMsgBuilder.WriteString(fmt.Sprintf("wg pubkey command failed: %s.", err.Error()))
		if errors.As(err, &execErr) {
			// Stderr is often more informative for 'wg' command failures
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

// Helper for logging public key
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
