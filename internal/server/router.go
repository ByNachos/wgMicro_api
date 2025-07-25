package server

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

func NewRouter(cfgHandler *handler.ConfigHandler, repo repository.Repo) *gin.Engine {
	if cfgHandler == nil {
		logger.Logger.Fatal("ConfigHandler cannot be nil for NewRouter")
	}
	if repo == nil {
		logger.Logger.Fatal("Repository cannot be nil for NewRouter (required for readiness probe)")
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(ZapLogger(logger.Logger)) // Передаем глобальный логгер
	r.Use(cors.Default())           // Включаем CORS с настройками по умолчанию

	// Health Check Endpoints
	r.GET("/healthz", HealthLiveness)       // Убедись, что HealthLiveness определен в health.go
	r.GET("/readyz", HealthReadiness(repo)) // Убедись, что HealthReadiness определен в health.go

	// API Routes - All endpoints now use JSON body for consistency
	r.GET("/configs", cfgHandler.GetAll)                               // List all configs (no params needed)
	r.POST("/configs", cfgHandler.CreateConfig)                        // Create new config with JSON body
	r.POST("/configs/get", cfgHandler.GetConfig)                       // Get specific config with JSON body
	r.POST("/configs/update-allowed-ips", cfgHandler.UpdateAllowedIPs) // Update allowed IPs with JSON body
	r.POST("/configs/delete", cfgHandler.DeleteConfig)                 // Delete config with JSON body
	r.POST("/configs/client-file", cfgHandler.GenerateClientConfigFile) // Generate client file with JSON body
	r.POST("/configs/rotate", cfgHandler.RotatePeer)                    // Rotate peer key with JSON body

	logger.Logger.Info("Router initialized with CORS (default), all routes and middleware.")
	return r
}

func ZapLogger(log *zap.Logger) gin.HandlerFunc {
	if log == nil {
		// Это не должно произойти, если logger.Init вызывается до NewRouter
		panic("ZapLogger middleware initialized with a nil logger")
	}
	return func(c *gin.Context) {
		start := time.Now()
		// Используем переданный экземпляр логгера 'log'
		log.Info("Incoming request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("clientIP", c.ClientIP()),
			zap.String("userAgent", c.Request.UserAgent()),
		)
		c.Next()
		log.Info("Request handled",
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", time.Since(start)), // Используем "duration"
		)
	}
}
