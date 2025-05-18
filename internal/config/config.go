package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"
)

// Config holds all configuration for the application.
type Config struct {
	AppEnv         string        // Application environment (e.g., "development", "production")
	Port           string        // API port, e.g., "8080"
	WGInterface    string        // WireGuard interface name, e.g., "wg0"
	WGConfigPath   string        // Path to the WireGuard server configuration file - MANDATORY (used by ServerKeyManager)
	ServerEndpoint string        // Public endpoint for clients to connect - Optional
	WgCmdTimeout   time.Duration // Timeout for 'wg' command execution
	KeyGenTimeout  time.Duration // Timeout for 'wg genkey/pubkey' command execution (for client keys by service)
}

// IsDevelopment checks if the application is running in development environment.
func (c *Config) IsDevelopment() bool {
	return strings.ToLower(c.AppEnv) == EnvDevelopment
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("INFO: No .env file found or failed to load, using system environment variables")
	}

	cfg := &Config{
		AppEnv:         getEnv("APP_ENV", EnvDevelopment),
		Port:           getEnv("PORT", "8080"),
		WGInterface:    getEnv("WG_INTERFACE", "wg0"),
		WGConfigPath:   getEnvOrFatal("WG_CONFIG_PATH"), // This path is critical
		ServerEndpoint: getEnv("SERVER_ENDPOINT", ""),
		WgCmdTimeout:   getEnvAsDuration("WG_CMD_TIMEOUT_SECONDS", "5") * time.Second,
		KeyGenTimeout:  getEnvAsDuration("KEY_GEN_TIMEOUT_SECONDS", "5") * time.Second,
	}

	cfg.AppEnv = strings.ToLower(cfg.AppEnv)
	validEnvs := map[string]bool{EnvDevelopment: true, EnvProduction: true, EnvTest: true}
	if !validEnvs[cfg.AppEnv] {
		log.Printf("WARNING: Invalid APP_ENV value: '%s'. Defaulting to '%s'. Expected one of: %s, %s, %s.",
			cfg.AppEnv, EnvDevelopment, EnvDevelopment, EnvProduction, EnvTest)
		cfg.AppEnv = EnvDevelopment
	}
	log.Printf("INFO: Application environment set to: '%s'", cfg.AppEnv)
	log.Printf("INFO: WireGuard server configuration path set to: '%s'", cfg.WGConfigPath)

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	log.Printf("INFO: Environment variable '%s' not set, using default value: '%s'", key, defaultValue)
	return defaultValue
}

func getEnvOrFatal(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("FATAL: Environment variable '%s' is not set and is required.", key)
	}
	if value == "" {
		log.Fatalf("FATAL: Environment variable '%s' is set but is empty, which is not allowed as it's required.", key)
	}
	log.Printf("INFO: Environment variable '%s' loaded with value: '%s'", key, value)
	return value
}

func getEnvAsDuration(key, defaultValueSeconds string) time.Duration {
	valueStr := getEnv(key, defaultValueSeconds)
	valueInt, errConv := strconv.Atoi(valueStr)
	defaultDurationInt, _ := strconv.Atoi(defaultValueSeconds)

	if errConv != nil {
		log.Printf("WARNING: Invalid format for environment variable '%s' (expected integer seconds): '%s'. Using default %d seconds.", key, valueStr, defaultDurationInt)
		return time.Duration(defaultDurationInt) * time.Second
	}
	if valueInt <= 0 {
		log.Printf("WARNING: Environment variable '%s' (timeout in seconds) must be positive: %d. Using default %d seconds.", key, valueInt, defaultDurationInt)
		return time.Duration(defaultDurationInt) * time.Second
	}
	return time.Duration(valueInt) * time.Second
}
