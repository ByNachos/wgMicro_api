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

// setupIntegrationTestEnvironment configures the environment for an integration test.
// It no longer creates a temporary wg config file for server keys.
func setupIntegrationTestEnvironment(t *testing.T) (router *gin.Engine, repo *repository.FakeWGRepository, cleanupFunc func()) {
	t.Helper()
	logger.Logger = zaptest.NewLogger(t) // Initialize logger for tests
	gin.SetMode(gin.TestMode)

	// Create an appConfig for the test, simulating what Viper would load.
	// We directly set the server's private and public keys for test predictability.
	appConfig := &config.Config{
		AppEnv:      config.EnvTest, // Use test environment
		Port:        "0",            // Let the system pick a port for the test server
		WGInterface: testIntegrationWgInterface,
		Server: struct {
			PrivateKey         string `mapstructure:"PRIVATE_KEY"`
			PublicKey          string
			EndpointHost       string   `mapstructure:"ENDPOINT_HOST"`
			EndpointPort       string   `mapstructure:"ENDPOINT_PORT"`
			ListenPort         int      `mapstructure:"LISTEN_PORT"`
			InterfaceAddresses []string `mapstructure:"INTERFACE_ADDRESSES"`
		}{
			PrivateKey:         testIntegrationServerPrivateKey, // Set directly for test
			PublicKey:          testIntegrationServerPublicKey,  // Set directly for test
			EndpointHost:       "integration.test.vpn",
			EndpointPort:       "51820",
			ListenPort:         51820,                     // Informational for test
			InterfaceAddresses: []string{"10.99.99.1/24"}, // Informational for test
		},
		ClientConfig: struct {
			DNSServers []string `mapstructure:"DNS_SERVERS"`
		}{
			DNSServers: []string{"1.1.1.1", "1.0.0.1"}, // Example DNS for tests
		},
		Timeouts: struct {
			WgCmdSeconds  int `mapstructure:"WG_CMD_TIMEOUT_SECONDS"`
			KeyGenSeconds int `mapstructure:"KEY_GEN_TIMEOUT_SECONDS"`
		}{
			WgCmdSeconds:  5,
			KeyGenSeconds: 5,
		},
		// Manually set derived fields that LoadConfig would normally populate
		DerivedWgCmdTimeout:   5 * time.Second,
		DerivedKeyGenTimeout:  5 * time.Second,
		DerivedServerEndpoint: "integration.test.vpn:51820",
	}

	// We could also call a helper that mimics parts of config.LoadConfig() to derive PublicKey
	// and timeouts if we only set PrivateKey and timeout seconds, but direct set is fine for test control.
	// For example:
	// appConfig.Server.PublicKey, _ = config.DerivePublicKey(appConfig.Server.PrivateKey, appConfig.DerivedKeyGenTimeout) // If DerivePublicKey was public

	// Using FakeWGRepository for isolation from actual 'wg' commands for peer management
	// to test the HTTP -> Service -> (Fake)Repo flow.
	// The actual 'wg' utility interaction for key derivation is now part of config loading,
	// which we are mocking/simulating here by providing the keys directly.
	fakeRepo := repository.NewFakeWGRepository()

	svc := service.NewConfigService(
		fakeRepo,
		appConfig.Server.PublicKey,        // From our test config
		appConfig.DerivedServerEndpoint,   // From our test config
		appConfig.DerivedKeyGenTimeout,    // From our test config
		appConfig.ClientConfig.DNSServers, // From our test config
	)
	cfgHandler := handler.NewConfigHandler(svc)

	// Use the application's router setup logic
	testRouter := NewRouter(cfgHandler, fakeRepo) // Pass fakeRepo for readiness probe

	cleanup := func() {
		// No file cleanup needed for wg config anymore
	}

	return testRouter, fakeRepo, cleanup
}

// TestIntegration_PeerLifecycle_WithViperConfig checks the main peer lifecycle using the new config approach.
func TestIntegration_PeerLifecycle_WithViperConfig(t *testing.T) {
	router, fakeRepo, cleanup := setupIntegrationTestEnvironment(t)
	defer cleanup()

	var createdPeer domain.Config // To store the peer created in the first subtest

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
		// You can also check PSK and Keepalive if they were part of the request and response structure
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
			ClientPrivateKey: createdPeer.PrivateKey, // Client provides its private key for this
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
		assert.Contains(t, confContent, "DNS = 1.1.1.1, 1.0.0.1", "Client conf should contain configured DNS servers")
	})

	// 4. Delete Peer
	t.Run("DeletePeer", func(t *testing.T) {
		require.NotEmpty(t, createdPeer.PublicKey, "DeletePeer: createdPeer.PublicKey is empty.")

		w := httptest.NewRecorder()
		reqPath := fmt.Sprintf("/configs/%s", createdPeer.PublicKey)
		req, _ := http.NewRequest(http.MethodDelete, reqPath, nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code, "DeletePeer: expected 204 No Content status")

		// Verify deletion in the fake repository
		_, err := fakeRepo.GetConfig(createdPeer.PublicKey)
		assert.ErrorIs(t, err, repository.ErrPeerNotFound, "DeletePeer: peer should no longer be found in the repository after deletion")
	})
}
