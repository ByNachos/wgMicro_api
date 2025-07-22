package handler

import (
	"errors"
	"fmt"
	"net/http" // Standard HTTP status codes
	"strings"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository" // Needed for checking repository.ErrPeerNotFound and repository.ErrWgTimeout

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ServiceInterface defines the operations that the handler can request from the service layer.
type ServiceInterface interface {
	GetAll() ([]domain.Config, error)
	Get(publicKey string) (*domain.Config, error)
	CreateWithNewKeys(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) // For server-side key generation
	// Create(cfg domain.Config) error // If clients provide their own PublicKey, this might be needed. Based on current decision, CreateWithNewKeys is primary.
	UpdateAllowedIPs(publicKey string, ips []string) error
	Delete(publicKey string) error
	BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error) // Takes client's private key
	RotatePeerKey(oldPublicKey string) (*domain.Config, error)
}

// ConfigHandler orchestrates request handling for WireGuard configurations.
type ConfigHandler struct {
	svc ServiceInterface
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(svc ServiceInterface) *ConfigHandler {
	if svc == nil {
		logger.Logger.Fatal("Service interface cannot be nil for ConfigHandler")
	}
	return &ConfigHandler{svc: svc}
}

// handleError standardizes error responses.
func (h *ConfigHandler) handleError(c *gin.Context, operation string, key string, err error) {
	logFields := []zap.Field{zap.Error(err), zap.String("operation", operation)}
	if key != "" {
		logFields = append(logFields, zap.String("publicKey", key))
	}
	logger.Logger.Error("Handler error", logFields...)

	var statusCode int
	var errMsg string = "An unexpected error occurred."

	switch {
	case errors.Is(err, repository.ErrPeerNotFound):
		statusCode = http.StatusNotFound
		errMsg = fmt.Sprintf("Peer with public key '%s' not found.", key)
	case errors.Is(err, repository.ErrWgTimeout):
		statusCode = http.StatusServiceUnavailable
		errMsg = "WireGuard operation timed out. The service might be temporarily unavailable or under heavy load."
	default:
		if err != nil {
			errMsg = err.Error()
		}
		statusCode = http.StatusInternalServerError
	}
	c.JSON(statusCode, domain.ErrorResponse{Error: errMsg})
}

// GetAll godoc
// @Summary      List all peer configurations
// @Description  Retrieves a list of all currently configured WireGuard peers. Private keys of peers are not included.
// @Tags         configs
// @Produce      json
// @Success      200  {array}   domain.Config         "A list of peer configurations."
// @Failure      500  {object}  domain.ErrorResponse  "Internal server error."
// @Failure      503  {object}  domain.ErrorResponse  "Service unavailable (WireGuard timeout)."
// @Router       /configs [get]
func (h *ConfigHandler) GetAll(c *gin.Context) {
	configs, err := h.svc.GetAll()
	if err != nil {
		h.handleError(c, "GetAllPeers", "", err)
		return
	}
	if configs == nil {
		c.JSON(http.StatusOK, []domain.Config{})
		return
	}
	c.JSON(http.StatusOK, configs)
}

// GetConfig godoc
// @Summary      Get configuration by public key
// @Description  Retrieves detailed configuration for a specific peer identified by its public key. The peer's private key is not included.
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        getRequest  body      domain.GetConfigRequest  true  "Public key to retrieve configuration for."
// @Success      200         {object}  domain.Config            "Peer's configuration."
// @Failure      400         {object}  domain.ErrorResponse     "Invalid input (e.g., empty public key or malformed JSON)."
// @Failure      404         {object}  domain.ErrorResponse     "Peer not found."
// @Failure      500         {object}  domain.ErrorResponse     "Internal server error."
// @Failure      503         {object}  domain.ErrorResponse     "Service unavailable (WireGuard timeout)."
// @Router       /configs/get [post]
func (h *ConfigHandler) GetConfig(c *gin.Context) {
	var req domain.GetConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for GetConfig", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	
	logger.Logger.Info("GetConfig request received", zap.String("publicKey", req.PublicKey))
	
	cfg, err := h.svc.Get(req.PublicKey)
	if err != nil {
		h.handleError(c, "GetPeerByPublicKey", req.PublicKey, err)
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// CreateConfig godoc
// @Summary      Create new peer with server-generated keys
// @Description  Adds a new peer. The server generates cryptographic keys for the peer.
// @Description  The request body should specify AllowedIPs and optionally PreSharedKey and PersistentKeepalive.
// @Description  The response includes the full peer configuration, including the server-generated PrivateKey, which the client must securely store.
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        peerRequest  body      domain.CreatePeerRequest  true  "Peer settings for creation (keys will be generated by server)."
// @Success      201          {object}  domain.Config             "Peer created successfully. The response includes the generated private key."
// @Failure      400          {object}  domain.ErrorResponse      "Invalid input if the request body is malformed or contains invalid data."
// @Failure      500          {object}  domain.ErrorResponse      "Internal server error if peer creation or key generation fails."
// @Failure      503          {object}  domain.ErrorResponse      "Service unavailable if a WireGuard command times out."
// @Router       /configs [post]
func (h *ConfigHandler) CreateConfig(c *gin.Context) {
	var req domain.CreatePeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for CreateConfig (new peer with generated keys)", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	logger.Logger.Info("CreateConfig request received (server will generate keys)",
		zap.Strings("allowedIPs", req.AllowedIps),
		zap.Bool("presharedKeyProvided", req.PreSharedKey != ""),
		zap.Int("persistentKeepalive", req.PersistentKeepalive))

	createdPeerConfig, err := h.svc.CreateWithNewKeys(
		req.AllowedIps,
		req.PreSharedKey,
		req.PersistentKeepalive,
	)
	if err != nil {
		h.handleError(c, "CreatePeerWithNewKeys", "", err) // publicKey is not known before creation attempt
		return
	}
	logger.Logger.Info("Successfully created new peer with server-generated keys",
		zap.String("publicKey", createdPeerConfig.PublicKey)) // DO NOT log private key
	c.JSON(http.StatusCreated, createdPeerConfig)
}

// UpdateAllowedIPs godoc
// @Summary      Update allowed IPs for a peer
// @Description  Replaces the list of allowed IP addresses for an existing peer, identified by its public key.
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        updateRequest  body      domain.UpdateAllowedIpsRequest  true  "Public key and new list of allowed IPs for the peer."
// @Success      200            {object}  nil                             "Allowed IPs updated successfully (No body content in response)."
// @Failure      400            {object}  domain.ErrorResponse            "Invalid input (e.g., missing public key or malformed body)."
// @Failure      404            {object}  domain.ErrorResponse            "Peer not found."
// @Failure      500            {object}  domain.ErrorResponse            "Internal server error."
// @Failure      503            {object}  domain.ErrorResponse            "Service unavailable (WireGuard timeout)."
// @Router       /configs/update-allowed-ips [post]
func (h *ConfigHandler) UpdateAllowedIPs(c *gin.Context) {
	var req domain.UpdateAllowedIpsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for UpdateAllowedIPs", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	
	logger.Logger.Info("UpdateAllowedIPs request received", 
		zap.String("publicKey", req.PublicKey),
		zap.Strings("allowedIPs", req.AllowedIps))
	
	if err := h.svc.UpdateAllowedIPs(req.PublicKey, req.AllowedIps); err != nil {
		h.handleError(c, "UpdatePeerAllowedIPs", req.PublicKey, err)
		return
	}
	c.Status(http.StatusOK) // Or 204 No Content
}

// DeleteConfig godoc
// @Summary      Delete a peer configuration
// @Description  Removes a peer from the WireGuard interface using its public key.
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        deleteRequest  body      domain.DeleteConfigRequest  true  "Public key of the peer to delete."
// @Success      204            {null}    nil                         "Peer deleted successfully (No Content)."
// @Failure      400            {object}  domain.ErrorResponse        "Invalid input (e.g., empty public key or malformed JSON)."
// @Failure      404            {object}  domain.ErrorResponse        "Peer not found (only if service layer can reliably detect this for delete operations)."
// @Failure      500            {object}  domain.ErrorResponse        "Internal server error."
// @Failure      503            {object}  domain.ErrorResponse        "Service unavailable (WireGuard timeout)."
// @Router       /configs/delete [post]
func (h *ConfigHandler) DeleteConfig(c *gin.Context) {
	var req domain.DeleteConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for DeleteConfig", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	
	logger.Logger.Info("DeleteConfig request received", zap.String("publicKey", req.PublicKey))
	
	if err := h.svc.Delete(req.PublicKey); err != nil {
		h.handleError(c, "DeletePeerConfig", req.PublicKey, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GenerateClientConfigFile godoc
// @Summary      Generate client .conf file (client provides keys)
// @Description  Generates a WireGuard .conf file for a client.
// @Description  The request body must contain the client's existing public key (to identify the peer on the server) and the client's corresponding private key.
// @Description  The API uses these keys along with server configuration (server public key, endpoint) and the specific peer's details (AllowedIPs, PSK from server, Keepalive) to construct the .conf file.
// @Description  The provided client private key is inserted directly into the .conf file. The API does not store this client-provided private key.
// @Tags         configs
// @Accept       json
// @Produce      text/plain
// @Param        clientKeysRequest  body  domain.ClientFileRequest  true  "Client's public and private keys needed for .conf generation."
// @Success      200 {file} string "The WireGuard .conf file content as plain text."
// @Failure      400 {object} domain.ErrorResponse "Invalid input if the request body is malformed or required keys are missing."
// @Failure      404 {object} domain.ErrorResponse "Peer not found if no peer matches the provided client_public_key."
// @Failure      500 {object} domain.ErrorResponse "Internal server error if .conf file generation fails for other reasons."
// @Failure      503 {object} domain.ErrorResponse "Service unavailable if a WireGuard command (e.g., during peer data fetch) times out."
// @Router       /configs/client-file [post]
func (h *ConfigHandler) GenerateClientConfigFile(c *gin.Context) {
	var req domain.ClientFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for GenerateClientConfigFile", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	logger.Logger.Info("GenerateClientConfigFile request received", zap.String("clientPublicKey", req.ClientPublicKey))

	peerCfg, err := h.svc.Get(req.ClientPublicKey)
	if err != nil {
		h.handleError(c, "GenerateClientConfigFile_GetPeer", req.ClientPublicKey, err)
		return
	}

	configFileContent, err := h.svc.BuildClientConfig(peerCfg, req.ClientPrivateKey) // Corrected call
	if err != nil {
		h.handleError(c, "GenerateClientConfigFile_BuildContent", req.ClientPublicKey, err)
		return
	}

	safeFilename := SanitizeFilename(req.ClientPublicKey) + ".conf"
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", safeFilename))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(configFileContent))
	logger.Logger.Info("Successfully generated and sent client .conf file",
		zap.String("clientPublicKey", req.ClientPublicKey),
		zap.String("filename", safeFilename))
}

// RotatePeer godoc
// @Summary      Rotate peer key
// @Description  Rotates peer's keys. Server generates new keys. Old peer removed, new one created preserving AllowedIPs & Keepalive. Response includes new PrivateKey (client must store it).
// @Tags         configs
// @Accept       json
// @Produce      json
// @Param        rotateRequest  body      domain.RotatePeerRequest  true  "Public key of the peer to rotate."
// @Success      200            {object}  domain.Config             "New peer configuration including new PrivateKey."
// @Failure      400            {object}  domain.ErrorResponse      "Invalid input (e.g., empty public key or malformed JSON)."
// @Failure      404            {object}  domain.ErrorResponse      "Peer not found."
// @Failure      500            {object}  domain.ErrorResponse      "Internal server error (key rotation fails)."
// @Failure      503            {object}  domain.ErrorResponse      "Service unavailable (WireGuard timeout)."
// @Router       /configs/rotate [post]
func (h *ConfigHandler) RotatePeer(c *gin.Context) {
	var req domain.RotatePeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Logger.Error("Invalid JSON input for RotatePeer", zap.Error(err))
		c.JSON(http.StatusBadRequest, domain.ErrorResponse{Error: "Invalid request body: " + err.Error()})
		return
	}
	
	logger.Logger.Info("RotatePeer request received", zap.String("publicKey", req.PublicKey))

	newCfg, err := h.svc.RotatePeerKey(req.PublicKey)
	if err != nil {
		h.handleError(c, "RotatePeerKey", req.PublicKey, err)
		return
	}
	logger.Logger.Info("Successfully rotated peer key",
		zap.String("oldPublicKey", req.PublicKey),
		zap.String("newPublicKey", newCfg.PublicKey)) // DO NOT log private key
	c.JSON(http.StatusOK, newCfg)
}

// SanitizeFilename removes characters problematic in filenames.
func SanitizeFilename(name string) string {
	replace := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", " "} // Added space
	for _, r := range replace {
		name = strings.ReplaceAll(name, r, "_")
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}
