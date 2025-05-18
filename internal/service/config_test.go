// internal/service/config_test.go
package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap" // For logger in tests
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository" // For mock repository and its errors
)

// fakeRepository is a mock implementation of repository.Repo for service tests.
// This can be kept as is, or if you have a more general mock/test helper package, it could reside there.
type fakeRepository struct {
	configs               map[string]domain.Config
	CreateConfigError     error // To simulate errors from CreateConfig
	GetConfigError        error // To simulate errors from GetConfig
	ListConfigsError      error // To simulate errors from ListConfigs
	UpdateAllowedIPsError error
	DeleteConfigError     error
	// Add more fields to simulate specific error conditions if needed
}

// Ensure fakeRepository implements repository.Repo
var _ repository.Repo = &fakeRepository{}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		configs: make(map[string]domain.Config),
	}
}

func (r *fakeRepository) ListConfigs() ([]domain.Config, error) {
	if r.ListConfigsError != nil {
		return nil, r.ListConfigsError
	}
	list := make([]domain.Config, 0, len(r.configs))
	for _, cfg := range r.configs {
		list = append(list, cfg)
	}
	return list, nil
}

func (r *fakeRepository) GetConfig(publicKey string) (*domain.Config, error) {
	if r.GetConfigError != nil {
		return nil, r.GetConfigError
	}
	cfg, ok := r.configs[publicKey]
	if !ok {
		return nil, repository.ErrPeerNotFound // Use defined error
	}
	return &cfg, nil
}

func (r *fakeRepository) CreateConfig(cfg domain.Config) error {
	if r.CreateConfigError != nil {
		return r.CreateConfigError
	}
	// publicKey is essential for the map key
	if cfg.PublicKey == "" {
		return fmt.Errorf("mock repo: public key cannot be empty for CreateConfig")
	}
	if _, exists := r.configs[cfg.PublicKey]; exists {
		return fmt.Errorf("mock repo: peer with public key %s already exists", cfg.PublicKey)
	}
	r.configs[cfg.PublicKey] = cfg
	return nil
}

func (r *fakeRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	if r.UpdateAllowedIPsError != nil {
		return r.UpdateAllowedIPsError
	}
	cfg, ok := r.configs[publicKey]
	if !ok {
		return repository.ErrPeerNotFound
	}
	cfg.AllowedIps = allowedIps
	r.configs[publicKey] = cfg
	return nil
}

func (r *fakeRepository) DeleteConfig(publicKey string) error {
	if r.DeleteConfigError != nil {
		return r.DeleteConfigError
	}
	if _, ok := r.configs[publicKey]; !ok {
		// For testing, it might be useful for the mock to indicate if a non-existent peer was "deleted"
		// return repository.ErrPeerNotFound
		logger.Logger.Warn("FakeRepo (service_test): DeleteConfig called for non-existent peer, not returning error as per `wg` behavior.", zap.String("publicKey", publicKey))
	}
	delete(r.configs, publicKey)
	return nil
}

// setupTestService initializes ConfigService with a mock repository and test config values.
func setupTestService(t *testing.T, repo repository.Repo) *ConfigService {
	t.Helper()
	logger.Logger = zaptest.NewLogger(t) // Initialize logger for tests

	// Define test configuration values that would normally come from Viper/Config struct
	testServerInterfacePublicKey := "testServiceServerPubKey"
	testServerExternalEndpoint := "test-service.example.com:12345"
	testClientKeyGenCmdTimeout := 3 * time.Second // Shorter for tests if needed, or use DefaultKeyGenTimeoutService
	testDnsServersForClient := []string{"8.8.8.8", "8.8.4.4"}

	// If repo is nil, create a new fake one for this test
	if repo == nil {
		repo = newFakeRepository()
	}

	svc := NewConfigService(
		repo,
		testServerInterfacePublicKey,
		testServerExternalEndpoint,
		testClientKeyGenCmdTimeout,
		testDnsServersForClient,
	)
	return svc
}

func TestBuildClientConfig_Service(t *testing.T) { // Renamed to avoid conflict with handler tests if in same package
	mockRepo := newFakeRepository() // Not directly used by BuildClientConfig logic itself, but NewConfigService needs it
	svc := setupTestService(t, mockRepo)

	clientPeerConfig := &domain.Config{
		PublicKey:           "clientServiceTestPubKey",
		AllowedIps:          []string{"10.10.0.2/32"},
		PreSharedKey:        "clientServicePSK123",
		PersistentKeepalive: 22,
	}
	clientActualPrivateKey := "clientServiceTestPrivateKey"

	out, err := svc.BuildClientConfig(clientPeerConfig, clientActualPrivateKey)
	require.NoError(t, err, "BuildClientConfig should not return an error")

	expectedFragments := []string{
		fmt.Sprintf("PrivateKey = %s", clientActualPrivateKey),
		fmt.Sprintf("Address = %s", clientPeerConfig.AllowedIps[0]),
		fmt.Sprintf("PublicKey = %s", svc.serverBasePublicKey), // Check against svc's configured server public key
		fmt.Sprintf("Endpoint = %s", svc.serverBaseEndpoint),
		fmt.Sprintf("PresharedKey = %s", clientPeerConfig.PreSharedKey),
		fmt.Sprintf("PersistentKeepalive = %d", clientPeerConfig.PersistentKeepalive),
		fmt.Sprintf("DNS = %s", strings.Join(svc.clientConfigDNSServers, ", ")), // Check DNS
		"AllowedIPs = 0.0.0.0/0, ::/0",
	}

	for _, fragment := range expectedFragments {
		assert.Contains(t, out, fragment, "Output config should contain fragment: "+fragment)
	}

	// Test case: Missing clientPrivateKey
	_, err = svc.BuildClientConfig(clientPeerConfig, "")
	assert.Error(t, err, "BuildClientConfig should return error if clientPrivateKey is empty")
	assert.Contains(t, err.Error(), "client private key cannot be empty")

	// Test case: Missing peerCfg
	_, err = svc.BuildClientConfig(nil, clientActualPrivateKey)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg is nil")

	// Test case: Missing peerCfg.PublicKey
	brokenPeerCfg := &domain.Config{PrivateKey: clientActualPrivateKey, AllowedIps: []string{"10.0.0.1/32"}} // Need AllowedIps for Address line
	_, err = svc.BuildClientConfig(brokenPeerCfg, clientActualPrivateKey)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg.PublicKey is empty")
}

func TestCreateWithNewKeys_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo) // This now correctly sets up clientKeyGenTimeout

	allowedIPs := []string{"10.20.0.1/32"}
	psk := "newServicePeerPSK"
	keepalive := 33

	createdCfg, err := svc.CreateWithNewKeys(allowedIPs, psk, keepalive)
	require.NoError(t, err, "CreateWithNewKeys should not return an error")
	require.NotNil(t, createdCfg, "Returned config should not be nil")

	assert.NotEmpty(t, createdCfg.PublicKey, "Generated PublicKey should not be empty")
	assert.NotEmpty(t, createdCfg.PrivateKey, "Generated PrivateKey should not be empty and MUST be returned")
	assert.Equal(t, allowedIPs, createdCfg.AllowedIps, "AllowedIPs should match input")
	assert.Equal(t, psk, createdCfg.PreSharedKey, "PreSharedKey should match input")
	assert.Equal(t, keepalive, createdCfg.PersistentKeepalive, "PersistentKeepalive should match input")

	repoCfg, repoErr := mockRepo.GetConfig(createdCfg.PublicKey)
	require.NoError(t, repoErr, "Peer should be findable in repository after creation")
	require.NotNil(t, repoCfg, "Config from repo should not be nil")
	assert.Equal(t, createdCfg.PublicKey, repoCfg.PublicKey)
	assert.Empty(t, repoCfg.PrivateKey, "Repository should not store the client's private key")
}

func TestRotatePeerKey_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	oldPeerKey := "peerToRotateServicePubKey"
	oldPeer := domain.Config{
		PublicKey:           oldPeerKey,
		AllowedIps:          []string{"10.30.0.1/32"},
		PreSharedKey:        "oldServicePSK",
		PersistentKeepalive: 23,
	}
	mockRepo.configs[oldPeerKey] = oldPeer // Pre-populate the repo

	rotatedCfg, err := svc.RotatePeerKey(oldPeerKey)
	require.NoError(t, err, "RotatePeerKey should not return an error")
	require.NotNil(t, rotatedCfg, "Returned rotated config should not be nil")

	assert.NotEqual(t, oldPeerKey, rotatedCfg.PublicKey, "New PublicKey should be different from old one")
	assert.NotEmpty(t, rotatedCfg.PrivateKey, "New PrivateKey should be generated and returned")
	assert.Equal(t, oldPeer.AllowedIps, rotatedCfg.AllowedIps, "AllowedIPs should be preserved")
	assert.Equal(t, oldPeer.PreSharedKey, rotatedCfg.PreSharedKey, "PreSharedKey should be preserved")
	assert.Equal(t, oldPeer.PersistentKeepalive, rotatedCfg.PersistentKeepalive, "PersistentKeepalive should be preserved")

	_, err = mockRepo.GetConfig(oldPeerKey)
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Old peer should be deleted from repository")

	newRepoCfg, err := mockRepo.GetConfig(rotatedCfg.PublicKey)
	require.NoError(t, err, "New peer should be findable in repository")
	require.NotNil(t, newRepoCfg)
	assert.Empty(t, newRepoCfg.PrivateKey, "Repository should not store the new client's private key")
}

// TODO: Add more service tests:
// - TestGet_NotFound
// - TestCreate_RepoError (by setting mockRepo.CreateConfigError)
// - TestUpdateAllowedIPs_Success
// - TestUpdateAllowedIPs_NotFound
// - TestUpdateAllowedIPs_RepoError
// - TestDelete_Success
// - TestDelete_RepoError
// - TestRotatePeerKey_PeerNotFound
// - TestRotatePeerKey_KeyGenerationError (harder to mock 'wg genkey' failures without more complex mocks or OS-level command mocking)
// - TestRotatePeerKey_CreateNewPeerError
// - TestRotatePeerKey_DeleteOldPeerError
