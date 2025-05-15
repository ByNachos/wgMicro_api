package handler

import (
	"errors"
	"fmt"
	"net/http"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ServiceInterface описывает методы бизнес-логики для handler-а.
type ServiceInterface interface {
	GetAll() ([]domain.Config, error)
	Get(publicKey string) (*domain.Config, error)
	Create(cfg domain.Config) error
	UpdateAllowedIPs(publicKey string, ips []string) error
	Delete(publicKey string) error
	BuildClientConfig(cfg *domain.Config) (string, error)
	Rotate(publicKey string) (*domain.Config, error)
}

// ConfigHandler хранит ссылку на сервис.
type ConfigHandler struct {
	svc ServiceInterface
}

// NewConfigHandler создаёт новый handler.
func NewConfigHandler(svc ServiceInterface) *ConfigHandler {
	return &ConfigHandler{svc: svc}
}

// GetAll godoc
// @Summary      List all peer configurations
// @Description  Возвращает список всех peer-конфигураций
// @Tags         configs
// @Produce      json
// @Success      200  {array}   domain.Config
// @Failure      503  {object}  domain.ErrorResponse  "WireGuard недоступен"
// @Failure      500  {object}  domain.ErrorResponse  "internal error"
// @Router       /configs [get]
func (h *ConfigHandler) GetAll(c *gin.Context) {
	configs, err := h.svc.GetAll()
	if err != nil {
		logger.Logger.Error("Failed to list configs", zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, configs)
}

// GetByPublicKey godoc
// @Summary      Get configuration by public key
// @Description  Возвращает информацию по публичному ключу
// @Tags         configs
// @Produce      json
// @Param        publicKey  path      string  true  "Public key peer'а"
// @Success      200        {object}  domain.Config
// @Failure      503        {object}  domain.ErrorResponse  "WireGuard недоступен"
// @Failure      404        {object}  domain.ErrorResponse  "config not found"
// @Router       /configs/{publicKey} [get]
func (h *ConfigHandler) GetByPublicKey(c *gin.Context) {
	key := c.Param("publicKey")
	cfg, err := h.svc.Get(key)
	if err != nil {
		logger.Logger.Error("Config not found", zap.String("publicKey", key), zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusNotFound, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// CreateConfig godoc
// @Summary      Create new peer configuration
// @Description  Создаёт новую конфигурацию для peer-а
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        config  body      domain.Config          true  "New configuration"
// @Success      201     {string}  string                 "created"
// @Failure      503     {object}  domain.ErrorResponse  "WireGuard недоступен"
// @Failure      400     {object}  domain.ErrorResponse  "invalid input"
// @Failure      500     {object}  domain.ErrorResponse  "internal error"
// @Router       /configs [post]
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	var input domain.Config
	if err := c.ShouldBindJSON(&input); err != nil {
		logger.Logger.Error("Invalid input for create config", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: err.Error()})
		return
	}
	if err := h.svc.Create(input); err != nil {
		logger.Logger.Error("Failed to create config", zap.String("publicKey", input.PublicKey), zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.Status(http.StatusCreated)
}

// UpdateAllowedIPs godoc
// @Summary      Update allowed IPs for a peer
// @Description  Заменяет список разрешённых IP-адресов
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        publicKey   path      string                   true  "Public key peer'а"
// @Param        allowedIps  body      domain.AllowedIpsUpdate  true  "New allowed IPs"
// @Success      200         {string}  string                   "updated"
// @Failure      503         {object}  domain.ErrorResponse     "WireGuard недоступен"
// @Failure      400         {object}  domain.ErrorResponse     "invalid input"
// @Failure      500         {object}  domain.ErrorResponse     "internal error"
// @Router       /configs/{publicKey}/allowed-ips [put]
func (h *ConfigHandler) UpdateAllowedIPs(c *gin.Context) {
	key := c.Param("publicKey")
	var body struct {
		AllowedIps []string `json:"allowedIps"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		logger.Logger.Error("Invalid input for update allowed-ips", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: err.Error()})
		return
	}
	if err := h.svc.UpdateAllowedIPs(key, body.AllowedIps); err != nil {
		logger.Logger.Error("Failed to update allowed-ips", zap.String("publicKey", key), zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.Status(http.StatusOK)
}

// DeleteConfig godoc
// @Summary      Delete a peer configuration
// @Description  Удаляет конфигурацию peer-а по публичному ключу
// @Tags         configs
// @Produce      json
// @Param        publicKey  path      string  true  "Public key peer'а"
// @Success      204        {string}  string  "deleted"
// @Failure      503        {object}  domain.ErrorResponse  "WireGuard недоступен"
// @Failure      500        {object}  domain.ErrorResponse  "internal error"
// @Router       /configs/{publicKey} [delete]
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	key := c.Param("publicKey")
	if err := h.svc.Delete(key); err != nil {
		logger.Logger.Error("Failed to delete config", zap.String("publicKey", key), zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// ExportConfigFile godoc
// @Summary      Export WireGuard config file
// @Description  Генерирует и возвращает .conf-файл для клиента
// @Tags         configs
// @Produce      text/plain
// @Param        publicKey  path      string  true  "Public key peer'а"
// @Success      200        {file}    string  "WireGuard .conf file"
// @Failure      503        {object}  domain.ErrorResponse  "WireGuard недоступен"
// @Failure      404        {object}  domain.ErrorResponse  "config not found"
// @Router       /configs/{publicKey}/file [get]
func (h *ConfigHandler) ExportConfigFile(c *gin.Context) {
	key := c.Param("publicKey")
	cfg, err := h.svc.Get(key)
	if err != nil {
		logger.Logger.Error("Config not found for export", zap.String("publicKey", key), zap.Error(err))
		if errors.Is(err, repository.ErrWgTimeout) {
			c.JSON(http.StatusServiceUnavailable, domain.ErrorResponse{Error: err.Error()})
		} else {
			c.JSON(http.StatusNotFound, domain.ErrorResponse{Error: err.Error()})
		}
		return
	}
	text, err := h.svc.BuildClientConfig(cfg)
	if err != nil {
		logger.Logger.Error("Failed to build client config", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		return
	}
	c.Header("Content-Type", "application/text")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.conf\"", key))
	c.String(http.StatusOK, text)
}

// internal/handler/config.go
// RotatePeer godoc
// @Summary      Rotate peer key
// @Description  Удаляет пир по publicKey и создаёт нового с теми же AllowedIps
// @Tags         configs
// @Produce      json
// @Param        publicKey  path      string         true  "Old public key"
// @Success      200        {object}  domain.Config "новая конфигурация"
// @Failure      400        {object}  domain.ErrorResponse
// @Failure      500        {object}  domain.ErrorResponse
// @Router       /configs/{publicKey}/rotate [post]
func (h *ConfigHandler) RotatePeer(c *gin.Context) {
	key := c.Param("publicKey")
	newCfg, err := h.svc.Rotate(key)
	if err != nil {
		logger.Logger.Error("Failed to rotate peer", zap.Error(err))
		c.JSON(http.StatusInternalServerError, domain.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, newCfg)
}
