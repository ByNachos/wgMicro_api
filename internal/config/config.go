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
	Port           string        // API port, e.g., "8080"
	LogPath        string        // Path to the log file, e.g., "logs/app.log"
	WGInterface    string        // WireGuard interface name, e.g., "wg0"
	WGConfigPath   string        // Path to the WireGuard server configuration file (e.g., "/etc/wireguard/wg0.conf") - MANDATORY
	ServerEndpoint string        // Public endpoint for clients to connect (e.g., "your.server.com:51820") - Optional
	WgCmdTimeout   time.Duration // Timeout for 'wg' command execution
	KeyGenTimeout  time.Duration // Timeout for 'wg genkey/pubkey' command execution (used by services)
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	cfg := &Config{
		Port:           getEnv("PORT", "8080"),
		LogPath:        getEnv("LOG_PATH", "logs/app.log"),
		WGInterface:    getEnv("WG_INTERFACE", "wg0"),
		WGConfigPath:   getEnvOrFatal("WG_CONFIG_PATH"), // Now strictly mandatory for server keys
		ServerEndpoint: getEnv("SERVER_ENDPOINT", ""),
		WgCmdTimeout:   getEnvAsDuration("WG_CMD_TIMEOUT_SECONDS", "5") * time.Second,
		KeyGenTimeout:  getEnvAsDuration("KEY_GEN_TIMEOUT_SECONDS", "5") * time.Second, // This timeout is for client key gen
	}
	// WG_CONFIG_PATH is now fetched with getEnvOrFatal, so explicit check below is not needed.
	// log.Printf("INFO: WireGuard server configuration path set to: %s", cfg.WGConfigPath)
	return cfg
}

// ... (getEnv, getEnvOrFatal, getEnvAsDuration - as before, but getEnvOrFatal now used for WG_CONFIG_PATH)
// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("INFO: Environment variable %s not set, using default value: '%s'", key, defaultValue)
	return defaultValue
}

// getEnvOrFatal retrieves an environment variable or panics if it's not set.
func getEnvOrFatal(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("FATAL: Environment variable %s is not set and is required.", key)
	}
	if value == "" {
		log.Fatalf("FATAL: Environment variable %s is set but is empty, which is not allowed as it's required.", key)
	}
	log.Printf("INFO: Environment variable %s loaded with value: '%s'", key, value)
	return value
}

// getEnvAsDuration retrieves an environment variable as an integer (seconds)
// and converts it to time.Duration, or returns a default duration.
// Logs a warning and uses default if parsing fails or value is non-positive.
func getEnvAsDuration(key, defaultValueSeconds string) time.Duration {
	valueStr := getEnv(key, defaultValueSeconds)
	valueInt, err := strconv.Atoi(valueStr)
	defaultDurationInt, _ := strconv.Atoi(defaultValueSeconds) // Assuming default is always valid

	if err != nil {
		log.Printf("WARNING: Invalid format for environment variable %s (expected integer seconds): '%s'. Using default %d seconds.", key, valueStr, defaultDurationInt)
		return time.Duration(defaultDurationInt) * time.Second
	}
	if valueInt <= 0 {
		log.Printf("WARNING: Environment variable %s (timeout in seconds) must be positive: %d. Using default %d seconds.", key, valueInt, defaultDurationInt)
		return time.Duration(defaultDurationInt) * time.Second
	}
	return time.Duration(valueInt) * time.Second
}
