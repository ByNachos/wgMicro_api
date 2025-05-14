package server

import (
	"net/http"

	"wgMicro_api/internal/repository"

	"github.com/gin-gonic/gin"
)

// Health godoc
// @Summary      Liveness probe
// @Description  Простая проверка, что сервис запущен
// @Tags         health
// @Produce      json
// @Success      200  {object}  gin.H{"status": string}
// @Router       /healthz [get]
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readiness godoc
// @Summary      Readiness probe
// @Description  Проверка, что сервис может обращаться к утилите wg
// @Tags         health
// @Produce      json
// @Success      200  {object}  gin.H{"status": string}
// @Failure      503  {object}  gin.H{"status": string,"error": string}
// @Router       /readyz [get]
func Readiness(repo repository.Repo) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Пробуем получить список конфигураций из WireGuard
		_, err := repo.ListConfigs()
		if err != nil {
			// если wg недоступен или ошибка выполнения – возвращаем 503
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not ready",
				"error":  err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
