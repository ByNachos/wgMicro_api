// internal/server/integration_test.go
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv" // Added for MTU test
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

const (
	testIntegrationServerPrivateKey = "BDFTfugHHNOHfPC3B4NSGfRmNE4zs+ZXM2ikT8//RUU="
	testIntegrationServerPublicKey  = "mK0477z4M24qLMVu2aSNwJjgCR97FPbyxsZ3+gx/NWg="
	testIntegrationWgInterface      = "wg_int_test"
	testIntegrationClientMTU        = 1400 // Example MTU for integration test
)

func setupIntegrationTestEnvironment(t *testing.T) (router *gin.Engine, repo repository.Repo, cleanupFunc func()) {
	t.Helper()
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	appConfig := &config.Config{
		AppEnv:      config.EnvTest,
		Port:        "0",
		WGInterface: testIntegrationWgInterface,
	}
	appConfig.Server.PrivateKey = testIntegrationServerPrivateKey
	appConfig.Server.PublicKey = testIntegrationServerPublicKey
	appConfig.Server.EndpointHost = "integration.test.vpn"
	appConfig.Server.EndpointPort = "51820"
	appConfig.Server.ListenPort = 51820
	appConfig.Server.InterfaceAddresses = []string{"10.99.99.1/24"}

	appConfig.ClientConfig.DNSServers = "1.1.1.1"
	appConfig.ClientConfig.MTU = testIntegrationClientMTU // Use constant for test

	appConfig.Timeouts.WgCmdSeconds = 5
	appConfig.Timeouts.KeyGenSeconds = 5

	appConfig.DerivedWgCmdTimeout = time.Duration(appConfig.Timeouts.WgCmdSeconds) * time.Second
	appConfig.DerivedKeyGenTimeout = time.Duration(appConfig.Timeouts.KeyGenSeconds) * time.Second
	appConfig.DerivedServerEndpoint = fmt.Sprintf("%s:%s", appConfig.Server.EndpointHost, appConfig.Server.EndpointPort)

	fakeRepo := repository.NewFakeWGRepository()
	var testRepo repository.Repo = fakeRepo

	svc := service.NewConfigService(
		testRepo,
		appConfig.Server.PublicKey,
		appConfig.DerivedServerEndpoint,
		appConfig.DerivedKeyGenTimeout,
		appConfig.ClientConfig.DNSServers,
		appConfig.ClientConfig.MTU, // Pass MTU
	)
	cfgHandler := handler.NewConfigHandler(svc)
	testRouter := NewRouter(cfgHandler, testRepo)

	cleanup := func() {}

	return testRouter, fakeRepo, cleanup
}

func TestIntegration_PeerLifecycle(t *testing.T) {
	router, fakeRepoImpl, cleanup := setupIntegrationTestEnvironment(t) // Renamed fakeRepoImpl to avoid confusion with interface
	defer cleanup()

	var createdPeer domain.Config

	// 1. Create Peer
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

		require.Equal(t, http.StatusCreated, w.Code)
		err := json.Unmarshal(w.Body.Bytes(), &createdPeer)
		require.NoError(t, err)
		assert.NotEmpty(t, createdPeer.PublicKey)
		assert.NotEmpty(t, createdPeer.PrivateKey)
		assert.Equal(t, createReqBody.AllowedIps, createdPeer.AllowedIps)
	})

	// 2. Get Peer
	t.Run("GetPeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey)
		
		getReqBody := domain.GetConfigRequest{
			PublicKey: createdPeer.PublicKey,
		}
		bodyBytes, _ := json.Marshal(getReqBody)
		
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs/get", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		var fetchedPeer domain.Config
		err := json.Unmarshal(w.Body.Bytes(), &fetchedPeer)
		require.NoError(t, err)
		assert.Equal(t, createdPeer.PublicKey, fetchedPeer.PublicKey)
		assert.Empty(t, fetchedPeer.PrivateKey)
		assert.Equal(t, createdPeer.AllowedIps, fetchedPeer.AllowedIps)
	})

	// 3. Generate Client Config File
	t.Run("GenerateClientFile", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey)
		require.NotEmpty(t, createdPeer.PrivateKey)

		fileReq := domain.ClientFileRequest{
			ClientPublicKey:  createdPeer.PublicKey,
			ClientPrivateKey: createdPeer.PrivateKey,
		}
		bodyBytes, _ := json.Marshal(fileReq)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs/client-file", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		confContent := w.Body.String()
		assert.Contains(t, confContent, fmt.Sprintf("PrivateKey = %s", createdPeer.PrivateKey))
		assert.Contains(t, confContent, fmt.Sprintf("PublicKey = %s", testIntegrationServerPublicKey))
		assert.Contains(t, confContent, "Endpoint = integration.test.vpn:51820")
		assert.Contains(t, confContent, "DNS = 1.1.1.1")
		// Check for MTU line
		if testIntegrationClientMTU > 0 {
			expectedMTULine := fmt.Sprintf("MTU = %s", strconv.Itoa(testIntegrationClientMTU))
			assert.Contains(t, confContent, expectedMTULine, "Client conf should contain MTU line")
		} else {
			assert.NotContains(t, confContent, "MTU =", "Client conf should not contain MTU line if MTU is 0")
		}
	})

	// 4. Delete Peer
	t.Run("DeletePeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey)
		
		deleteReqBody := domain.DeleteConfigRequest{
			PublicKey: createdPeer.PublicKey,
		}
		bodyBytes, _ := json.Marshal(deleteReqBody)
		
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/configs/delete", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		// Ensure fakeRepoImpl is of type *repository.FakeWGRepository to access its methods/fields if needed,
		// or use the repository.Repo interface methods.
		// Here, we assume fakeRepoImpl is the concrete *repository.FakeWGRepository instance.
		if concreteFakeRepo, ok := fakeRepoImpl.(*repository.FakeWGRepository); ok { // Type assertion
			_, err := concreteFakeRepo.GetConfig(createdPeer.PublicKey)
			assert.Error(t, err, "DeletePeer: peer should no longer be found in the repository after deletion") // Adjusted for FakeWGRepository's GetConfig
			// Depending on FakeWGRepository's GetConfig error for not found, you might need:
			// assert.EqualError(t, err, "not found", "Expected 'not found' error from FakeWGRepository")
			// Or if FakeWGRepository returns repository.ErrPeerNotFound, then use:
			// assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Expected ErrPeerNotFound from FakeWGRepository")
			// Since our fake repo returns fmt.Errorf("not found"), we check for a generic error or specific string.
			// Let's assume FakeWGRepository was updated to return repository.ErrPeerNotFound for consistency.
			assert.ErrorIs(t, err, repository.ErrPeerNotFound, "DeletePeer: peer should no longer be found in the repository after deletion")
		} else {
			t.Fatal("fakeRepoImpl is not of type *repository.FakeWGRepository, cannot verify deletion properly")
		}
	})
}
