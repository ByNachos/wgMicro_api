// internal/config/config.go
package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os" // Для os.Getenv()
	"os/exec"
	"strconv" // Для парсинга портов и таймаутов
	"strings"
	"time"

	"wgMicro_api/internal/repository"
	// Используем Viper для дефолтов и, возможно, чтения .env файла локально
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
	DefaultServerListenPort       = 51820
	DefaultClientConfigDNSServers = "" // Одна строка
)

type Config struct {
	AppEnv      string
	Port        string
	WGInterface string

	Server struct {
		PrivateKey         string
		PublicKey          string // Derived
		EndpointHost       string
		EndpointPort       string
		ListenPort         int
		InterfaceAddresses []string // Parsed from string
	}

	ClientConfig struct {
		DNSServers string // Single string
	}

	Timeouts struct {
		WgCmdSeconds  int
		KeyGenSeconds int
	}

	DerivedWgCmdTimeout   time.Duration
	DerivedKeyGenTimeout  time.Duration
	DerivedServerEndpoint string
}

func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.AppEnv) == EnvDevelopment
}

// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvInt retrieves an environment variable as int or returns a default value.
func getEnvInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	valueInt, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("WARNING: Invalid integer value for %s: '%s'. Using default %d. Error: %v", key, valueStr, defaultValue, err)
		return defaultValue
	}
	return valueInt
}

func LoadConfig() *Config {
	cfg := Config{}

	// --- Load simple string configurations directly from environment or use defaults ---
	cfg.AppEnv = getEnv("APP_ENV", DefaultAppEnv)
	cfg.Port = getEnv("PORT", DefaultPort)
	cfg.WGInterface = getEnv("WG_INTERFACE", DefaultWGInterface)

	// --- Server Configurations ---
	cfg.Server.PrivateKey = os.Getenv("SERVER_PRIVATE_KEY") // Mandatory, no default in code
	cfg.Server.EndpointHost = getEnv("SERVER_ENDPOINT_HOST", "")
	cfg.Server.EndpointPort = getEnv("SERVER_ENDPOINT_PORT", DefaultServerEndpointPort)
	cfg.Server.ListenPort = getEnvInt("SERVER_LISTEN_PORT", DefaultServerListenPort)

	interfaceAddressesStr := getEnv("SERVER_INTERFACE_ADDRESSES", "")
	if interfaceAddressesStr != "" {
		cfg.Server.InterfaceAddresses = strings.Split(interfaceAddressesStr, ",")
		for i, addr := range cfg.Server.InterfaceAddresses {
			cfg.Server.InterfaceAddresses[i] = strings.TrimSpace(addr)
		}
	} else {
		log.Println("WARNING: SERVER_INTERFACE_ADDRESSES is not set or empty.")
		cfg.Server.InterfaceAddresses = []string{}
	}

	// --- ClientConfig Configurations ---
	cfg.ClientConfig.DNSServers = getEnv("CLIENT_CONFIG_DNS_SERVERS", DefaultClientConfigDNSServers)

	// --- Timeouts Configurations ---
	cfg.Timeouts.WgCmdSeconds = getEnvInt("WG_CMD_TIMEOUT_SECONDS", DefaultWgCmdTimeoutSeconds)
	cfg.Timeouts.KeyGenSeconds = getEnvInt("KEY_GEN_TIMEOUT_SECONDS", DefaultKeyGenTimeoutSeconds)

	// --- Validate critical configurations ---
	if cfg.Server.PrivateKey == "" {
		log.Fatal("FATAL: SERVER_PRIVATE_KEY environment variable is not set. This is mandatory.")
	}
	if cfg.Server.EndpointHost == "" { // Was a warning, can be critical if needed
		log.Println("WARNING: SERVER_ENDPOINT_HOST is not set. Client .conf files will not have an endpoint host.")
	}

	// --- Derive PublicKey from PrivateKey ---
	var errDeriveKey error
	keyGenTimeout := time.Duration(cfg.Timeouts.KeyGenSeconds) * time.Second
	if keyGenTimeout <= 0 { // Ensure valid timeout for derivation
		keyGenTimeout = time.Duration(DefaultKeyGenTimeoutSeconds) * time.Second
		log.Printf("WARNING: KEY_GEN_TIMEOUT_SECONDS was invalid or zero, using default %d for key derivation.", DefaultKeyGenTimeoutSeconds)
	}
	cfg.Server.PublicKey, errDeriveKey = derivePublicKey(cfg.Server.PrivateKey, keyGenTimeout)
	if errDeriveKey != nil {
		log.Fatalf("FATAL: Could not derive server public key from private key: %v", errDeriveKey)
	}
	log.Printf("INFO: Successfully derived server public key: %s...", cfg.Server.PublicKey[:min(10, len(cfg.Server.PublicKey))])

	// --- Derive other fields ---
	cfg.DerivedWgCmdTimeout = time.Duration(cfg.Timeouts.WgCmdSeconds) * time.Second
	cfg.DerivedKeyGenTimeout = keyGenTimeout // Use the (potentially corrected) keyGenTimeout

	if cfg.DerivedWgCmdTimeout <= 0 {
		log.Printf("WARNING: WG_CMD_TIMEOUT_SECONDS is invalid, using default %d seconds.", DefaultWgCmdTimeoutSeconds)
		cfg.DerivedWgCmdTimeout = time.Duration(DefaultWgCmdTimeoutSeconds) * time.Second
	}
	// DerivedKeyGenTimeout уже проверен выше

	if cfg.Server.EndpointHost != "" && cfg.Server.EndpointPort != "" {
		cfg.DerivedServerEndpoint = fmt.Sprintf("%s:%s", cfg.Server.EndpointHost, cfg.Server.EndpointPort)
	} else if cfg.Server.EndpointHost != "" {
		cfg.DerivedServerEndpoint = cfg.Server.EndpointHost
	}

	// --- Final logging of loaded configuration ---
	log.Printf("INFO: Configuration loaded. AppEnv: '%s', Port: '%s', WGInterface: '%s'", cfg.AppEnv, cfg.Port, cfg.WGInterface)
	log.Printf("INFO: Server Endpoint: '%s'", cfg.DerivedServerEndpoint)
	log.Printf("INFO: Client DNS Server (single string): '%s'", cfg.ClientConfig.DNSServers)
	log.Printf("INFO: Timeouts: WG Cmd: %v, Key Gen: %v", cfg.DerivedWgCmdTimeout, cfg.DerivedKeyGenTimeout)

	return &cfg
}

// derivePublicKey uses 'wg pubkey' to derive a public key from a private key.
func derivePublicKey(privateKey string, timeout time.Duration) (string, error) {
	if privateKey == "" {
		return "", errors.New("private key is empty, cannot derive public key")
	}
	if timeout <= 0 { // Should have been caught earlier, but defensive check
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

// min is a helper function.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
