package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
)

// ServiceInterface описывает набор методов, который нужен handler-у.
// Любая реализация с таким же набором методов может быть прокинута в NewConfigHandler.
type ServiceInterface interface {
	GetAll() ([]domain.Config, error)
	Get(publicKey string) (*domain.Config, error)
	Create(cfg domain.Config) error
	UpdateAllowedIPs(publicKey string, ips []string) error
	Delete(publicKey string) error
	BuildClientConfig(cfg *domain.Config) (string, error)
}

// ConfigHandler теперь хранит интерфейс, а не конкретный *service.ConfigService
type ConfigHandler struct {
	svc ServiceInterface
}

// AllowedIpsUpdate представляет запрос на обновление разрешенных IP
type AllowedIpsUpdate struct {
	AllowedIps []string `json:"allowedIps" example:"10.0.0.2/32,192.168.1.0/24"`
}

// NewConfigHandler принимает любой объект, реализующий ServiceInterface
func NewConfigHandler(svc ServiceInterface) *ConfigHandler {
	return &ConfigHandler{svc: svc}
}

// GetAll godoc
// @Summary      List all peer configurations
// @Description  Возвращает список всех peer-конфигураций
// @Tags         configs
// @Produce      json
// @Success      200  {array}   domain.Config
// @Failure 400 {object} domain.ErrorResponse "bad request"
// @Failure 500 {object} domain.ErrorResponse "internal error"
// @Router       /configs [get]
// GetAll возвращает все peer-конфигурации в виде JSON массива.
func (h *ConfigHandler) GetAll(c *gin.Context) {
	configs, err := h.svc.GetAll()
	if err != nil {
		logger.Logger.Error("Failed to list configs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, configs)
}

// GetByPublicKey godoc
// @Summary      Get configuration by public key
// @Description  Возвращает информацию об одной конфигурации WireGuard по публичному ключу
// @Tags         configs
// @Produce      json
// @Param        publicKey   path      string  true  "Public key peer'а"
// @Success      200         {object}  domain.Config
// @Failure      404         {object}  domain.ErrorResponse  "конфигурация не найдена"
// @Failure      500         {object}  domain.ErrorResponse  "внутренняя ошибка"
// @Router       /configs/{publicKey} [get]
// GetByPublicKey возвращает одну конфигурацию по publicKey.
func (h *ConfigHandler) GetByPublicKey(c *gin.Context) {
	key := c.Param("publicKey")
	cfg, err := h.svc.Get(key)
	if err != nil {
		logger.Logger.Error("Config not found", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// CreateConfig godoc
// @Summary      Create a new peer configuration
// @Description  Создает новую конфигурацию для peer-а, используя publicKey и список разрешенных IP
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        config  body      domain.Config  true  "Данные новой конфигурации"
// @Success      201     {string}  string         "создано успешно"
// @Failure      400     {object}  domain.ErrorResponse  "неверный формат запроса"
// @Failure      500     {object}  domain.ErrorResponse  "ошибка создания конфигурации"
// @Router       /configs [post]
// CreateConfig создаёт новую конфигурацию для peer.
// Ожидает JSON body вида domain.Config (publicKey + allowedIps и опционально privateKey для клиента).
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	var input domain.Config
	if err := c.ShouldBindJSON(&input); err != nil {
		logger.Logger.Error("Invalid input for create config", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Create(input); err != nil {
		logger.Logger.Error("Failed to create config", zap.String("publicKey", input.PublicKey), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusCreated)
}

// UpdateAllowedIPs godoc
// @Summary      Update allowed IPs for a peer
// @Description  Заменяет список разрешенных IP-адресов для указанного peer-а
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        publicKey   path      string                  true  "Public key peer'а"
// @Param        allowedIps  body      domain.AllowedIpsUpdate true  "Список разрешенных IP-адресов"
// @Success      200         {string}  string                  "обновлено успешно"
// @Failure      400         {object}  domain.ErrorResponse   "неверный формат запроса"
// @Failure      404         {object}  domain.ErrorResponse   "peer не найден"
// @Failure      500         {object}  domain.ErrorResponse   "ошибка обновления"
// @Router       /configs/{publicKey}/allowed-ips [put]
// UpdateAllowedIPs заменяет список allowed-ips у peer-а.
func (h *ConfigHandler) UpdateAllowedIPs(c *gin.Context) {
	key := c.Param("publicKey")
	var body struct {
		AllowedIps []string `json:"allowedIps"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		logger.Logger.Error("Invalid input for update allowed-ips", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.UpdateAllowedIPs(key, body.AllowedIps); err != nil {
		logger.Logger.Error("Failed to update allowed-ips", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// DeleteConfig godoc
// @Summary      Delete a peer configuration
// @Description  Полностью удаляет конфигурацию peer-а по публичному ключу
// @Tags         configs
// @Produce      json
// @Param        publicKey  path      string  true  "Public key peer'а"
// @Success      204        {string}  string  "удалено успешно"
// @Failure      404        {object}  domain.ErrorResponse  "peer не найден"
// @Failure      500        {object}  domain.ErrorResponse  "ошибка удаления"
// @Router       /configs/{publicKey} [delete]
// DeleteConfig удаляет peer-конфигурацию целиком.
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	key := c.Param("publicKey")
	if err := h.svc.Delete(key); err != nil {
		logger.Logger.Error("Failed to delete config", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ExportConfigFile godoc
// @Summary      Export WireGuard config file
// @Description  Генерирует и скачивает файл конфигурации .conf для peer-а по publicKey
// @Tags         configs
// @Produce      text/plain
// @Param        publicKey  path      string  true  "Public key peer'а"
// @Success      200        {file}    file    "WireGuard .conf файл"
// @Failure      404        {object}  domain.ErrorResponse  "конфигурация не найдена"
// @Failure      500        {object}  domain.ErrorResponse  "ошибка создания файла конфигурации"
// @Router       /configs/{publicKey}/file [get]
// ExportConfigFile собирает и возвращает .conf-файл для клиента.
func (h *ConfigHandler) ExportConfigFile(c *gin.Context) {
	key := c.Param("publicKey")
	cfg, err := h.svc.Get(key)
	if err != nil {
		logger.Logger.Error("Config not found for export", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Сборка .conf текста
	text, err := h.svc.BuildClientConfig(cfg)
	if err != nil {
		logger.Logger.Error("Failed to build client config", zap.String("publicKey", key), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Устанавливаем заголовки для скачивания файла
	c.Header("Content-Type", "application/text")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.conf\"", key))
	c.String(http.StatusOK, text)
}
