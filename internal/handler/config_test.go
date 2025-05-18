package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest" // For logger in tests

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger" // Import global logger package
)

// mockService implements ServiceInterface for testing ConfigHandler.
type mockService struct {
	// You can add fields here to control the behavior of mock methods,
	// e.g., predefine responses or errors to return.
	// Example:
	// CreateWithNewKeysFunc func(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error)
	// GetFunc func(publicKey string) (*domain.Config, error)
	// ... and so on for other methods.
}

// Ensure mockService implements ServiceInterface
var _ ServiceInterface = &mockService{}

func (m *mockService) GetAll() ([]domain.Config, error) {
	// Default mock behavior, can be overridden if mockService has function fields.
	return []domain.Config{
		{PublicKey: "key1_from_mock_getall", AllowedIps: []string{"10.0.0.1/32"}},
	}, nil
}

func (m *mockService) Get(publicKey string) (*domain.Config, error) {
	// Example: if m.GetFunc != nil { return m.GetFunc(publicKey) }
	if publicKey == "existing_key" {
		return &domain.Config{PublicKey: publicKey, AllowedIps: []string{"10.0.0.10/32"}}, nil
	}
	return nil, fmt.Errorf("mock error: peer %s not found", publicKey) // Or use repository.ErrPeerNotFound if appropriate for tests
}

func (m *mockService) CreateWithNewKeys(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
	// Example mock:
	return &domain.Config{
		PublicKey:           "mockGeneratedPubKey",
		PrivateKey:          "mockGeneratedPrivKey", // Service returns this, client must store
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}, nil
}

func (m *mockService) UpdateAllowedIPs(publicKey string, ips []string) error {
	if publicKey == "non_existent_key_for_update" {
		return fmt.Errorf("mock error: peer %s not found for update", publicKey) // Or repository.ErrPeerNotFound
	}
	return nil
}

func (m *mockService) Delete(publicKey string) error {
	if publicKey == "non_existent_key_for_delete" {
		return fmt.Errorf("mock error: peer %s not found for delete", publicKey) // Or repository.ErrPeerNotFound
	}
	return nil
}

// BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error)
func (m *mockService) BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
	if peerCfg.PublicKey == "key_for_conf" && clientPrivateKey == "priv_for_conf" {
		return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\n\n[Peer]\nPublicKey = server_pub_key\nEndpoint = example.com:51820\nAllowedIPs = 0.0.0.0/0",
			clientPrivateKey, strings.Join(peerCfg.AllowedIps, ",")), nil
	}
	return "", fmt.Errorf("mock BuildClientConfig error for peer %s", peerCfg.PublicKey)
}

// RotatePeerKey(oldPublicKey string) (*domain.Config, error)
func (m *mockService) RotatePeerKey(oldPublicKey string) (*domain.Config, error) {
	if oldPublicKey == "key_to_rotate" {
		return &domain.Config{
			PublicKey:  "new_rotated_pub_key",
			PrivateKey: "new_rotated_priv_key",
			AllowedIps: []string{"10.0.0.20/32"},
		}, nil
	}
	return nil, fmt.Errorf("mock RotatePeerKey error for peer %s", oldPublicKey)
}

// TestGetAllHandler demonstrates a basic test for the GetAll handler.
// More tests should be added for other handlers and error cases.
func TestGetAllHandler(t *testing.T) {
	// Setup logger for tests
	logger.Logger = zaptest.NewLogger(t) // Use zaptest for silent logging during tests unless -v is used
	gin.SetMode(gin.TestMode)

	mockSvc := &mockService{}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.GET("/configs", h.GetAll)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/configs", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "Expected HTTP status OK")

	var resp []domain.Config
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err, "Error unmarshalling response body")
	assert.Len(t, resp, 1, "Expected one config item")
	if len(resp) == 1 {
		assert.Equal(t, "key1_from_mock_getall", resp[0].PublicKey, "Unexpected public key in response")
	}
}

// TODO: Add more tests for other handlers:
// - TestGetByPublicKey_Success
// - TestGetByPublicKey_NotFound
// - TestCreateConfig_Success
// - TestCreateConfig_InvalidInput
// - TestUpdateAllowedIPs_Success
// - TestUpdateAllowedIPs_NotFound
// - TestUpdateAllowedIPs_InvalidInput
// - TestDeleteConfig_Success
// - TestDeleteConfig_NotFound
// - TestGenerateClientConfigFile_Success
// - TestGenerateClientConfigFile_PeerNotFound
// - TestGenerateClientConfigFile_InvalidInput
// - TestRotatePeer_Success
// - TestRotatePeer_NotFound

// Example for TestCreateConfig_Success
func TestCreateConfig_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	mockSvc := &mockService{} // Using the default mock behavior
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs", h.CreateConfig) // Assuming this is the correct route now

	createReq := domain.CreatePeerRequest{
		AllowedIps:          []string{"10.99.0.1/32"},
		PreSharedKey:        "testPSK",
		PersistentKeepalive: 25,
	}
	body, _ := json.Marshal(createReq)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "Expected HTTP status Created")

	var respCfg domain.Config
	err := json.Unmarshal(w.Body.Bytes(), &respCfg)
	assert.NoError(t, err, "Error unmarshalling response body for created config")
	assert.Equal(t, "mockGeneratedPubKey", respCfg.PublicKey)
	assert.Equal(t, "mockGeneratedPrivKey", respCfg.PrivateKey) // Verify private key is returned
	assert.Equal(t, createReq.AllowedIps, respCfg.AllowedIps)
	assert.Equal(t, createReq.PreSharedKey, respCfg.PreSharedKey)
	assert.Equal(t, createReq.PersistentKeepalive, respCfg.PersistentKeepalive)
}
