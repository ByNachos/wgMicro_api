// @title        WireGuard API Service
// @version      1.0
// @description  Управление настройками WireGuard через HTTP
// @host         localhost:8080
// @BasePath     /

package main

import (
	"log"

	"github.com/joho/godotenv"

	_ "wgMicro_api/docs" // подключаем сгенерированные swagger.json/yaml
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/server"
	"wgMicro_api/internal/service"

	_ "wgMicro_api/docs" // здесь подключается сгенерированный Swagger JSON/YAML

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	// Загружаем переменные окружения из .env
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment")
	}

	// Инициализируем логгер
	logger.Init("logs/app.log")
	defer logger.Logger.Sync()

	// Создаём репозиторий, сервис, хендлер
	repo := repository.NewWGRepository("wg0")
	svc := service.NewConfigService(repo)
	cfgHandler := handler.NewConfigHandler(svc)

	r := server.NewRouter(cfgHandler, repo)

	// Регистрируем маршрут для Swagger-UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Запуск сервера
	r.Run(":8080")

}
