// internal/service/config_test.go
package service

import (
	"errors"
	"fmt"
	"strconv" // Added for MTU tests
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
	configs           map[string]domain.Config
	CreateConfigError error
	GetConfigError    error
	ListConfigsError  error
	// UpdateAllowedIPsError error // Можно удалить или оставить для простых случаев
	DeleteConfigError error

	// Новые поля-функции для более тонкой настройки мока
	UpdateAllowedIPsFunc func(publicKey string, allowedIps []string) error
	// Добавь аналогичные поля-функции для GetConfig, ListConfigs, CreateConfig, DeleteConfig, если нужно
	// GetConfigFunc func(publicKey string) (*domain.Config, error)
	// ListConfigsFunc func() ([]domain.Config, error)
	// CreateConfigFunc func(cfg domain.Config) error
	// DeleteConfigFunc func(publicKey string) error
	DeleteFunc       func(publicKey string) error
	GetConfigFunc    func(publicKey string) (*domain.Config, error)
	CreateConfigFunc func(cfg domain.Config) error
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
	if r.GetConfigFunc != nil { // Если кастомная функция задана, вызываем ее
		return r.GetConfigFunc(publicKey)
	}
	// Поведение по умолчанию, если GetConfigFunc не задана
	if r.GetConfigError != nil { // Если хотим использовать старый механизм ошибок
		return nil, r.GetConfigError
	}
	cfg, ok := r.configs[publicKey]
	if !ok {
		return nil, repository.ErrPeerNotFound
	}
	return &cfg, nil
}

func (r *fakeRepository) CreateConfig(cfg domain.Config) error {
	if r.CreateConfigFunc != nil { // <--- Используем CreateConfigFunc
		return r.CreateConfigFunc(cfg)
	}
	// Поведение по умолчанию, если CreateConfigFunc не задана
	if r.CreateConfigError != nil {
		return r.CreateConfigError
	}
	if cfg.PublicKey == "" {
		return fmt.Errorf("mock repo: public key cannot be empty for CreateConfig")
	}
	// Для `wg set` обновление существующего пира - это норма.
	// Если мы хотим строго имитировать "создание", то ошибка при существующем ключе оправдана.
	// Но для ротации это может быть не то, что нужно, т.к. новый ключ не должен существовать.
	// В данном случае, если ключ уже есть, это ошибка для "чистого" создания.
	if _, exists := r.configs[cfg.PublicKey]; exists {
		// В контексте RotatePeerKey, если сюда попадает уже существующий НОВЫЙ ключ (маловероятно из-за генерации),
		// это была бы проблема. Но wg genkey должен давать уникальные ключи.
		// Если это старый ключ, то это не вызовется, т.к. repoPeerCfgForCreate имеет новый Public Key.
		logger.Logger.Warn("FakeRepo: CreateConfig called for an already existing key, which is unexpected for a 'new' peer config.", zap.String("publicKey", cfg.PublicKey))
		// Можно вернуть ошибку, если это действительно проблема для логики теста
		// return fmt.Errorf("mock repo: peer with public key %s already exists", cfg.PublicKey)
	}
	r.configs[cfg.PublicKey] = cfg
	return nil
}

// internal/service/config_test.go
func (r *fakeRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	if r.UpdateAllowedIPsFunc != nil { // Вызываем функцию из поля, если она задана
		return r.UpdateAllowedIPsFunc(publicKey, allowedIps)
	}
	// Старая логика с UpdateAllowedIPsError или поведение по умолчанию
	// if r.UpdateAllowedIPsError != nil {
	// 	return r.UpdateAllowedIPsError
	// }
	cfg, ok := r.configs[publicKey]
	if !ok {
		return repository.ErrPeerNotFound
	}
	cfg.AllowedIps = allowedIps
	r.configs[publicKey] = cfg
	return nil
}

func (r *fakeRepository) DeleteConfig(publicKey string) error {
	if r.DeleteFunc != nil { // Если кастомная функция задана, вызываем ее
		return r.DeleteFunc(publicKey)
	}
	// Поведение по умолчанию, если DeleteFunc не задана
	// (можно использовать r.DeleteConfigError, если он оставлен)
	if r.DeleteConfigError != nil {
		return r.DeleteConfigError
	}

	// Оригинальное поведение warn + delete
	if _, ok := r.configs[publicKey]; !ok {
		logger.Logger.Warn("FakeRepo (service_test): DeleteConfig called for non-existent peer, not returning error as per `wg` behavior.", zap.String("publicKey", publicKey))
	}
	delete(r.configs, publicKey) // Просто удаляем, `wg` обычно не жалуется, если пира нет
	return nil
}

// setupTestService initializes ConfigService with a mock repository and test config values.
func setupTestService(t *testing.T, repo repository.Repo, clientMTU int) *ConfigService { // Added clientMTU
	t.Helper()
	logger.Logger = zaptest.NewLogger(t) // Initialize logger for tests

	testServerInterfacePublicKey := "testServiceServerPubKey"
	testServerExternalEndpoint := "test-service.example.com:12345"
	testClientKeyGenCmdTimeout := 3 * time.Second
	testDnsServersForClient := "1.1.1.1"

	if repo == nil {
		repo = newFakeRepository()
	}

	svc := NewConfigService(
		repo,
		testServerInterfacePublicKey,
		testServerExternalEndpoint,
		testClientKeyGenCmdTimeout,
		testDnsServersForClient,
		clientMTU, // Pass MTU
	)
	return svc
}

func TestBuildClientConfig_Service(t *testing.T) {
	mockRepo := newFakeRepository() // Not directly used by BuildClientConfig logic itself

	testCases := []struct {
		name             string
		clientMTU        int
		expectMTULine    bool
		expectedMTUValue int
	}{
		{
			name:             "With_MTU_1420",
			clientMTU:        1420,
			expectMTULine:    true,
			expectedMTUValue: 1420,
		},
		{
			name:             "With_MTU_0_omitted",
			clientMTU:        0,
			expectMTULine:    false,
			expectedMTUValue: 0,
		},
		{
			name:             "With_MTU_1300",
			clientMTU:        1300,
			expectMTULine:    true,
			expectedMTUValue: 1300,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := setupTestService(t, mockRepo, tc.clientMTU) // Pass MTU to service setup

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
				fmt.Sprintf("PublicKey = %s", svc.serverBasePublicKey),
				fmt.Sprintf("Endpoint = %s", svc.serverBaseEndpoint),
				fmt.Sprintf("PresharedKey = %s", clientPeerConfig.PreSharedKey),
				fmt.Sprintf("PersistentKeepalive = %d", clientPeerConfig.PersistentKeepalive),
				fmt.Sprintf("DNS = %s", svc.clientConfigDNSServers),
				"AllowedIPs = 0.0.0.0/0, ::/0",
			}

			for _, fragment := range expectedFragments {
				assert.Contains(t, out, fragment, "Output config should contain fragment: "+fragment)
			}

			// Check MTU line
			mtuLine := fmt.Sprintf("MTU = %s", strconv.Itoa(tc.expectedMTUValue))
			if tc.expectMTULine {
				assert.Contains(t, out, mtuLine, "Output config should contain MTU line: "+mtuLine)
			} else {
				assert.NotContains(t, out, "MTU =", "Output config should NOT contain MTU line when MTU is 0")
			}
		})
	}

	// Test error cases (run once, as they are independent of MTU value)
	svcForErrorTests := setupTestService(t, mockRepo, 0) // MTU value doesn't matter for these
	clientPeerConfigForError := &domain.Config{PublicKey: "errorPeerKey", AllowedIps: []string{"10.0.0.1/32"}}
	clientPrivateKeyForError := "errorClientPrivKey"

	_, err := svcForErrorTests.BuildClientConfig(clientPeerConfigForError, "")
	assert.Error(t, err, "BuildClientConfig should return error if clientPrivateKey is empty")
	assert.Contains(t, err.Error(), "client private key cannot be empty")

	_, err = svcForErrorTests.BuildClientConfig(nil, clientPrivateKeyForError)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg is nil")

	brokenPeerCfg := &domain.Config{PrivateKey: clientPrivateKeyForError, AllowedIps: []string{"10.0.0.1/32"}}
	_, err = svcForErrorTests.BuildClientConfig(brokenPeerCfg, clientPrivateKeyForError)
	assert.Error(t, err, "BuildClientConfig should return error if peerCfg.PublicKey is empty")
}

func TestCreateWithNewKeys_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	// MTU value doesn't affect CreateWithNewKeys logic directly, so passing 0 or any valid value.
	svc := setupTestService(t, mockRepo, 0)

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
	svc := setupTestService(t, mockRepo, 0) // MTU not directly relevant here

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

// TestGet_NotFound tests fetching a non-existent peer from the service.
func TestGet_NotFound(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	nonExistentKey := "someNonExistentKey"
	mockRepo.GetConfigError = repository.ErrPeerNotFound

	config, err := svc.Get(nonExistentKey)

	require.Error(t, err, "Expected an error when getting a non-existent peer")
	assert.Nil(t, config, "Expected config to be nil on error")
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Expected ErrPeerNotFound")
}

func TestCreateWithNewKeys_RepoError(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	allowedIPs := []string{"10.20.0.5/32"}
	psk := "newServicePeerPSKErrorCase"
	keepalive := 30

	simulatedRepoErrorMessage := "repository failed to create config"
	mockRepo.CreateConfigError = errors.New(simulatedRepoErrorMessage)

	createdCfg, err := svc.CreateWithNewKeys(allowedIPs, psk, keepalive)

	require.Error(t, err, "Expected an error when repository fails to create config")
	assert.Nil(t, createdCfg, "Returned config should be nil on repository error")
	assert.Contains(t, err.Error(), simulatedRepoErrorMessage, "Error message should contain the repository's error")
}

func TestUpdateAllowedIPs_Success_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	targetPublicKey := "peerToUpdateIPsInService"
	newIPs := []string{"192.168.200.1/32", "fd00::100/128"}
	mockRepo.configs[targetPublicKey] = domain.Config{PublicKey: targetPublicKey, AllowedIps: []string{"10.0.0.1/32"}}

	repoUpdateCalled := false
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error {
		repoUpdateCalled = true
		assert.Equal(t, targetPublicKey, key)
		assert.Equal(t, newIPs, ips)
		cfg, ok := mockRepo.configs[key]
		if !ok {
			return repository.ErrPeerNotFound
		}
		cfg.AllowedIps = ips
		mockRepo.configs[key] = cfg
		return nil
	}

	err := svc.UpdateAllowedIPs(targetPublicKey, newIPs)
	require.NoError(t, err)
	assert.True(t, repoUpdateCalled)
	updatedPeerConfig, _ := mockRepo.GetConfig(targetPublicKey)
	require.NotNil(t, updatedPeerConfig)
	assert.Equal(t, newIPs, updatedPeerConfig.AllowedIps)
}

func TestUpdateAllowedIPs_NotFound_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	nonExistentPeerKey := "nonExistentPeerForUpdateService"
	newIPs := []string{"172.16.10.1/32"}

	repoUpdateCalled := false
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error {
		repoUpdateCalled = true
		assert.Equal(t, nonExistentPeerKey, key)
		return repository.ErrPeerNotFound
	}

	err := svc.UpdateAllowedIPs(nonExistentPeerKey, newIPs)
	require.Error(t, err)
	assert.True(t, repoUpdateCalled)
	assert.ErrorIs(t, err, repository.ErrPeerNotFound)
}

func TestUpdateAllowedIPs_RepoError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	targetPeerKey := "peerForUpdateRepoError"
	newIPs := []string{"172.16.20.1/32"}
	simulatedRepoError := fmt.Errorf("simulated generic repository error during IP update")
	mockRepo.configs[targetPeerKey] = domain.Config{PublicKey: targetPeerKey}

	repoUpdateCalled := false
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error {
		repoUpdateCalled = true
		assert.Equal(t, targetPeerKey, key)
		return simulatedRepoError
	}

	err := svc.UpdateAllowedIPs(targetPeerKey, newIPs)
	require.Error(t, err)
	assert.True(t, repoUpdateCalled)
	assert.Equal(t, simulatedRepoError, err)
}

func TestDelete_Success_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	targetPublicKey := "peerToDeleteInService"
	mockRepo.configs[targetPublicKey] = domain.Config{PublicKey: targetPublicKey, AllowedIps: []string{"10.0.100.1/32"}}

	repoDeleteCalled := false
	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		assert.Equal(t, targetPublicKey, key)
		_, exists := mockRepo.configs[key]
		if !exists {
			return repository.ErrPeerNotFound
		}
		delete(mockRepo.configs, key)
		return nil
	}

	err := svc.Delete(targetPublicKey)
	require.NoError(t, err)
	assert.True(t, repoDeleteCalled)
	_, getErr := mockRepo.GetConfig(targetPublicKey)
	assert.ErrorIs(t, getErr, repository.ErrPeerNotFound)
}

func TestDelete_RepoError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	targetPeerKey := "peerForDeleteRepoError"
	simulatedRepoError := fmt.Errorf("simulated generic repository error during deletion")

	repoDeleteCalled := false
	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		assert.Equal(t, targetPeerKey, key)
		return simulatedRepoError
	}

	err := svc.Delete(targetPeerKey)
	require.Error(t, err)
	assert.True(t, repoDeleteCalled)
	assert.Equal(t, simulatedRepoError, err)
}

func TestRotatePeerKey_PeerNotFound_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	nonExistentOldPublicKey := "nonExistentPeerForRotationService"
	repoGetCalled := false
	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		repoGetCalled = true
		assert.Equal(t, nonExistentOldPublicKey, key)
		return nil, repository.ErrPeerNotFound
	}
	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		t.Errorf("repo.CreateConfig should not be called if old peer is not found")
		return nil
	}
	mockRepo.DeleteFunc = func(publicKey string) error {
		t.Errorf("repo.DeleteConfig should not be called if old peer is not found")
		return nil
	}

	rotatedCfg, err := svc.RotatePeerKey(nonExistentOldPublicKey)
	require.Error(t, err)
	assert.Nil(t, rotatedCfg)
	assert.True(t, repoGetCalled)
	assert.ErrorIs(t, err, repository.ErrPeerNotFound)
	assert.Contains(t, err.Error(), fmt.Sprintf("cannot rotate key for peer %s", nonExistentOldPublicKey))
}

func TestRotatePeerKey_CreateNewPeerError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	oldPublicKey := "oldPeerKeyForCreateError"
	oldPeerConfig := domain.Config{PublicKey: oldPublicKey, AllowedIps: []string{"10.0.0.1/32"}, PreSharedKey: "psk1", PersistentKeepalive: 25}
	mockRepo.configs[oldPublicKey] = oldPeerConfig

	simulatedCreateErrorMessage := "repository failed to create new peer config"
	repoGetCalled, repoCreateCalled, repoDeleteCalled := false, false, false

	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		repoGetCalled = true
		assert.Equal(t, oldPublicKey, key)
		cfgCopy := oldPeerConfig
		return &cfgCopy, nil
	}
	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		repoCreateCalled = true
		assert.Equal(t, oldPeerConfig.AllowedIps, cfg.AllowedIps)
		return errors.New(simulatedCreateErrorMessage)
	}
	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		t.Errorf("repo.DeleteConfig for old key %s should not be called", key)
		return nil
	}

	rotatedCfg, err := svc.RotatePeerKey(oldPublicKey)
	require.Error(t, err)
	assert.Nil(t, rotatedCfg)
	assert.True(t, repoGetCalled)
	assert.True(t, repoCreateCalled)
	assert.False(t, repoDeleteCalled)
	assert.Contains(t, err.Error(), simulatedCreateErrorMessage)
	assert.Contains(t, err.Error(), fmt.Sprintf("failed to apply new configuration for rotated peer %s", oldPublicKey))
	_, getErr := mockRepo.GetConfig(oldPublicKey)
	assert.NoError(t, getErr)
}

func TestRotatePeerKey_DeleteOldPeerError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo, 0) // MTU irrelevant

	oldPublicKey := "oldPeerKeyForDeleteError"
	oldPeerConfig := domain.Config{PublicKey: oldPublicKey, AllowedIps: []string{"10.0.0.2/32"}, PreSharedKey: "psk2", PersistentKeepalive: 22}
	mockRepo.configs[oldPublicKey] = oldPeerConfig

	simulatedDeleteErrorMessage := "repository failed to delete old peer config"
	var generatedNewPublicKey string
	repoGetCalled, repoCreateCalled, repoDeleteCalled := false, false, false

	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		// This mock needs to handle Get(oldPublicKey) initially by service,
		// and then Get(oldPublicKey) and Get(newPublicKey) by asserts at the end.
		cfg, exists := mockRepo.configs[key]
		if !exists {
			// If it's the *initial* call for oldPublicKey and it's not found, that's an error handled by other tests.
			// Here we assume it's found initially. If an assert calls for a non-existent key, return ErrPeerNotFound.
			if key == oldPublicKey && !repoGetCalled { // First call for old key must succeed for this test path
				t.Fatalf("Initial GetConfig for old key %s failed in DeleteOldPeerError test setup", oldPublicKey)
			}
			return nil, repository.ErrPeerNotFound
		}
		repoGetCalled = true // Mark that GetConfig was called at least once (for the initial fetch by service)
		cfgCopy := cfg
		return &cfgCopy, nil
	}
	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		repoCreateCalled = true
		generatedNewPublicKey = cfg.PublicKey
		assert.Equal(t, oldPeerConfig.AllowedIps, cfg.AllowedIps)
		mockRepo.configs[cfg.PublicKey] = cfg // Simulate successful creation
		return nil
	}
	mockRepo.DeleteFunc = func(receivedKey string) error {
		repoDeleteCalled = true
		assert.Equal(t, oldPublicKey, receivedKey)
		// Do NOT delete from mockRepo.configs[oldPublicKey] here to simulate the error
		return errors.New(simulatedDeleteErrorMessage)
	}

	rotatedCfg, err := svc.RotatePeerKey(oldPublicKey)
	require.Error(t, err)
	require.NotNil(t, rotatedCfg) // New config IS returned

	assert.True(t, repoGetCalled, "GetConfigFunc was not called as expected")
	assert.True(t, repoCreateCalled, "CreateConfigFunc was not called")
	assert.True(t, repoDeleteCalled, "DeleteFunc was not called")

	assert.Equal(t, generatedNewPublicKey, rotatedCfg.PublicKey)
	assert.Contains(t, err.Error(), simulatedDeleteErrorMessage)
	assert.Contains(t, err.Error(), "failed to delete old peer")
	assert.Contains(t, err.Error(), "new peer configuration is still valid and returned")

	_, getOldErr := mockRepo.GetConfig(oldPublicKey) // Should still exist
	assert.NoError(t, getOldErr, "Old peer should still exist in repo if its deletion failed")
	_, getNewErr := mockRepo.GetConfig(generatedNewPublicKey) // New peer should exist
	assert.NoError(t, getNewErr, "New peer should exist in repo")
}
