// internal/service/config_test.go
package service

import (
	"errors"
	"fmt"
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
func setupTestService(t *testing.T, repo repository.Repo) *ConfigService {
	t.Helper()
	logger.Logger = zaptest.NewLogger(t) // Initialize logger for tests

	// Define test configuration values that would normally come from Viper/Config struct
	testServerInterfacePublicKey := "testServiceServerPubKey"
	testServerExternalEndpoint := "test-service.example.com:12345"
	testClientKeyGenCmdTimeout := 3 * time.Second // Shorter for tests if needed, or use DefaultKeyGenTimeoutService
	testDnsServersForClient := "1.1.1.1"

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
		fmt.Sprintf("DNS = %s", svc.clientConfigDNSServers), // Check DNS
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

// TestGet_NotFound tests fetching a non-existent peer from the service.
func TestGet_NotFound(t *testing.T) {
	mockRepo := newFakeRepository() // Создаем чистый мок
	svc := setupTestService(t, mockRepo)

	nonExistentKey := "someNonExistentKey"

	// Настраиваем мок репозитория так, чтобы он вернул ошибку ErrPeerNotFound
	mockRepo.GetConfigError = repository.ErrPeerNotFound
	// Или, если бы мы хотели быть более точными для конкретного ключа:
	// mockRepo.GetFunc = func(key string) (*domain.Config, error) {
	// 	if key == nonExistentKey {
	// 		return nil, repository.ErrPeerNotFound
	// 	}
	// 	return nil, fmt.Errorf("unexpected key in mock repo: %s", key)
	// }

	config, err := svc.Get(nonExistentKey)

	require.Error(t, err, "Expected an error when getting a non-existent peer")
	assert.Nil(t, config, "Expected config to be nil on error")
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Expected ErrPeerNotFound")
}

// TestCreateWithNewKeys_RepoError tests peer creation when the repository fails to save the config.
func TestCreateWithNewKeys_RepoError(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo) // clientKeyGenTimeout настроен в setupTestService

	allowedIPs := []string{"10.20.0.5/32"}
	psk := "newServicePeerPSKErrorCase"
	keepalive := 30

	// Имитируем ошибку от репозитория при попытке создания
	simulatedRepoErrorMessage := "repository failed to create config"
	mockRepo.CreateConfigError = errors.New(simulatedRepoErrorMessage)
	// Используем fmt.Errorf для создания новой ошибки, чтобы быть уверенным,
	// что мы проверяем именно ее, а не какое-то глобальное значение.
	// Если бы это была стандартная ошибка, можно было бы использовать errors.New.

	createdCfg, err := svc.CreateWithNewKeys(allowedIPs, psk, keepalive)

	require.Error(t, err, "Expected an error when repository fails to create config")
	assert.Nil(t, createdCfg, "Returned config should be nil on repository error")

	// Проверяем, что ошибка содержит наше симулированное сообщение от репозитория.
	// Метод CreateConfig в сервисе оборачивает ошибку репозитория, поэтому используем Contains.
	assert.Contains(t, err.Error(), simulatedRepoErrorMessage, "Error message should contain the repository's error")
	// Более точная проверка, если известно, как сервис оборачивает ошибку:
	// Например, если сервис делает: fmt.Errorf("failed to add new peer %s to WireGuard: %w", newPubKey, err)
	// то можно было бы проверить на "failed to add new peer" и "repository failed to create config"
}

// TestUpdateAllowedIPs_Success_Service tests successful update of allowed IPs via the service.
func TestUpdateAllowedIPs_Success_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	targetPublicKey := "peerToUpdateIPsInService"
	newIPs := []string{"192.168.200.1/32", "fd00::100/128"}

	// Предварительно добавим пира в мок-репозиторий, чтобы было что обновлять
	// (хотя UpdateAllowedIPs в WGRepository может и не проверять существование пира явно,
	// но для чистоты теста лучше, чтобы он был)
	mockRepo.configs[targetPublicKey] = domain.Config{
		PublicKey:  targetPublicKey,
		AllowedIps: []string{"10.0.0.1/32"}, // Старые IP
	}

	// Флаг, чтобы убедиться, что метод репозитория был вызван
	repoUpdateCalled := false
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error { // Присваиваем полю-функции
		repoUpdateCalled = true
		assert.Equal(t, targetPublicKey, key, "PublicKey passed to repo mismatch")
		assert.Equal(t, newIPs, ips, "IPs passed to repo mismatch")

		// Обновляем данные в нашем моке, чтобы соответствовать ожидаемому поведению
		cfg, ok := mockRepo.configs[key]
		if !ok {
			return repository.ErrPeerNotFound
		}
		cfg.AllowedIps = ips
		mockRepo.configs[key] = cfg
		return nil
	}
	// defer уже не нужен, так как мы меняем поле мока, созданного для этого теста

	err := svc.UpdateAllowedIPs(targetPublicKey, newIPs)

	require.NoError(t, err, "UpdateAllowedIPs should not return an error on success")
	assert.True(t, repoUpdateCalled, "Repository's UpdateAllowedIPs method was not called")

	// Дополнительно можно проверить, что данные в мок-репозитории действительно обновились
	updatedPeerConfig, _ := mockRepo.GetConfig(targetPublicKey) // Ошибку здесь не ожидаем
	require.NotNil(t, updatedPeerConfig, "Peer config should exist in mock repo")
	assert.Equal(t, newIPs, updatedPeerConfig.AllowedIps, "AllowedIPs in mock repo were not updated")
}

// TestUpdateAllowedIPs_NotFound_Service tests updating IPs for a non-existent peer at the service level.
func TestUpdateAllowedIPs_NotFound_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	nonExistentPeerKey := "nonExistentPeerForUpdateService"
	newIPs := []string{"172.16.10.1/32"}

	repoUpdateCalled := false
	// Настраиваем мок-репозиторий так, чтобы он вернул ErrPeerNotFound
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error {
		repoUpdateCalled = true
		assert.Equal(t, nonExistentPeerKey, key)
		return repository.ErrPeerNotFound // Имитируем ошибку от репозитория
	}

	err := svc.UpdateAllowedIPs(nonExistentPeerKey, newIPs)

	require.Error(t, err, "Expected an error when updating IPs for a non-existent peer")
	assert.True(t, repoUpdateCalled, "Repository's UpdateAllowedIPsFunc was not called")
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Expected ErrPeerNotFound")
}

// TestUpdateAllowedIPs_RepoError_Service tests updating IPs when the repository returns a generic error.
func TestUpdateAllowedIPs_RepoError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	targetPeerKey := "peerForUpdateRepoError"
	newIPs := []string{"172.16.20.1/32"}
	simulatedRepoError := fmt.Errorf("simulated generic repository error during IP update")

	// Предварительно добавим пира, чтобы ошибка была не "не найдено", а именно ошибка обновления
	mockRepo.configs[targetPeerKey] = domain.Config{PublicKey: targetPeerKey}

	repoUpdateCalled := false
	mockRepo.UpdateAllowedIPsFunc = func(key string, ips []string) error {
		repoUpdateCalled = true
		assert.Equal(t, targetPeerKey, key)
		return simulatedRepoError // Имитируем другую ошибку от репозитория
	}

	err := svc.UpdateAllowedIPs(targetPeerKey, newIPs)

	require.Error(t, err, "Expected an error when repository fails to update IPs")
	assert.True(t, repoUpdateCalled, "Repository's UpdateAllowedIPsFunc was not called")

	// Проверяем, что возвращенная ошибка является той самой, которую мы симулировали.
	// Поскольку сервис UpdateAllowedIPs просто возвращает ошибку от репозитория без обертывания,
	// мы можем использовать assert.Equal для самой ошибки или assert.ErrorIs.
	assert.Equal(t, simulatedRepoError, err, "The returned error should be the one from the repository")
	// или assert.ErrorIs(t, err, simulatedRepoError) - тоже хорошо
}

// TestDelete_Success_Service tests successful deletion of a peer via the service.
func TestDelete_Success_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	targetPublicKey := "peerToDeleteInService"

	// Предварительно добавим пира в мок-репозиторий, чтобы было что удалять
	mockRepo.configs[targetPublicKey] = domain.Config{
		PublicKey:  targetPublicKey,
		AllowedIps: []string{"10.0.100.1/32"},
	}

	repoDeleteCalled := false

	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		assert.Equal(t, targetPublicKey, key, "PublicKey passed to repo for deletion mismatch")

		// Имитируем удаление в моке
		_, exists := mockRepo.configs[key]
		if !exists {
			return repository.ErrPeerNotFound // Хотя для успешного теста это не должно произойти
		}
		delete(mockRepo.configs, key)
		return nil
	}

	err := svc.Delete(targetPublicKey)

	require.NoError(t, err, "Delete should not return an error on success")
	assert.True(t, repoDeleteCalled, "Repository's DeleteFunc method was not called")

	// Дополнительно проверяем, что пир действительно удален из мок-репозитория
	_, getErr := mockRepo.GetConfig(targetPublicKey) // Используем GetConfig для проверки
	assert.ErrorIs(t, getErr, repository.ErrPeerNotFound, "Peer should be deleted from mock repo")
}

// TestDelete_RepoError_Service tests deleting a peer when the repository returns an error.
func TestDelete_RepoError_Service(t *testing.T) {
	mockRepo := newFakeRepository() // Убедись, что newFakeRepository() инициализирует DeleteFunc как nil
	svc := setupTestService(t, mockRepo)

	targetPeerKey := "peerForDeleteRepoError"
	simulatedRepoError := fmt.Errorf("simulated generic repository error during deletion")

	// Можно предварительно добавить пира, чтобы показать, что он был, но удаление не удалось.
	// Но для этого теста это не строго обязательно, т.к. ошибка имитируется напрямую.
	// mockRepo.configs[targetPeerKey] = domain.Config{PublicKey: targetPeerKey}

	repoDeleteCalled := false
	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		assert.Equal(t, targetPeerKey, key)
		return simulatedRepoError // Имитируем ошибку от репозитория
	}

	err := svc.Delete(targetPeerKey)

	require.Error(t, err, "Expected an error when repository fails to delete peer")
	assert.True(t, repoDeleteCalled, "Repository's DeleteFunc was not called")

	// Проверяем, что возвращенная ошибка является той самой, которую мы симулировали.
	// Метод Delete в ConfigService просто возвращает ошибку от репозитория.
	assert.Equal(t, simulatedRepoError, err, "The returned error should be the one from the repository")
	// или assert.ErrorIs(t, err, simulatedRepoError)
}

// TestRotatePeerKey_PeerNotFound_Service tests key rotation when the original peer is not found.
func TestRotatePeerKey_PeerNotFound_Service(t *testing.T) {
	mockRepo := newFakeRepository() // Убедись, что newFakeRepository() инициализирует GetConfigFunc как nil
	svc := setupTestService(t, mockRepo)

	nonExistentOldPublicKey := "nonExistentPeerForRotationService"

	repoGetCalled := false
	// Настраиваем GetConfigFunc, чтобы он вернул ErrPeerNotFound
	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		repoGetCalled = true
		assert.Equal(t, nonExistentOldPublicKey, key)
		return nil, repository.ErrPeerNotFound // Имитируем, что старый пир не найден
	}

	// Эти функции не должны быть вызваны, если GetConfig завершился ошибкой
	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		t.Errorf("repo.CreateConfig should not be called if old peer is not found")
		return nil
	}
	mockRepo.DeleteFunc = func(publicKey string) error {
		t.Errorf("repo.DeleteConfig should not be called if old peer is not found")
		return nil
	}

	rotatedCfg, err := svc.RotatePeerKey(nonExistentOldPublicKey)

	require.Error(t, err, "Expected an error when trying to rotate keys for a non-existent peer")
	assert.Nil(t, rotatedCfg, "Returned config should be nil on error")
	assert.True(t, repoGetCalled, "Repository's GetConfigFunc was not called")

	// Проверяем, что сервис вернул или обернул ошибку ErrPeerNotFound
	// В ConfigService.RotatePeerKey: return nil, fmt.Errorf("cannot rotate key for peer %s: %w", oldPublicKey, repository.ErrPeerNotFound)
	// или return nil, fmt.Errorf("failed to retrieve config for peer %s before rotation: %w", oldPublicKey, err)
	// Поэтому нужно проверять через errors.Is
	assert.ErrorIs(t, err, repository.ErrPeerNotFound, "Expected error to be or wrap ErrPeerNotFound")
	assert.Contains(t, err.Error(), fmt.Sprintf("cannot rotate key for peer %s", nonExistentOldPublicKey), "Error message is not as expected")
}

// TestRotatePeerKey_CreateNewPeerError_Service tests key rotation when creating the new peer config fails.
func TestRotatePeerKey_CreateNewPeerError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo) // clientKeyGenTimeout для generateKeyPair используется из svc

	oldPublicKey := "oldPeerKeyForCreateError"
	oldPeerConfig := domain.Config{
		PublicKey:           oldPublicKey,
		AllowedIps:          []string{"10.0.0.1/32"},
		PreSharedKey:        "psk1",
		PersistentKeepalive: 25,
	}
	// Добавляем старого пира в репозиторий
	mockRepo.configs[oldPublicKey] = oldPeerConfig

	simulatedCreateErrorMessage := "repository failed to create new peer config"

	repoGetCalled := false
	repoCreateCalled := false
	repoDeleteCalled := false // Убедимся, что удаление старого не вызывалось

	// repo.GetConfig для старого ключа должен быть успешным
	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		repoGetCalled = true
		assert.Equal(t, oldPublicKey, key)
		// Возвращаем копию, чтобы избежать модификации оригинального oldPeerConfig в мапе мока до его "удаления"
		cfgCopy := oldPeerConfig
		return &cfgCopy, nil
	}

	// repo.CreateConfig для нового ключа должен вернуть ошибку
	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		repoCreateCalled = true
		// Мы не знаем точно новый публичный ключ, который будет сгенерирован,
		// но можем проверить, что AllowedIPs, PSK, Keepalive переданы правильно.
		assert.Equal(t, oldPeerConfig.AllowedIps, cfg.AllowedIps)
		assert.Equal(t, oldPeerConfig.PreSharedKey, cfg.PreSharedKey)
		assert.Equal(t, oldPeerConfig.PersistentKeepalive, cfg.PersistentKeepalive)
		return errors.New(simulatedCreateErrorMessage)
	}

	// repo.DeleteConfig не должен вызываться
	mockRepo.DeleteFunc = func(key string) error {
		repoDeleteCalled = true
		t.Errorf("repo.DeleteConfig for old key %s should not be called when CreateConfig for new key fails", key)
		return nil
	}

	rotatedCfg, err := svc.RotatePeerKey(oldPublicKey)

	require.Error(t, err, "Expected an error when creating new peer config fails during rotation")
	assert.Nil(t, rotatedCfg, "Returned config should be nil on error")

	assert.True(t, repoGetCalled, "Repository's GetConfigFunc was not called")
	assert.True(t, repoCreateCalled, "Repository's CreateConfigFunc was not called")
	assert.False(t, repoDeleteCalled, "Repository's DeleteFunc (for old key) should not have been called")

	// Проверяем, что ошибка содержит сообщение от CreateConfig
	assert.Contains(t, err.Error(), simulatedCreateErrorMessage, "Error message mismatch")
	// Также проверяем, что сервис обернул ошибку с указанием на проблему
	// Ожидаемый формат из сервиса: "failed to apply new configuration for rotated peer %s (new key %s): %w"
	assert.Contains(t, err.Error(), fmt.Sprintf("failed to apply new configuration for rotated peer %s", oldPublicKey))

	// Убедимся, что старый пир все еще существует в репозитории
	_, getErr := mockRepo.GetConfig(oldPublicKey)
	assert.NoError(t, getErr, "Old peer should still exist in repository if new peer creation failed")
}

// TestRotatePeerKey_DeleteOldPeerError_Service tests key rotation when deleting the old peer config fails.
func TestRotatePeerKey_DeleteOldPeerError_Service(t *testing.T) {
	mockRepo := newFakeRepository()
	svc := setupTestService(t, mockRepo)

	oldPublicKey := "oldPeerKeyForDeleteError"
	oldPeerConfig := domain.Config{
		PublicKey:           oldPublicKey,
		AllowedIps:          []string{"10.0.0.2/32"},
		PreSharedKey:        "psk2",
		PersistentKeepalive: 22,
	}
	mockRepo.configs[oldPublicKey] = oldPeerConfig // Добавляем старого пира

	simulatedDeleteErrorMessage := "repository failed to delete old peer config"

	var generatedNewPublicKey string // Сохраним сгенерированный ключ для проверок

	repoGetCalled := false
	repoCreateCalled := false
	repoDeleteCalled := false

	mockRepo.GetConfigFunc = func(key string) (*domain.Config, error) {
		repoGetCalled = true // Этот флаг теперь будет true после первого вызова GetConfig сервисом

		// Сервис вызывает GetConfig(oldPublicKey) один раз в начале.
		// Проверки в конце теста также вызывают GetConfig.
		// Нам нужно, чтобы эта функция-мок корректно обрабатывала ВСЕ эти вызовы.

		if key == oldPublicKey { // Если запрашивают старый ключ
			// Если это первый вызов от сервиса, мы можем вернуть копию.
			// Если это вызов из assert'а в конце теста, мы должны вернуть то, что реально в configs.
			cfg, exists := mockRepo.configs[key]
			if !exists {
				return nil, repository.ErrPeerNotFound
			}
			cfgCopy := cfg // Всегда возвращаем копию, чтобы избежать модификации через указатель
			return &cfgCopy, nil
		}
		if key == generatedNewPublicKey { // Если запрашивают новый ключ (из assert'ов в конце)
			cfg, exists := mockRepo.configs[key]
			if !exists {
				return nil, repository.ErrPeerNotFound
			}
			cfgCopy := cfg
			return &cfgCopy, nil
		}
		// Если какой-то другой ключ, которого мы не ожидаем
		t.Errorf("mockRepo.GetConfigFunc called with unexpected key: %s", key)
		return nil, fmt.Errorf("unexpected key in GetConfigFunc: %s", key)
	}

	mockRepo.CreateConfigFunc = func(cfg domain.Config) error {
		repoCreateCalled = true
		assert.NotEmpty(t, cfg.PublicKey)
		generatedNewPublicKey = cfg.PublicKey // Сохраняем для последующей проверки
		// Проверяем, что атрибуты унаследованы
		assert.Equal(t, oldPeerConfig.AllowedIps, cfg.AllowedIps)
		assert.Equal(t, oldPeerConfig.PreSharedKey, cfg.PreSharedKey)
		assert.Equal(t, oldPeerConfig.PersistentKeepalive, cfg.PersistentKeepalive)
		// Имитируем успешное создание нового пира в моке
		mockRepo.configs[cfg.PublicKey] = cfg
		return nil
	}

	mockRepo.DeleteFunc = func(receivedKey string) error {
		repoDeleteCalled = true
		t.Logf("mockRepo.DeleteFunc: captured oldPublicKey = '%s', receivedKey from service = '%s'", oldPublicKey, receivedKey)
		assert.Equal(t, oldPublicKey, receivedKey, "PublicKey passed to repo.DeleteConfig in mock should be oldPublicKey")
		return errors.New(simulatedDeleteErrorMessage)
	}

	rotatedCfg, err := svc.RotatePeerKey(oldPublicKey)

	// Ожидаем И ошибку, И конфигурацию нового пира
	require.Error(t, err, "Expected an error when deleting old peer config fails")
	require.NotNil(t, rotatedCfg, "Returned new config should NOT be nil even if old peer deletion failed")

	assert.True(t, repoGetCalled, "Repository's GetConfigFunc was not called")
	assert.True(t, repoCreateCalled, "Repository's CreateConfigFunc was not called")
	assert.True(t, repoDeleteCalled, "Repository's DeleteFunc was not called")

	// Проверяем свойства возвращенной конфигурации (нового пира)
	require.NotEmpty(t, rotatedCfg.PublicKey, "Rotated config should have a PublicKey")
	assert.NotEqual(t, oldPublicKey, rotatedCfg.PublicKey, "New PublicKey should be different from old")
	require.NotEmpty(t, rotatedCfg.PrivateKey, "Rotated config should include the new PrivateKey")
	assert.Equal(t, oldPeerConfig.AllowedIps, rotatedCfg.AllowedIps)
	assert.Equal(t, oldPeerConfig.PreSharedKey, rotatedCfg.PreSharedKey)
	assert.Equal(t, oldPeerConfig.PersistentKeepalive, rotatedCfg.PersistentKeepalive)

	// Проверяем текст ошибки
	assert.Contains(t, err.Error(), simulatedDeleteErrorMessage, "Error message should contain the repository's delete error")
	assert.Contains(t, err.Error(), "failed to delete old peer", "Error message context is not as expected")
	assert.Contains(t, err.Error(), "new peer configuration is still valid and returned", "Error message context is not as expected")

	// Проверяем состояние репозитория:
	// 1. Старый пир все еще должен существовать (т.к. его удаление не удалось)
	_, getOldErr := mockRepo.GetConfig(oldPublicKey)
	assert.NoError(t, getOldErr, "Old peer should still exist in repository if its deletion failed")

	// 2. Новый пир должен существовать
	_, getNewErr := mockRepo.GetConfig(generatedNewPublicKey)
	assert.NoError(t, getNewErr, "New peer should exist in repository")
	assert.Equal(t, generatedNewPublicKey, rotatedCfg.PublicKey, "Returned PublicKey should match the one created in repo")
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
