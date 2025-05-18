package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository" // Для мока репозитория
)

// fakeRepository is a mock implementation of repository.Repo for service tests.
type fakeRepository struct {
	// Store configs to simulate GetConfig, ListConfigs, etc.
	configs map[string]domain.Config
	// You can add more fields or function fields to control behavior
	// e.g., CreateConfigError error
}

// Ensure fakeRepository implements repository.Repo
var _ repository.Repo = &fakeRepository{}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		configs: make(map[string]domain.Config),
	}
}

func (r *fakeRepository) ListConfigs() ([]domain.Config, error) {
	list := make([]domain.Config, 0, len(r.configs))
	for _, cfg := range r.configs {
		list = append(list, cfg)
	}
	return list, nil
}

func (r *fakeRepository) GetConfig(publicKey string) (*domain.Config, error) {
	cfg, ok := r.configs[publicKey]
	if !ok {
		return nil, repository.ErrPeerNotFound // Use defined error
	}
	return &cfg, nil
}

func (r *fakeRepository) CreateConfig(cfg domain.Config) error {
	if _, exists := r.configs[cfg.PublicKey]; exists {
		return fmt.Errorf("peer with public key %s already exists", cfg.PublicKey)
	}
	r.configs[cfg.PublicKey] = cfg
	return nil
}

func (r *fakeRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	cfg, ok := r.configs[publicKey]
	if !ok {
		return repository.ErrPeerNotFound
	}
	cfg.AllowedIps = allowedIps
	r.configs[publicKey] = cfg
	return nil
}

func (r *fakeRepository) DeleteConfig(publicKey string) error {
	if _, ok := r.configs[publicKey]; !ok {
		// 'wg set ... remove' doesn't usually error on not found, but for mock it can be useful
		// return repository.ErrPeerNotFound
		logger.Logger.Warn("FakeRepo: DeleteConfig called for non-existent peer, but not returning error.", zap.String("publicKey", publicKey))
	}
	delete(r.configs, publicKey)
	return nil
}

func TestBuildClientConfig(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)

	// Mock repository (needed by NewConfigService, though not directly by this specific method logic if it doesn't call repo)
	mockRepo := newFakeRepository()

	// Server details that would come from ServerKeyManager and Config
	testServerPublicKey := "testServerPubKey"
	testServerEndpoint := "test.example.com:51820"
	testClientKeyGenTimeout := 5 * time.Second

	svc := NewConfigService(mockRepo, testServerPublicKey, testServerEndpoint, testClientKeyGenTimeout)

	clientPeerConfig := &domain.Config{
		// This peerCfg is what `Get(clientPubKey)` would return from the repository for an existing peer.
		// It typically does NOT contain the client's private key.
		PublicKey:           "testClientPubKey",
		AllowedIps:          []string{"10.1.0.2/32"}, // This is what the *server* allows *from* this client
		PreSharedKey:        "clientPSK123",
		PersistentKeepalive: 25,
	}
	// The client's actual private key is passed separately for this operation.
	clientActualPrivateKey := "testClientPrivateKey"

	out, err := svc.BuildClientConfig(clientPeerConfig, clientActualPrivateKey)
	require.NoError(t, err, "BuildClientConfig should not return an error")

	expectedFragments := []string{
		fmt.Sprintf("PrivateKey = %s", clientActualPrivateKey),
		fmt.Sprintf("Address = %s", clientPeerConfig.AllowedIps[0]), // Client uses its first allowed IP as its address
		fmt.Sprintf("PublicKey = %s", testServerPublicKey),          // Server's public key
		fmt.Sprintf("Endpoint = %s", testServerEndpoint),
		fmt.Sprintf("PresharedKey = %s", clientPeerConfig.PreSharedKey),
		fmt.Sprintf("PersistentKeepalive = %d", clientPeerConfig.PersistentKeepalive),
		"AllowedIPs = 0.0.0.0/0, ::/0", // Client's peer section typically routes all traffic
	}

	for _, fragment := range expectedFragments {
		assert.Contains(t, out, fragment, "Output config should contain fragment")
	}

	// Test case: Missing clientPrivateKey
	_, err = svc.BuildClientConfig(clientPeerConfig, "")
	assert.Error(t, err, "BuildClientConfig should return error if clientPrivateKey is empty")
	assert.Contains(t, err.Error(), "client private key cannot be empty", "Error message should indicate missing private key")

	// Test case: Missing peerCfg
	_, err = svc.BuildClientConfig(nil, clientActualPrivateKey)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg is nil")

	// Test case: Missing peerCfg.PublicKey
	brokenPeerCfg := &domain.Config{PrivateKey: clientActualPrivateKey}
	_, err = svc.BuildClientConfig(brokenPeerCfg, clientActualPrivateKey)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg.PublicKey is empty")

}

func TestCreateWithNewKeys(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	mockRepo := newFakeRepository()

	testServerPublicKey := "testServerPubKeyForCreate"
	testServerEndpoint := "create.example.com:51820"
	testClientKeyGenTimeout := 5 * time.Second

	svc := NewConfigService(mockRepo, testServerPublicKey, testServerEndpoint, testClientKeyGenTimeout)

	allowedIPs := []string{"10.2.0.1/32"}
	psk := "newPeerPSK"
	keepalive := 30

	createdCfg, err := svc.CreateWithNewKeys(allowedIPs, psk, keepalive)
	require.NoError(t, err, "CreateWithNewKeys should not return an error")
	require.NotNil(t, createdCfg, "Returned config should not be nil")

	assert.NotEmpty(t, createdCfg.PublicKey, "Generated PublicKey should not be empty")
	assert.NotEmpty(t, createdCfg.PrivateKey, "Generated PrivateKey should not be empty and MUST be returned")
	assert.Equal(t, allowedIPs, createdCfg.AllowedIps, "AllowedIPs should match input")
	assert.Equal(t, psk, createdCfg.PreSharedKey, "PreSharedKey should match input")
	assert.Equal(t, keepalive, createdCfg.PersistentKeepalive, "PersistentKeepalive should match input")

	// Verify that the peer was actually added to the repository (via the mock)
	repoCfg, repoErr := mockRepo.GetConfig(createdCfg.PublicKey)
	require.NoError(t, repoErr, "Peer should be findable in repository after creation")
	require.NotNil(t, repoCfg, "Config from repo should not be nil")
	assert.Equal(t, createdCfg.PublicKey, repoCfg.PublicKey)
	assert.Equal(t, allowedIPs, repoCfg.AllowedIps)
	// Note: The mockRepo.CreateConfig only stores what's passed in domain.Config.
	// The PrivateKey isn't stored by CreateConfig in the repo, only returned by the service. This is correct.
	assert.Empty(t, repoCfg.PrivateKey, "Repository should not store the client's private key")
}

func TestRotatePeerKey(t *testing.T) {
	logger.Logger = zaptest.NewLogger(t)
	mockRepo := newFakeRepository()

	oldPeerKey := "peerToRotatePubKey"
	oldPeer := domain.Config{
		PublicKey:           oldPeerKey,
		AllowedIps:          []string{"10.3.0.1/32"},
		PreSharedKey:        "oldPSK",
		PersistentKeepalive: 21,
	}
	mockRepo.configs[oldPeerKey] = oldPeer // Pre-populate the repo

	testServerPublicKey := "testServerPubKeyForRotate"
	testServerEndpoint := "rotate.example.com:51820"
	testClientKeyGenTimeout := 5 * time.Second

	svc := NewConfigService(mockRepo, testServerPublicKey, testServerEndpoint, testClientKeyGenTimeout)

	rotatedCfg, err := svc.RotatePeerKey(oldPeerKey)
	require.NoError(t, err, "RotatePeerKey should not return an error")
	require.NotNil(t, rotatedCfg, "Returned rotated config should not be nil")

	assert.NotEmpty(t, rotatedCfg.PublicKey, "New PublicKey should not be empty")
	assert.NotEqual(t, oldPeerKey, rotatedCfg.PublicKey, "New PublicKey should be different from old one")
	assert.NotEmpty(t, rotatedCfg.PrivateKey, "New PrivateKey should be generated and returned")
	assert.Equal(t, oldPeer.AllowedIps, rotatedCfg.AllowedIps, "AllowedIPs should be preserved")
	assert.Equal(t, oldPeer.PreSharedKey, rotatedCfg.PreSharedKey, "PreSharedKey should be preserved (as per current logic)")
	assert.Equal(t, oldPeer.PersistentKeepalive, rotatedCfg.PersistentKeepalive, "PersistentKeepalive should be preserved")

	// Verify old peer is deleted from repo
	_, err = mockRepo.GetConfig(oldPeerKey)
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Old peer should be deleted from repository")

	// Verify new peer is created in repo
	newRepoCfg, err := mockRepo.GetConfig(rotatedCfg.PublicKey)
	require.NoError(t, err, "New peer should be findable in repository")
	require.NotNil(t, newRepoCfg)
	assert.Equal(t, rotatedCfg.PublicKey, newRepoCfg.PublicKey)
	assert.Empty(t, newRepoCfg.PrivateKey, "Repository should not store the new client's private key")
}

// TODO: Add more service tests:
// - TestGet_NotFound
// - TestCreate_RepoError
// - TestUpdateAllowedIPs_NotFound
// - TestUpdateAllowedIPs_RepoError
// - TestDelete_NotFound (if repo mock is changed to return error)
// - TestDelete_RepoError
// - TestRotatePeerKey_PeerNotFound
// - TestRotatePeerKey_KeyGenerationError (how to mock 'wg genkey' failure?)
// - TestRotatePeerKey_CreateNewPeerError
// - TestRotatePeerKey_DeleteOldPeerError
