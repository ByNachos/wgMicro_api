package main

import (
	"log"
	"os"

	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/server"
	"wgMicro_api/internal/service"

	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment")
	}

	// Логгер
	logger.Init(os.Getenv("LOG_PATH"))
	defer logger.Logger.Sync()

	// WireGuard-интерфейс
	repo := repository.NewWGRepository(os.Getenv("WG_INTERFACE"))
	svc := service.NewConfigService(repo)
	cfgHandler := handler.NewConfigHandler(svc)

	// Маршрутизатор без авторизации
	r := server.NewRouter(cfgHandler, repo)

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r.Run(":" + port)
}
