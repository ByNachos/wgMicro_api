package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

// mockService implements ServiceInterface for testing ConfigHandler.
type mockService struct {
	GetFunc               func(publicKey string) (*domain.Config, error)
	GetAllFunc            func() ([]domain.Config, error)
	CreateWithNewKeysFunc func(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error)
	UpdateAllowedIPsFunc  func(publicKey string, ips []string) error
	DeleteFunc            func(publicKey string) error
	BuildClientConfigFunc func(peerCfg *domain.Config, clientPrivateKey string) (string, error)
	RotatePeerKeyFunc     func(oldPublicKey string) (*domain.Config, error)
}

var _ ServiceInterface = &mockService{} // Ensure mockService implements ServiceInterface

func (m *mockService) GetAll() ([]domain.Config, error) {
	if m.GetAllFunc != nil {
		return m.GetAllFunc()
	}
	return []domain.Config{
		{PublicKey: "key1_from_mock_getall", AllowedIps: []string{"10.0.0.1/32"}},
	}, nil
}

func (m *mockService) Get(publicKey string) (*domain.Config, error) {
	if m.GetFunc != nil {
		return m.GetFunc(publicKey)
	}
	if publicKey == "existing_key" {
		return &domain.Config{PublicKey: publicKey, AllowedIps: []string{"10.0.0.10/32"}, PreSharedKey: "psk123", PersistentKeepalive: 25}, nil
	}
	if publicKey == "non_existent_key" {
		return nil, repository.ErrPeerNotFound
	}
	return nil, fmt.Errorf("mock error: unexpected key %s", publicKey)
}

func (m *mockService) CreateWithNewKeys(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
	if m.CreateWithNewKeysFunc != nil {
		return m.CreateWithNewKeysFunc(allowedIPs, presharedKey, persistentKeepalive)
	}
	// Этот метод не должен быть вызван в TestCreateConfig_InvalidInput,
	// но для полноты мока оставим стандартное поведение.
	return &domain.Config{
		PublicKey:           "mockGeneratedPubKey",
		PrivateKey:          "mockGeneratedPrivKey",
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}, nil
}

func (m *mockService) UpdateAllowedIPs(publicKey string, ips []string) error {
	if m.UpdateAllowedIPsFunc != nil {
		return m.UpdateAllowedIPsFunc(publicKey, ips)
	}
	if publicKey == "non_existent_key_for_update" {
		return repository.ErrPeerNotFound
	}
	return nil
}

func (m *mockService) Delete(publicKey string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(publicKey)
	}
	if publicKey == "non_existent_key_for_delete" {
		return repository.ErrPeerNotFound
	}
	return nil
}

func (m *mockService) BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
	if m.BuildClientConfigFunc != nil {
		return m.BuildClientConfigFunc(peerCfg, clientPrivateKey)
	}
	if peerCfg.PublicKey == "key_for_conf" && clientPrivateKey == "priv_for_conf" {
		return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\n\n[Peer]\nPublicKey = server_pub_key\nEndpoint = example.com:51820\nAllowedIPs = 0.0.0.0/0",
			clientPrivateKey, strings.Join(peerCfg.AllowedIps, ",")), nil
	}
	return "", fmt.Errorf("mock BuildClientConfig error for peer %s", peerCfg.PublicKey)
}

func (m *mockService) RotatePeerKey(oldPublicKey string) (*domain.Config, error) {
	if m.RotatePeerKeyFunc != nil {
		return m.RotatePeerKeyFunc(oldPublicKey)
	}
	if oldPublicKey == "key_to_rotate" {
		return &domain.Config{
			PublicKey:  "new_rotated_pub_key",
			PrivateKey: "new_rotated_priv_key",
			AllowedIps: []string{"10.0.0.20/32"},
		}, nil
	}
	return nil, repository.ErrPeerNotFound
}

func TestGetAllHandler(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)
	mockSvc := &mockService{}
	h := NewConfigHandler(mockSvc)
	r := gin.New()
	r.GET("/configs", h.GetAll)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/configs", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp []domain.Config
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp, 1)
	if len(resp) == 1 {
		assert.Equal(t, "key1_from_mock_getall", resp[0].PublicKey)
	}
}

func TestGetByPublicKey_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)
	expectedPeerKey := "existing_key"
	expectedPeerConfig := &domain.Config{
		PublicKey:           expectedPeerKey,
		AllowedIps:          []string{"192.168.1.1/32", "10.0.0.5/32"},
		PreSharedKey:        "testPSK",
		PersistentKeepalive: 25,
	}
	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			if publicKey == expectedPeerKey {
				return &domain.Config{
					PublicKey:           expectedPeerConfig.PublicKey,
					AllowedIps:          expectedPeerConfig.AllowedIps,
					PreSharedKey:        expectedPeerConfig.PreSharedKey,
					PersistentKeepalive: expectedPeerConfig.PersistentKeepalive,
				}, nil
			}
			return nil, repository.ErrPeerNotFound
		},
	}
	h := NewConfigHandler(mockSvc)
	r := gin.New()
	r.GET("/configs/:publicKey", h.GetByPublicKey)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/configs/"+expectedPeerKey, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var respConfig domain.Config
	err = json.Unmarshal(w.Body.Bytes(), &respConfig)
	require.NoError(t, err)
	assert.Equal(t, expectedPeerConfig.PublicKey, respConfig.PublicKey)
	assert.Equal(t, expectedPeerConfig.AllowedIps, respConfig.AllowedIps)
	assert.Equal(t, expectedPeerConfig.PreSharedKey, respConfig.PreSharedKey)
	assert.Equal(t, expectedPeerConfig.PersistentKeepalive, respConfig.PersistentKeepalive)
	assert.Empty(t, respConfig.PrivateKey)
}

func TestGetByPublicKey_NotFound(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)
	nonExistentKey := "non_existent_key"
	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			if publicKey == nonExistentKey {
				return nil, repository.ErrPeerNotFound
			}
			return nil, fmt.Errorf("mock GetFunc called with unexpected key: %s", publicKey)
		},
	}
	h := NewConfigHandler(mockSvc)
	r := gin.New()
	r.GET("/configs/:publicKey", h.GetByPublicKey)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/configs/"+nonExistentKey, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err)
	expectedErrorMessage := fmt.Sprintf("Peer with public key '%s' not found.", nonExistentKey)
	assert.Equal(t, expectedErrorMessage, respError.Error)
}

// TestCreateConfig_InvalidInput tests peer creation with invalid JSON body.
func TestCreateConfig_InvalidInput(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	mockSvc := &mockService{
		// CreateWithNewKeysFunc не должен быть вызван, так как ошибка на этапе биндинга
		CreateWithNewKeysFunc: func(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
			t.Error("mockService.CreateWithNewKeysFunc should not be called in TestCreateConfig_InvalidInput")
			return nil, fmt.Errorf("service method should not be called")
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs", h.CreateConfig)

	// Невалидное тело запроса
	invalidBody := []byte("это точно не JSON")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(invalidBody))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json") // Важно указать, чтобы Gin пытался парсить как JSON
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "Expected HTTP status 400 Bad Request for invalid JSON")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	// Сообщение об ошибке от Gin при невалидном JSON может варьироваться,
	// но оно должно указывать на проблему с разбором JSON.
	// Например, "invalid request body: invalid character 'э' looking for beginning of value"
	assert.Contains(t, respError.Error, "Invalid request body", "Error message should indicate invalid request body")
	assert.Contains(t, respError.Error, "invalid character", "Error message should detail JSON parsing error") // Часть типового сообщения Gin
}

func TestCreateConfig_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)
	createReq := domain.CreatePeerRequest{
		AllowedIps:          []string{"10.99.0.1/32"},
		PreSharedKey:        "testPSK",
		PersistentKeepalive: 25,
	}
	expectedCreatedPeer := &domain.Config{
		PublicKey:           "newlyGeneratedKey123",
		PrivateKey:          "superSecretClientPrivateKey456",
		AllowedIps:          createReq.AllowedIps,
		PreSharedKey:        createReq.PreSharedKey,
		PersistentKeepalive: createReq.PersistentKeepalive,
	}
	mockSvc := &mockService{
		CreateWithNewKeysFunc: func(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
			assert.Equal(t, createReq.AllowedIps, allowedIPs)
			assert.Equal(t, createReq.PreSharedKey, presharedKey)
			assert.Equal(t, createReq.PersistentKeepalive, persistentKeepalive)
			return expectedCreatedPeer, nil
		},
	}
	h := NewConfigHandler(mockSvc)
	r := gin.New()
	r.POST("/configs", h.CreateConfig)
	body, err := json.Marshal(createReq)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var respCfg domain.Config
	err = json.Unmarshal(w.Body.Bytes(), &respCfg)
	require.NoError(t, err)
	assert.Equal(t, expectedCreatedPeer.PublicKey, respCfg.PublicKey)
	assert.Equal(t, expectedCreatedPeer.PrivateKey, respCfg.PrivateKey)
	assert.Equal(t, expectedCreatedPeer.AllowedIps, respCfg.AllowedIps)
	assert.Equal(t, expectedCreatedPeer.PreSharedKey, respCfg.PreSharedKey)
	assert.Equal(t, expectedCreatedPeer.PersistentKeepalive, respCfg.PersistentKeepalive)
}

// TestCreateConfig_ServiceError tests peer creation when the service layer returns an error.
func TestCreateConfig_ServiceError(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	serviceErrorMessage := "simulated service layer error during peer creation"

	mockSvc := &mockService{
		CreateWithNewKeysFunc: func(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
			// Имитируем ошибку от сервисного слоя
			return nil, errors.New(serviceErrorMessage)
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs", h.CreateConfig)

	// Валидное тело запроса, чтобы ошибка произошла именно на уровне сервиса
	validCreateReq := domain.CreatePeerRequest{
		AllowedIps:          []string{"10.50.0.1/32"},
		PersistentKeepalive: 20,
	}
	body, err := json.Marshal(validCreateReq)
	require.NoError(t, err, "Failed to marshal valid CreatePeerRequest")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Ожидаем 500 Internal Server Error или 503 Service Unavailable,
	// в зависимости от того, как handleError классифицирует ошибку сервиса.
	// В нашем текущем handleError, любая неизвестная ошибка от сервиса (не ErrPeerNotFound и не ErrWgTimeout)
	// приведет к http.StatusInternalServerError.
	require.Equal(t, http.StatusInternalServerError, w.Code, "Expected HTTP status 500 Internal Server Error for service error")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Equal(t, serviceErrorMessage, respError.Error, "Error message mismatch")
}

// TestUpdateAllowedIPs_Success tests successful update of allowed IPs for a peer.
func TestUpdateAllowedIPs_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "peer_to_update_ips"
	updateReqPayload := domain.AllowedIpsUpdate{
		AllowedIps: []string{"192.168.100.1/32", "10.10.10.0/24"},
	}

	serviceCalled := false
	mockSvc := &mockService{
		UpdateAllowedIPsFunc: func(publicKey string, ips []string) error {
			serviceCalled = true
			assert.Equal(t, targetPublicKey, publicKey, "PublicKey passed to service mismatch")
			assert.Equal(t, updateReqPayload.AllowedIps, ips, "AllowedIps passed to service mismatch")
			return nil // Успешное обновление в сервисе
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.PUT("/configs/:publicKey/allowed-ips", h.UpdateAllowedIPs)

	body, err := json.Marshal(updateReqPayload)
	require.NoError(t, err, "Failed to marshal AllowedIpsUpdate payload")

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/allowed-ips", targetPublicKey)
	req, err := http.NewRequest(http.MethodPut, reqPath, bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "Expected HTTP status 200 OK for successful update")
	assert.True(t, serviceCalled, "Service method UpdateAllowedIPs was not called")
	// Тело ответа для 200 OK в данном случае пустое, так что проверять его особо нечего.
	// Если бы возвращался 204 No Content, то и w.Body.Len() был бы 0.
	// Для 200 OK без тела w.Body.String() будет пустой строкой.
	assert.Empty(t, w.Body.String(), "Response body should be empty for 200 OK in this case")
}

// TestUpdateAllowedIPs_NotFound tests updating allowed IPs for a non-existent peer.
func TestUpdateAllowedIPs_NotFound(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	nonExistentPeerKey := "non_existent_peer_for_ip_update"
	updateReqPayload := domain.AllowedIpsUpdate{
		AllowedIps: []string{"172.16.0.1/32"},
	}

	serviceCalled := false
	mockSvc := &mockService{
		UpdateAllowedIPsFunc: func(publicKey string, ips []string) error {
			serviceCalled = true
			assert.Equal(t, nonExistentPeerKey, publicKey) // Убедимся, что сервис вызван с правильным ключом
			return repository.ErrPeerNotFound              // Имитируем ошибку "не найдено" от сервиса
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.PUT("/configs/:publicKey/allowed-ips", h.UpdateAllowedIPs)

	body, err := json.Marshal(updateReqPayload)
	require.NoError(t, err, "Failed to marshal AllowedIpsUpdate payload")

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/allowed-ips", nonExistentPeerKey)
	req, err := http.NewRequest(http.MethodPut, reqPath, bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "Expected HTTP status 404 Not Found")
	assert.True(t, serviceCalled, "Service method UpdateAllowedIPs was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	expectedErrorMessage := fmt.Sprintf("Peer with public key '%s' not found.", nonExistentPeerKey)
	assert.Equal(t, expectedErrorMessage, respError.Error, "Error message mismatch")
}

// TestUpdateAllowedIPs_InvalidInput tests updating allowed IPs with an invalid request body.
func TestUpdateAllowedIPs_InvalidInput(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "any_valid_peer_key_format" // Ключ может быть валидным, проблема в теле

	mockSvc := &mockService{
		UpdateAllowedIPsFunc: func(publicKey string, ips []string) error {
			// Этот метод не должен быть вызван, если тело запроса невалидно
			t.Errorf("mockService.UpdateAllowedIPsFunc should not be called in TestUpdateAllowedIPs_InvalidInput")
			return fmt.Errorf("service method UpdateAllowedIPs should not have been called")
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.PUT("/configs/:publicKey/allowed-ips", h.UpdateAllowedIPs)

	// Невалидное тело запроса
	invalidBody := []byte("this is not a valid json body for allowedips")

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/allowed-ips", targetPublicKey)
	req, err := http.NewRequest(http.MethodPut, reqPath, bytes.NewBuffer(invalidBody))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json") // Указываем, что пытаемся отправить JSON
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "Expected HTTP status 400 Bad Request for invalid JSON body")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Contains(t, respError.Error, "Invalid request body", "Error message should indicate invalid request body")
	// Конкретное сообщение об ошибке парсинга JSON может отличаться, поэтому проверяем подстроку
	assert.Contains(t, respError.Error, "invalid character", "Error message should detail JSON parsing error")
}

// TestUpdateAllowedIPs_ServiceError tests updating allowed IPs when the service returns an error.
func TestUpdateAllowedIPs_ServiceError(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "peer_with_service_error_on_update"
	updateReqPayload := domain.AllowedIpsUpdate{
		AllowedIps: []string{"10.0.20.1/32"},
	}
	simulatedServiceErrorMessage := "simulated service error during IP update"
	// Можно также протестировать специфичную ошибку, например, ErrWgTimeout
	// simulatedServiceError := repository.ErrWgTimeout

	serviceCalled := false
	mockSvc := &mockService{
		UpdateAllowedIPsFunc: func(publicKey string, ips []string) error {
			serviceCalled = true
			assert.Equal(t, targetPublicKey, publicKey)
			assert.Equal(t, updateReqPayload.AllowedIps, ips)
			return errors.New(simulatedServiceErrorMessage) // Имитируем ошибку от сервиса
			// return simulatedServiceError // Если тестируем ErrWgTimeout
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.PUT("/configs/:publicKey/allowed-ips", h.UpdateAllowedIPs)

	body, err := json.Marshal(updateReqPayload)
	require.NoError(t, err, "Failed to marshal AllowedIpsUpdate payload")

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/allowed-ips", targetPublicKey)
	req, err := http.NewRequest(http.MethodPut, reqPath, bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// В соответствии с handleError, общая ошибка сервиса должна вернуть 500.
	// Если бы мы имитировали repository.ErrWgTimeout, то ожидали бы 503.
	expectedStatusCode := http.StatusInternalServerError
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	// 	expectedStatusCode = http.StatusServiceUnavailable
	// }

	require.Equal(t, expectedStatusCode, w.Code, "Expected appropriate HTTP status for service error")
	assert.True(t, serviceCalled, "Service method UpdateAllowedIPs was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Equal(t, simulatedServiceErrorMessage, respError.Error, "Error message mismatch")
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	// 	assert.Equal(t, "WireGuard operation timed out. The service might be temporarily unavailable or under heavy load.", respError.Error, "Error message mismatch for timeout")
	// }
}

// TestDeleteConfig_Success tests successful deletion of a peer configuration.
func TestDeleteConfig_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "peer_to_delete_successfully"

	serviceCalled := false
	mockSvc := &mockService{
		DeleteFunc: func(publicKey string) error {
			serviceCalled = true
			assert.Equal(t, targetPublicKey, publicKey, "PublicKey passed to service for deletion mismatch")
			return nil // Успешное удаление в сервисе
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.DELETE("/configs/:publicKey", h.DeleteConfig)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s", targetPublicKey)
	req, err := http.NewRequest(http.MethodDelete, reqPath, nil) // DELETE запросы обычно не имеют тела
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code, "Expected HTTP status 204 No Content for successful deletion")
	assert.True(t, serviceCalled, "Service method Delete was not called")
	assert.Empty(t, w.Body.String(), "Response body should be empty for 204 No Content")
}

// TestDeleteConfig_NotFound tests deleting a non-existent peer.
func TestDeleteConfig_NotFound(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	nonExistentPeerKey := "non_existent_peer_for_delete"

	serviceCalled := false
	mockSvc := &mockService{
		DeleteFunc: func(publicKey string) error {
			serviceCalled = true
			assert.Equal(t, nonExistentPeerKey, publicKey)
			return repository.ErrPeerNotFound // Имитируем ошибку "не найдено" от сервиса
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.DELETE("/configs/:publicKey", h.DeleteConfig)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s", nonExistentPeerKey)
	req, err := http.NewRequest(http.MethodDelete, reqPath, nil)
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "Expected HTTP status 404 Not Found for deleting non-existent peer")
	assert.True(t, serviceCalled, "Service method Delete was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	expectedErrorMessage := fmt.Sprintf("Peer with public key '%s' not found.", nonExistentPeerKey)
	assert.Equal(t, expectedErrorMessage, respError.Error, "Error message mismatch")
}

// TestDeleteConfig_ServiceError tests deleting a peer when the service returns an error.
func TestDeleteConfig_ServiceError(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "peer_with_service_error_on_delete"
	simulatedServiceErrorMessage := "simulated service error during peer deletion"
	// Для примера, если бы мы хотели протестировать ошибку таймаута:
	// simulatedServiceError := repository.ErrWgTimeout

	serviceCalled := false
	mockSvc := &mockService{
		DeleteFunc: func(publicKey string) error {
			serviceCalled = true
			assert.Equal(t, targetPublicKey, publicKey)
			return errors.New(simulatedServiceErrorMessage) // Имитируем общую ошибку от сервиса
			// return simulatedServiceError // Если тестируем ErrWgTimeout
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.DELETE("/configs/:publicKey", h.DeleteConfig)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s", targetPublicKey)
	req, err := http.NewRequest(http.MethodDelete, reqPath, nil)
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	// Ожидаем 500 для общей ошибки сервиса.
	// Если бы мы имитировали repository.ErrWgTimeout, ожидали бы 503.
	expectedStatusCode := http.StatusInternalServerError
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	// 	expectedStatusCode = http.StatusServiceUnavailable
	// }

	require.Equal(t, expectedStatusCode, w.Code, "Expected appropriate HTTP status for service error during deletion")
	assert.True(t, serviceCalled, "Service method Delete was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Equal(t, simulatedServiceErrorMessage, respError.Error, "Error message mismatch")
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	// 	assert.Equal(t, "WireGuard operation timed out. The service might be temporarily unavailable or under heavy load.", respError.Error, "Error message mismatch for timeout")
	// }
}

// TestGenerateClientConfigFile_Success tests successful generation of a client .conf file.
func TestGenerateClientConfigFile_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	clientPublicKeyForFile := "clientPubKeyForFileGeneration"
	clientPrivateKeyForFile := "clientPrivKeyForFileGeneration" // Этот ключ клиент передает в запросе

	// Ожидаемая конфигурация пира, которую вернет Get()
	peerConfigFromServer := &domain.Config{
		PublicKey:           clientPublicKeyForFile,
		AllowedIps:          []string{"10.0.0.99/32"},
		PreSharedKey:        "peerPSK123",
		PersistentKeepalive: 21,
	}

	// Ожидаемое содержимое .conf файла, которое вернет BuildClientConfig()
	expectedConfContent := fmt.Sprintf(
		"[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = 1.1.1.1, 8.8.8.8\n\n[Peer]\nPublicKey = mockServerPubKey\nEndpoint = mock.server.com:51820\nAllowedIPs = 0.0.0.0/0, ::/0\nPresharedKey = %s\nPersistentKeepalive = %d\n",
		clientPrivateKeyForFile,
		peerConfigFromServer.AllowedIps[0],
		peerConfigFromServer.PreSharedKey,
		peerConfigFromServer.PersistentKeepalive,
	)

	serviceGetCalled := false
	serviceBuildCalled := false
	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			serviceGetCalled = true
			require.Equal(t, clientPublicKeyForFile, publicKey, "PublicKey passed to Get service mismatch")
			return peerConfigFromServer, nil
		},
		BuildClientConfigFunc: func(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
			serviceBuildCalled = true
			assert.Equal(t, peerConfigFromServer.PublicKey, peerCfg.PublicKey, "PeerConfig.PublicKey passed to BuildClientConfig mismatch")
			// Можно добавить больше проверок для peerCfg, если необходимо
			assert.Equal(t, clientPrivateKeyForFile, clientPrivateKey, "ClientPrivateKey passed to BuildClientConfig mismatch")
			return expectedConfContent, nil
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/client-file", h.GenerateClientConfigFile)

	reqPayload := domain.ClientFileRequest{
		ClientPublicKey:  clientPublicKeyForFile,
		ClientPrivateKey: clientPrivateKeyForFile,
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err, "Failed to marshal ClientFileRequest payload")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "Expected HTTP status 200 OK")
	assert.True(t, serviceGetCalled, "Service method Get was not called")
	assert.True(t, serviceBuildCalled, "Service method BuildClientConfig was not called")

	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"), "Content-Type header mismatch")

	// Проверяем Content-Disposition (санитизированное имя файла)
	sanitizedFilename := SanitizeFilename(clientPublicKeyForFile) + ".conf"
	expectedContentDisposition := fmt.Sprintf("attachment; filename=\"%s\"", sanitizedFilename)
	assert.Equal(t, expectedContentDisposition, w.Header().Get("Content-Disposition"), "Content-Disposition header mismatch")

	assert.Equal(t, expectedConfContent, w.Body.String(), "Response body (conf file content) mismatch")
}

// TestGenerateClientConfigFile_PeerNotFound tests .conf file generation when the peer is not found.
func TestGenerateClientConfigFile_PeerNotFound(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	clientPublicKeyNotFound := "clientPubKeyNotFoundOnServer"
	clientPrivateKeyAttempt := "anyClientPrivateKey" // Не важен, т.к. до BuildClientConfig не дойдет

	serviceGetCalled := false
	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			serviceGetCalled = true
			require.Equal(t, clientPublicKeyNotFound, publicKey, "PublicKey passed to Get service mismatch")
			return nil, repository.ErrPeerNotFound // Имитируем, что пир не найден
		},
		BuildClientConfigFunc: func(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
			// Этот метод не должен быть вызван
			t.Errorf("mockService.BuildClientConfigFunc should not be called in TestGenerateClientConfigFile_PeerNotFound")
			return "", fmt.Errorf("BuildClientConfig should not have been called")
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/client-file", h.GenerateClientConfigFile)

	reqPayload := domain.ClientFileRequest{
		ClientPublicKey:  clientPublicKeyNotFound,
		ClientPrivateKey: clientPrivateKeyAttempt,
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err, "Failed to marshal ClientFileRequest payload")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "Expected HTTP status 404 Not Found")
	assert.True(t, serviceGetCalled, "Service method Get was not called")
	// serviceBuildCalled проверять не нужно, т.к. mockSvc.BuildClientConfigFunc содержит t.Errorf

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	expectedErrorMessage := fmt.Sprintf("Peer with public key '%s' not found.", clientPublicKeyNotFound)
	assert.Equal(t, expectedErrorMessage, respError.Error, "Error message mismatch")
}

// TestGenerateClientConfigFile_InvalidInput_BadJSON tests .conf file generation with a malformed JSON body.
func TestGenerateClientConfigFile_InvalidInput_BadJSON(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			t.Errorf("mockService.GetFunc should not be called in TestGenerateClientConfigFile_InvalidInput_BadJSON")
			return nil, nil
		},
		BuildClientConfigFunc: func(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
			t.Errorf("mockService.BuildClientConfigFunc should not be called in TestGenerateClientConfigFile_InvalidInput_BadJSON")
			return "", nil
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/client-file", h.GenerateClientConfigFile)

	invalidJSONBody := []byte("{not_a_valid_json,,,")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(invalidJSONBody))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "Expected HTTP status 400 Bad Request for malformed JSON")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")
	assert.Contains(t, respError.Error, "Invalid request body", "Error message should indicate invalid request body")
	// Сообщение об ошибке от Gin при невалидном JSON может варьироваться
	// Например: "invalid request body: invalid character 'n' looking for beginning of object key string"
}

// TestGenerateClientConfigFile_InvalidInput_MissingField tests .conf file generation with a JSON body missing a required field.
func TestGenerateClientConfigFile_InvalidInput_MissingField(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			t.Errorf("mockService.GetFunc should not be called in TestGenerateClientConfigFile_InvalidInput_MissingField")
			return nil, nil
		},
		BuildClientConfigFunc: func(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
			t.Errorf("mockService.BuildClientConfigFunc should not be called in TestGenerateClientConfigFile_InvalidInput_MissingField")
			return "", nil
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/client-file", h.GenerateClientConfigFile)

	// Валидный JSON, но отсутствует обязательное поле client_private_key
	// payloadMissingField := domain.ClientFileRequest{
	// 	ClientPublicKey: "somePubKey",
	// 	// ClientPrivateKey отсутствует
	// }
	// Чтобы Gin корректно обработал структуру без поля, лучше маршалить map[string]string
	// или убедиться, что `binding:"required"` работает как ожидается с неполной структурой.
	// Для ClientFileRequest поля `client_public_key` и `client_private_key` помечены `binding:"required"`.
	// Попробуем отправить JSON без одного из них.
	// json.Marshal не будет включать нулевое (пустое) значение ClientPrivateKey, если оно omitempty, но у нас нет omitempty.
	// Для более явного контроля можно создать map.

	// Вариант 1: Маршалим неполную структуру
	// body, err := json.Marshal(payloadMissingField)
	// require.NoError(t, err)

	// Вариант 2: Формируем JSON строку вручную или через map для большей предсказуемости отсутствия поля
	jsonStr := `{"client_public_key": "somePubKey"}` // Отсутствует client_private_key
	body := []byte(jsonStr)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "Expected HTTP status 400 Bad Request for missing required field")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Contains(t, respError.Error, "Invalid request body", "Error message should indicate invalid request body")
	// Ожидаемое сообщение от Gin validator для обязательного поля
	assert.Contains(t, respError.Error, "ClientPrivateKey", "Error message should mention the missing field ClientPrivateKey")
	assert.Contains(t, respError.Error, "required", "Error message should mention 'required'")
}

// TestGenerateClientConfigFile_ServiceError tests .conf file generation when the service's BuildClientConfig returns an error.
func TestGenerateClientConfigFile_ServiceError(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	clientPublicKey := "clientPubKeyForServiceError"
	clientPrivateKey := "clientPrivKeyForServiceError"

	// Пир найден успешно
	peerConfigFromServer := &domain.Config{
		PublicKey: clientPublicKey,
		// ... другие поля могут быть, но BuildClientConfig вернет ошибку
	}

	simulatedBuildErrorMessage := "simulated error during .conf file building"

	serviceGetCalled := false
	serviceBuildCalled := false
	mockSvc := &mockService{
		GetFunc: func(publicKey string) (*domain.Config, error) {
			serviceGetCalled = true
			require.Equal(t, clientPublicKey, publicKey)
			return peerConfigFromServer, nil // Get успешен
		},
		BuildClientConfigFunc: func(peerCfg *domain.Config, cPrivateKey string) (string, error) {
			serviceBuildCalled = true
			assert.Equal(t, peerConfigFromServer.PublicKey, peerCfg.PublicKey)
			assert.Equal(t, clientPrivateKey, cPrivateKey)
			return "", errors.New(simulatedBuildErrorMessage) // BuildClientConfig возвращает ошибку
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/client-file", h.GenerateClientConfigFile)

	reqPayload := domain.ClientFileRequest{
		ClientPublicKey:  clientPublicKey,
		ClientPrivateKey: clientPrivateKey,
	}
	body, err := json.Marshal(reqPayload)
	require.NoError(t, err, "Failed to marshal ClientFileRequest payload")

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(body))
	require.NoError(t, err, "Failed to create HTTP request")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Ожидаем 500 Internal Server Error для общей ошибки сервиса
	require.Equal(t, http.StatusInternalServerError, w.Code, "Expected HTTP status 500 Internal Server Error")
	assert.True(t, serviceGetCalled, "Service method Get was not called")
	assert.True(t, serviceBuildCalled, "Service method BuildClientConfig was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Equal(t, simulatedBuildErrorMessage, respError.Error, "Error message mismatch")
}

// TestRotatePeer_Success tests successful key rotation for a peer.
func TestRotatePeer_Success(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	oldPublicKeyToRotate := "peerKeyToBeRotated"

	// Ожидаемая новая конфигурация, которую вернет сервис RotatePeerKey
	expectedNewConfigAfterRotation := &domain.Config{
		PublicKey:           "newlyRotatedPublicKey123",
		PrivateKey:          "newlyRotatedAndReturnedPrivateKey456", // Сервис должен вернуть новый приватный ключ
		AllowedIps:          []string{"10.0.50.1/32"},
		PreSharedKey:        "originalPSK", // Предполагаем, что PSK сохраняется или пересоздается
		PersistentKeepalive: 25,            // Предполагаем, что Keepalive сохраняется
	}

	serviceCalled := false
	mockSvc := &mockService{
		RotatePeerKeyFunc: func(oldPublicKey string) (*domain.Config, error) {
			serviceCalled = true
			require.Equal(t, oldPublicKeyToRotate, oldPublicKey, "Old PublicKey passed to RotatePeerKey service mismatch")
			return expectedNewConfigAfterRotation, nil // Успешная ротация
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/:publicKey/rotate", h.RotatePeer)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/rotate", oldPublicKeyToRotate)
	// POST запросы на ротацию обычно не требуют тела, т.к. вся информация в пути
	req, err := http.NewRequest(http.MethodPost, reqPath, nil)
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "Expected HTTP status 200 OK for successful rotation")
	assert.True(t, serviceCalled, "Service method RotatePeerKey was not called")

	var respConfig domain.Config
	err = json.Unmarshal(w.Body.Bytes(), &respConfig)
	require.NoError(t, err, "Error unmarshalling response body for rotated config")

	assert.Equal(t, expectedNewConfigAfterRotation.PublicKey, respConfig.PublicKey, "New PublicKey mismatch")
	assert.Equal(t, expectedNewConfigAfterRotation.PrivateKey, respConfig.PrivateKey, "New PrivateKey should be returned and match expected")
	assert.Equal(t, expectedNewConfigAfterRotation.AllowedIps, respConfig.AllowedIps, "AllowedIps mismatch")
	assert.Equal(t, expectedNewConfigAfterRotation.PreSharedKey, respConfig.PreSharedKey, "PreSharedKey mismatch")
	assert.Equal(t, expectedNewConfigAfterRotation.PersistentKeepalive, respConfig.PersistentKeepalive, "PersistentKeepalive mismatch")
}

// TestRotatePeer_NotFound tests key rotation for a non-existent peer.
func TestRotatePeer_NotFound(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	nonExistentOldPublicKey := "nonExistentPeerKeyForRotation"

	serviceCalled := false
	mockSvc := &mockService{
		RotatePeerKeyFunc: func(oldPublicKey string) (*domain.Config, error) {
			serviceCalled = true
			require.Equal(t, nonExistentOldPublicKey, oldPublicKey, "Old PublicKey passed to RotatePeerKey service mismatch")
			return nil, repository.ErrPeerNotFound // Имитируем ошибку "не найдено" от сервиса
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/:publicKey/rotate", h.RotatePeer)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/rotate", nonExistentOldPublicKey)
	req, err := http.NewRequest(http.MethodPost, reqPath, nil)
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "Expected HTTP status 404 Not Found for rotating non-existent peer")
	assert.True(t, serviceCalled, "Service method RotatePeerKey was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	// Сообщение об ошибке должно соответствовать тому, как handleError обрабатывает ErrPeerNotFound
	expectedErrorMessage := fmt.Sprintf("Peer with public key '%s' not found.", nonExistentOldPublicKey)
	assert.Equal(t, expectedErrorMessage, respError.Error, "Error message mismatch")
}

// TestRotatePeer_ServiceError tests key rotation when the service returns an error.
func TestRotatePeer_ServiceError(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	targetPublicKey := "peer_with_service_error_on_rotation"
	simulatedServiceErrorMessage := "simulated service error during key rotation"
	// Пример для специфичной ошибки, если потребуется:
	// simulatedServiceError := repository.ErrWgTimeout

	serviceCalled := false
	mockSvc := &mockService{
		RotatePeerKeyFunc: func(oldPublicKey string) (*domain.Config, error) {
			serviceCalled = true
			assert.Equal(t, targetPublicKey, oldPublicKey)
			return nil, errors.New(simulatedServiceErrorMessage) // Имитируем общую ошибку от сервиса
			// return nil, simulatedServiceError // Если тестируем ErrWgTimeout
		},
	}
	h := NewConfigHandler(mockSvc)

	r := gin.New()
	r.POST("/configs/:publicKey/rotate", h.RotatePeer)

	w := httptest.NewRecorder()
	reqPath := fmt.Sprintf("/configs/%s/rotate", targetPublicKey)
	req, err := http.NewRequest(http.MethodPost, reqPath, nil)
	require.NoError(t, err, "Failed to create HTTP request")
	r.ServeHTTP(w, req)

	// Ожидаем 500 для общей ошибки сервиса.
	// Если бы мы имитировали repository.ErrWgTimeout, ожидали бы 503.
	expectedStatusCode := http.StatusInternalServerError
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	// 	expectedStatusCode = http.StatusServiceUnavailable
	// }

	require.Equal(t, expectedStatusCode, w.Code, "Expected appropriate HTTP status for service error during rotation")
	assert.True(t, serviceCalled, "Service method RotatePeerKey was not called")

	var respError domain.ErrorResponse
	err = json.Unmarshal(w.Body.Bytes(), &respError)
	require.NoError(t, err, "Error unmarshalling error response body")

	assert.Equal(t, simulatedServiceErrorMessage, respError.Error, "Error message mismatch")
	// if errors.Is(simulatedServiceError, repository.ErrWgTimeout) {
	//  	assert.Equal(t, "WireGuard operation timed out. The service might be temporarily unavailable or under heavy load.", respError.Error, "Error message mismatch for timeout")
	// }
}
