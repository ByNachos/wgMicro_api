// cmd/wg-api/main.go
package main

import (
	"log" // Standard log for initial messages

	"wgMicro_api/internal/config"
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/server"
	"wgMicro_api/internal/service"

	// "wgMicro_api/internal/serverkeys" // No longer needed

	_ "wgMicro_api/docs" // Swagger docs

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

// @title WireGuard API Service
// @version 1.0
// @description Manages WireGuard peer configurations via an HTTP API.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @host localhost:8080
// @BasePath /

// @schemes http https
func main() {
	// Load configuration using Viper
	// Note: logger.Init should ideally be called after config is loaded if logger itself needs config.
	// For now, assume logger.Init can be called before or uses its own simple config.
	// If logger needs to be configured by Viper, the order might need adjustment.
	appConfig := config.LoadConfig()

	// Initialize logger first
	logger.Init(appConfig.IsDevelopment()) // Pass development status from config

	defer func() {
		if err := logger.Logger.Sync(); err != nil {
			log.Printf("FATAL: Failed to sync zap logger: %v\n", err)
		}
	}()

	logger.Logger.Info("Application starting with loaded configuration...",
		zap.String("version", "1.0"), // Consider making this a build-time variable
		zap.String("environment", appConfig.AppEnv),
		zap.String("serverPublicKeyLoaded", appConfig.Server.PublicKey[:10]+"..."),
	)

	// ServerKeyManager is no longer needed, server's public key is in appConfig.Server.PublicKey

	repo := repository.NewWGRepository(appConfig.WGInterface, appConfig.DerivedWgCmdTimeout)

	svc := service.NewConfigService(
		repo,
		appConfig.Server.PublicKey,        // Pass derived server public key
		appConfig.DerivedServerEndpoint,   // Pass combined server endpoint
		appConfig.DerivedKeyGenTimeout,    // Pass derived key gen timeout
		appConfig.ClientConfig.DNSServers, // Pass client DNS servers
	)

	cfgHandler := handler.NewConfigHandler(svc)
	router := server.NewRouter(cfgHandler, repo) // repo is passed for readiness probe

	// Swagger UI
	// Update @host in annotations if it needs to be dynamic based on config
	// For now, localhost:8080 is hardcoded in Swaggo annotations.
	// If you change cfg.Port, Swagger UI might show the old default.
	// Swaggo can take a dynamic host via `docs.SwaggerInfo.Host = "newhost:port"`
	// but that needs to be done before `ginSwagger.WrapHandler` is called or by re-registering.
	// For now, we assume the @host annotation is sufficient for typical use.
	// Example: docs.SwaggerInfo.Host = fmt.Sprintf("localhost:%s", appConfig.Port)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	logger.Logger.Info("Swagger UI available at /swagger/index.html")

	serverAddress := ":" + appConfig.Port
	logger.Logger.Info("Starting HTTP server...",
		zap.String("address", "http://localhost"+serverAddress), // Log for convenience
		zap.String("port", appConfig.Port),
	)

	if err := router.Run(serverAddress); err != nil {
		logger.Logger.Fatal("Failed to start HTTP server",
			zap.String("address", serverAddress),
			zap.Error(err),
		)
	}
}
