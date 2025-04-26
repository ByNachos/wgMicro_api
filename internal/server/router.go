package server

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
)

// NewRouter создаёт и настраивает gin.Engine.
// Вместо *service.Service теперь принимаем *handler.ConfigHandler.
func NewRouter(cfg *handler.ConfigHandler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery()) // Recovery после паники
	r.Use(ZapLogger())    // наш zap-логгер

	// Экспорт .conf-файла
	r.GET("/configs/:publicKey/file", cfg.ExportConfigFile)

	// Стандартные CRUD
	r.GET("/configs", cfg.GetAll)
	r.GET("/configs/:publicKey", cfg.GetByPublicKey)
	r.POST("/configs", cfg.CreateConfig)
	r.PUT("/configs/:publicKey/allowed-ips", cfg.UpdateAllowedIPs)
	r.DELETE("/configs/:publicKey", cfg.DeleteConfig)

	return r
}

// ZapLogger - middleware для логирования запросов через zap.
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
