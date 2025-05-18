package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/config"
	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/serverkeys"
	"wgMicro_api/internal/service"
)

// Эти ключи должны быть валидной парой. Сгенерируй их один раз и используй.
// wg genkey | tee test_server_private.key | wg pubkey > test_server_public.key
const (
	testIntegrationServerPrivateKey = "BDFTfugHHNOHfPC3B4NSGfRmNE4zs+ZXM2ikT8//RUU=" // ЗАМЕНИ НА СВОЙ СГЕНЕРИРОВАННЫЙ
	testIntegrationServerPublicKey  = "mK0477z4M24qLMVu2aSNwJjgCR97FPbyxsZ3+gx/NWg=" // ЗАМЕНИ НА СООТВЕТСТВУЮЩИЙ ПУБЛИЧНЫЙ
	testIntegrationWgInterface      = "wg_int_test"
)

// createTestWgConfigFileForIntegrationTest создает временный wg0.conf для интеграционных тестов.
func createTestWgConfigFileForIntegrationTest(t *testing.T, privateKey string) string {
	t.Helper()
	// Используем t.TempDir() для автоматической очистки после теста
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "test_wg_server.conf")

	// Содержимое файла только с секцией Interface
	content := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = 10.90.90.1/24\nListenPort = 51820\n", privateKey)
	err := os.WriteFile(confPath, []byte(content), 0600)
	require.NoError(t, err, "Failed to write temporary wg server config file for integration test")
	return confPath
}

// setupIntegrationTestEnvironment настраивает окружение для интеграционного теста.
func setupIntegrationTestEnvironment(t *testing.T) (router *gin.Engine, repo *repository.FakeWGRepository, cleanupFunc func()) {
	t.Helper()
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	testWgConfPath := createTestWgConfigFileForIntegrationTest(t, testIntegrationServerPrivateKey)

	// Создаем appConfig для теста
	appConfig := &config.Config{
		AppEnv:         config.EnvDevelopment,
		Port:           "0", // Автоматический выбор порта
		WGInterface:    testIntegrationWgInterface,
		WGConfigPath:   testWgConfPath, // Используем временный файл
		ServerEndpoint: "integration.test:12345",
		WgCmdTimeout:   5 * time.Second,
		KeyGenTimeout:  5 * time.Second,
	}

	// Инициализируем ServerKeyManager
	skm, err := serverkeys.NewServerKeyManager(appConfig.WGConfigPath, appConfig.KeyGenTimeout)
	require.NoError(t, err, "Integration Test: ServerKeyManager initialization failed")
	serverPubKey, err := skm.GetServerPublicKey()
	require.NoError(t, err, "Integration Test: Failed to get server public key")
	require.Equal(t, testIntegrationServerPublicKey, serverPubKey, "Integration Test: Derived server public key mismatch")

	// Используем FakeWGRepository для изоляции от реальных команд wg set/show для пиров в этом тесте
	// Мы тестируем HTTP -> Service -> (Fake)Repo связку
	fakeRepo := repository.NewFakeWGRepository()

	svc := service.NewConfigService(
		fakeRepo,
		serverPubKey,
		appConfig.ServerEndpoint,
		appConfig.KeyGenTimeout,
	)
	cfgHandler := handler.NewConfigHandler(svc)

	// Используем реальный роутер приложения
	testRouter := NewRouter(cfgHandler, fakeRepo) // Передаем fakeRepo для readiness probe

	cleanup := func() {
		// t.TempDir() автоматически удалит временную директорию и файл
	}

	return testRouter, fakeRepo, cleanup
}

// TestIntegration_PeerLifecycle_WithServerKeyManager проверяет основной жизненный цикл пира.
func TestIntegration_PeerLifecycle_WithServerKeyManager(t *testing.T) {
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

		require.Equal(t, http.StatusCreated, w.Code, "CreatePeer: expected 201")
		err := json.Unmarshal(w.Body.Bytes(), &createdPeer)
		require.NoError(t, err, "CreatePeer: failed to unmarshal response")
		assert.NotEmpty(t, createdPeer.PublicKey, "CreatePeer: PublicKey should not be empty")
		assert.NotEmpty(t, createdPeer.PrivateKey, "CreatePeer: PrivateKey should be returned")
		assert.Equal(t, createReqBody.AllowedIps, createdPeer.AllowedIps)
	})

	// 2. Get Peer
	t.Run("GetPeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "GetPeer: createdPeer.PublicKey is empty, cannot proceed")
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/configs/"+createdPeer.PublicKey, nil)
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "GetPeer: expected 200")
		var fetchedPeer domain.Config
		err := json.Unmarshal(w.Body.Bytes(), &fetchedPeer)
		require.NoError(t, err, "GetPeer: failed to unmarshal response")
		assert.Equal(t, createdPeer.PublicKey, fetchedPeer.PublicKey)
		assert.Empty(t, fetchedPeer.PrivateKey, "GetPeer: PrivateKey should not be returned by this endpoint")
	})

	// 3. Generate Client Config File
	t.Run("GenerateClientFile", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "GenerateClientFile: createdPeer.PublicKey is empty")
		require.NotEmpty(t, createdPeer.PrivateKey, "GenerateClientFile: createdPeer.PrivateKey is empty (needed from create step)")

		fileReq := domain.ClientFileRequest{
			ClientPublicKey:  createdPeer.PublicKey,
			ClientPrivateKey: createdPeer.PrivateKey,
		}
		bodyBytes, _ := json.Marshal(fileReq)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "GenerateClientFile: expected 200")
		confContent := w.Body.String()
		assert.Contains(t, confContent, fmt.Sprintf("PrivateKey = %s", createdPeer.PrivateKey))
		assert.Contains(t, confContent, fmt.Sprintf("PublicKey = %s", testIntegrationServerPublicKey)) // Server's pub key
	})

	// 4. Delete Peer
	t.Run("DeletePeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "DeletePeer: createdPeer.PublicKey is empty")
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodDelete, "/configs/"+createdPeer.PublicKey, nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, "DeletePeer: expected 204")

		// Verify deletion in fake repo
		_, err := fakeRepo.GetConfig(createdPeer.PublicKey)
		assert.ErrorIs(t, err, repository.ErrPeerNotFound, "DeletePeer: peer should be not found in repo after deletion")
	})
}
