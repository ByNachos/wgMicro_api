package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application.
// Values are read from environment variables.
type Config struct {
	Port                string        // API port, e.g., "8080"
	LogPath             string        // Path to the log file, e.g., "logs/app.log"
	WGInterface         string        // WireGuard interface name, e.g., "wg0"
	WGConfigPath        string        // Path to the WireGuard server configuration file (e.g., "/etc/wireguard/wg0.conf")
	InterfacePublicKey  string        // Public key of the WireGuard interface (server's public key)
	InterfacePrivateKey string        // Private key of the WireGuard interface (server's private key)
	ServerEndpoint      string        // Public endpoint for clients to connect (e.g., "your.server.com:51820")
	WgCmdTimeout        time.Duration // Timeout for 'wg' command execution
	KeyGenTimeout       time.Duration // Timeout for 'wg genkey/pubkey' command execution
}

// LoadConfig loads configuration from environment variables.
// It uses godotenv.Load to load .env file if present.
func LoadConfig() *Config {
	// Attempt to load .env file, but don't fail if it's not present.
	// Environment variables will take precedence.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := &Config{
		Port:                getEnv("PORT", "8080"),
		LogPath:             getEnv("LOG_PATH", "logs/app.log"),
		WGInterface:         getEnv("WG_INTERFACE", "wg0"),
		WGConfigPath:        getEnv("WG_CONFIG_PATH", ""), // Default to empty, might need to be mandatory
		InterfacePublicKey:  getEnvOrFatal("INTERFACE_PUBLIC_KEY"),
		InterfacePrivateKey: getEnvOrFatal("INTERFACE_PRIVATE_KEY"), // Critical for server operation
		ServerEndpoint:      getEnv("SERVER_ENDPOINT", ""),          // Optional, client .conf might miss Endpoint
		WgCmdTimeout:        getEnvAsDuration("WG_CMD_TIMEOUT_SECONDS", "5") * time.Second,
		KeyGenTimeout:       getEnvAsDuration("KEY_GEN_TIMEOUT_SECONDS", "5") * time.Second,
	}

	// Validate critical configurations that were not handled by getEnvOrFatal but are essential for new features.
	if cfg.WGConfigPath == "" {
		// For Task 3, this path is essential. We can make it fatal or log a warning
		// depending on whether the feature is considered optional or core.
		// For now, let's make it a fatal error if we intend to proceed with Task 3 immediately.
		log.Fatalf("FATAL: Environment variable WG_CONFIG_PATH is not set. This is required for managing client private keys.")
	} else {
		log.Printf("INFO: WireGuard server configuration path set to: %s", cfg.WGConfigPath)
	}

	return cfg
}

// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("INFO: Environment variable %s not set, using default value: %s", key, defaultValue)
	return defaultValue
}

// getEnvOrFatal retrieves an environment variable or panics if it's not set.
func getEnvOrFatal(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("FATAL: Environment variable %s is not set.", key)
	}
	return value
}

// getEnvAsDuration retrieves an environment variable as an integer (seconds)
// and converts it to time.Duration, or returns a default duration.
// Logs a warning and uses default if parsing fails.
func getEnvAsDuration(key, defaultValueSeconds string) time.Duration {
	valueStr := getEnv(key, defaultValueSeconds)
	valueInt, err := strconv.Atoi(valueStr)
	if err != nil {
		log.Printf("WARNING: Invalid format for environment variable %s (expected integer seconds): %s. Using default %s seconds.", key, valueStr, defaultValueSeconds)
		// Attempt to parse the default value, assuming it's valid.
		defaultValueInt, _ := strconv.Atoi(defaultValueSeconds) // This should not fail if defaults are correctly set.
		return time.Duration(defaultValueInt)
	}
	if valueInt <= 0 {
		log.Printf("WARNING: Environment variable %s (timeout in seconds) must be positive: %d. Using default %s seconds.", key, valueInt, defaultValueSeconds)
		defaultValueInt, _ := strconv.Atoi(defaultValueSeconds)
		return time.Duration(defaultValueInt)
	}
	return time.Duration(valueInt)
}
