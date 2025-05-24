// internal/server/integration_test.go
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	// "os" // No longer needed for creating test wg config file
	// "path/filepath" // No longer needed
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/config" // We'll use the Config struct directly
	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"

	// "wgMicro_api/internal/serverkeys" // No longer needed
	"wgMicro_api/internal/service"
)

// These keys should be a valid pair. Generate them once and use.
// Example: wg genkey | tee test_server_private.key | wg pubkey > test_server_public.key
const (
	testIntegrationServerPrivateKey = "BDFTfugHHNOHfPC3B4NSGfRmNE4zs+ZXM2ikT8//RUU=" // REPLACE WITH YOUR GENERATED KEY
	testIntegrationServerPublicKey  = "mK0477z4M24qLMVu2aSNwJjgCR97FPbyxsZ3+gx/NWg=" // REPLACE WITH CORRESPONDING PUBLIC KEY
	testIntegrationWgInterface      = "wg_int_test"                                  // A test interface name
)

func setupIntegrationTestEnvironment(t *testing.T) (router *gin.Engine, repo repository.Repo, cleanupFunc func()) { // Изменил тип repo на repository.Repo
	t.Helper()
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	// Создаем appConfig, имитируя загрузку.
	// Важно: эти значения должны быть консистентны с тем, что ожидает API,
	// когда он будет запущен в Docker с реальным .env файлом для интеграционных тестов.
	// Но для *этих конкретных юнит-тестов сервера/хендлера с FakeWGRepository* мы можем задать их напрямую.
	// Когда мы будем писать настоящие интеграционные тесты, которые бьют по Docker-контейнеру,
	// там будет использоваться config.LoadConfig(), читающий .env.
	appConfig := &config.Config{
		AppEnv:      config.EnvTest,
		Port:        "0",                        // Для httptest не используется, но для полноты
		WGInterface: testIntegrationWgInterface, // Имя интерфейса для тестов
	}
	// Заполняем вложенные структуры
	appConfig.Server.PrivateKey = testIntegrationServerPrivateKey
	appConfig.Server.PublicKey = testIntegrationServerPublicKey // Обычно вычисляется, но для теста можем задать
	appConfig.Server.EndpointHost = "integration.test.vpn"
	appConfig.Server.EndpointPort = "51820"
	appConfig.Server.ListenPort = 51820
	appConfig.Server.InterfaceAddresses = []string{"10.99.99.1/24"} // Это теперь строка в .env, но []string в структуре

	// ИЗМЕНЕНИЕ ЗДЕСЬ: DNSServers теперь строка
	appConfig.ClientConfig.DNSServers = "1.1.1.1" // Было: []string{"1.1.1.1", "1.0.0.1"}

	appConfig.Timeouts.WgCmdSeconds = 5
	appConfig.Timeouts.KeyGenSeconds = 5

	// Производные поля
	appConfig.DerivedWgCmdTimeout = time.Duration(appConfig.Timeouts.WgCmdSeconds) * time.Second
	appConfig.DerivedKeyGenTimeout = time.Duration(appConfig.Timeouts.KeyGenSeconds) * time.Second
	appConfig.DerivedServerEndpoint = fmt.Sprintf("%s:%s", appConfig.Server.EndpointHost, appConfig.Server.EndpointPort)

	// Используем FakeWGRepository для этих тестов, чтобы изолировать логику сервера/хендлера
	// от реальных вызовов wg.
	// Для настоящих интеграционных тестов (бьющих по Docker) мы бы использовали реальный repository.NewWGRepository().
	// Тип возвращаемого repo изменен на repository.Repo для общности, но мы знаем, что это FakeWGRepository.
	fakeRepo := repository.NewFakeWGRepository()
	// Убедимся, что fakeRepo реализует repository.Repo (если есть сомнения)
	var testRepo repository.Repo = fakeRepo

	svc := service.NewConfigService(
		testRepo, // Передаем интерфейс
		appConfig.Server.PublicKey,
		appConfig.DerivedServerEndpoint,
		appConfig.DerivedKeyGenTimeout,
		appConfig.ClientConfig.DNSServers, // Передаем строку DNS
	)
	cfgHandler := handler.NewConfigHandler(svc)

	// Тип второго аргумента NewRouter - repository.Repo
	testRouter := NewRouter(cfgHandler, testRepo)

	cleanup := func() {
		// No file cleanup needed
	}

	return testRouter, fakeRepo, cleanup // Возвращаем конкретный тип fakeRepo для удобства в тестах, если нужно будет обращаться к его полям
}

// TestIntegration_PeerLifecycle_WithViperConfig (или лучше переименовать в TestHandler_PeerLifecycle_WithMockedConfigAndFakeRepo)
// Этот тест сейчас больше похож на юнит/интеграционный тест для связки handler+service+fakerepo,
// а не на полноценный интеграционный тест с реальным config.LoadConfig() и реальным WGRepository.
// Название _WithViperConfig может сбивать с толку, так как Viper здесь не используется для загрузки.
func TestIntegration_PeerLifecycle(t *testing.T) { // Переименовал для ясности
	router, fakeRepo, cleanup := setupIntegrationTestEnvironment(t)
	defer cleanup()

	var createdPeer domain.Config

	// 1. Create Peer (server generates keys)
	t.Run("CreatePeer", func(t *testing.T) {
		createReqBody := domain.CreatePeerRequest{
			AllowedIps:          []string{"10.100.0.2/32"},
			PersistentKeepalive: 25,
		}
		bodyBytes, _ := json.Marshal(createReqBody)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, "CreatePeer: expected 201 Created status")
		err := json.Unmarshal(w.Body.Bytes(), &createdPeer)
		require.NoError(t, err, "CreatePeer: failed to unmarshal response body")
		assert.NotEmpty(t, createdPeer.PublicKey, "CreatePeer: PublicKey in response should not be empty")
		assert.NotEmpty(t, createdPeer.PrivateKey, "CreatePeer: PrivateKey in response should be returned to the client")
		assert.Equal(t, createReqBody.AllowedIps, createdPeer.AllowedIps, "CreatePeer: AllowedIps in response should match request")
	})

	// 2. Get Peer by PublicKey
	t.Run("GetPeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "GetPeer: createdPeer.PublicKey is empty, cannot proceed. Check CreatePeer step.")

		w := httptest.NewRecorder()
		reqPath := fmt.Sprintf("/configs/%s", createdPeer.PublicKey)
		req, _ := http.NewRequest(http.MethodGet, reqPath, nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "GetPeer: expected 200 OK status")
		var fetchedPeer domain.Config
		err := json.Unmarshal(w.Body.Bytes(), &fetchedPeer)
		require.NoError(t, err, "GetPeer: failed to unmarshal response body")
		assert.Equal(t, createdPeer.PublicKey, fetchedPeer.PublicKey, "GetPeer: PublicKey in response should match requested key")
		assert.Empty(t, fetchedPeer.PrivateKey, "GetPeer: PrivateKey should NOT be returned by the Get endpoint")
		assert.Equal(t, createdPeer.AllowedIps, fetchedPeer.AllowedIps, "GetPeer: AllowedIps should match the created peer's IPs")
	})

	// 3. Generate Client Config File
	t.Run("GenerateClientFile", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "GenerateClientFile: createdPeer.PublicKey is empty.")
		require.NotEmpty(t, createdPeer.PrivateKey, "GenerateClientFile: createdPeer.PrivateKey is empty (needed from create step).")

		fileReq := domain.ClientFileRequest{
			ClientPublicKey:  createdPeer.PublicKey,
			ClientPrivateKey: createdPeer.PrivateKey,
		}
		bodyBytes, _ := json.Marshal(fileReq)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "GenerateClientFile: expected 200 OK status")
		confContent := w.Body.String()
		assert.Contains(t, confContent, fmt.Sprintf("PrivateKey = %s", createdPeer.PrivateKey), "Client conf should contain client's private key")
		assert.Contains(t, confContent, fmt.Sprintf("PublicKey = %s", testIntegrationServerPublicKey), "Client conf should contain server's public key")
		assert.Contains(t, confContent, "Endpoint = integration.test.vpn:51820", "Client conf should contain server's endpoint")
		// ИЗМЕНЕНИЕ ЗДЕСЬ: DNS теперь одна строка
		assert.Contains(t, confContent, "DNS = 1.1.1.1", "Client conf should contain configured DNS server")
	})

	// 4. Delete Peer
	t.Run("DeletePeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "DeletePeer: createdPeer.PublicKey is empty.")

		w := httptest.NewRecorder()
		reqPath := fmt.Sprintf("/configs/%s", createdPeer.PublicKey)
		req, _ := http.NewRequest(http.MethodDelete, reqPath, nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "DeletePeer: expected 204 No Content status")

		_, err := fakeRepo.GetConfig(createdPeer.PublicKey)
		assert.ErrorIs(t, err, repository.ErrPeerNotFound, "DeletePeer: peer should no longer be found in the repository after deletion")
	})
}
