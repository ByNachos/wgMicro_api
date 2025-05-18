package server

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

// NewRouter создаёт Gin-движок со всеми публичными маршрутами.
func NewRouter(cfg *handler.ConfigHandler, repo repository.Repo) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(ZapLogger())

	// Liveness и Readiness
	r.GET("/healthz", Health)
	r.GET("/readyz", Readiness(repo))

	// CRUD и экспорт
	r.GET("/configs", cfg.GetAll)
	r.GET("/configs/:publicKey", cfg.GetByPublicKey)
	r.POST("/configs", cfg.CreateConfig)
	r.PUT("/configs/:publicKey/allowed-ips", cfg.UpdateAllowedIPs)
	r.DELETE("/configs/:publicKey", cfg.DeleteConfig)
	r.GET("/configs/:publicKey/file", cfg.ExportConfigFile)
	r.POST("/configs/:publicKey/rotate", cfg.RotatePeer)

	return r
}

// ZapLogger - простой middleware для логирования.
func ZapLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		logger.Logger.Info("Incoming request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
		)
		c.Next()
		logger.Logger.Info("Request handled",
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration_ms", time.Since(start)),
		)
	}
}
