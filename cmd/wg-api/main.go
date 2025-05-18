package main

import (
	"log" // Standard log for initial messages

	"wgMicro_api/internal/config"
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/server"
	"wgMicro_api/internal/serverkeys"
	"wgMicro_api/internal/service"

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

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /

// @schemes http https
func main() {
	appConfig := config.LoadConfig()

	logger.Init(appConfig.IsDevelopment()) // Используем appConfig.IsDevelopment()
	defer func() {
		if err := logger.Logger.Sync(); err != nil {
			log.Printf("FATAL: Failed to sync zap logger: %v\n", err)
		}
	}()
	logger.Logger.Info("Application starting...",
		zap.String("version", "1.0"), // Можешь сделать это значением из конфига или переменной сборки
		zap.String("environment", appConfig.AppEnv),
	)

	skm, err := serverkeys.NewServerKeyManager(appConfig.WGConfigPath, appConfig.KeyGenTimeout)
	if err != nil {
		logger.Logger.Fatal("Failed to initialize ServerKeyManager",
			zap.String("wgConfigPath", appConfig.WGConfigPath),
			zap.Error(err),
		)
	}
	serverPubKey, err := skm.GetServerPublicKey()
	if err != nil { // Добавил проверку ошибки и здесь
		logger.Logger.Fatal("Failed to get server public key from ServerKeyManager", zap.Error(err))
	}
	logger.Logger.Info("Server keys loaded/derived successfully", zap.String("serverPublicKey", serverPubKey))

	repo := repository.NewWGRepository(appConfig.WGInterface, appConfig.WgCmdTimeout)

	svc := service.NewConfigService(
		repo,
		serverPubKey,
		appConfig.ServerEndpoint,
		appConfig.KeyGenTimeout,
	)

	cfgHandler := handler.NewConfigHandler(svc) // Здесь должна уйти ошибка компиляции, если интерфейсы совпадают
	router := server.NewRouter(cfgHandler, repo)

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	logger.Logger.Info("Swagger UI available at /swagger/index.html")

	serverAddress := ":" + appConfig.Port
	logger.Logger.Info("Starting HTTP server...",
		zap.String("address", "http://localhost"+serverAddress), // Для лога, реальный адрес может быть другим
		zap.String("port", appConfig.Port),
	)

	if err := router.Run(serverAddress); err != nil {
		logger.Logger.Fatal("Failed to start HTTP server",
			zap.String("address", serverAddress),
			zap.Error(err),
		)
	}
}
