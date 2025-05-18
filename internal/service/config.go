package service

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

const (
	// DefaultKeyGenTimeout используется, если не задан таймаут для генерации КЛИЕНТСКИХ ключей.
	// Таймаут для генерации серверного публичного ключа (в ServerKeyManager) задается отдельно.
	DefaultKeyGenTimeout = 5 * time.Second
)

// ConfigService инкапсулирует бизнес-логику для управления конфигурациями пиров WireGuard.
type ConfigService struct {
	repo                repository.Repo
	serverBasePublicKey string        // Публичный ключ ИНТЕРФЕЙСА сервера (полученный из ServerKeyManager)
	serverBaseEndpoint  string        // Внешний эндпоинт сервера (host:port) для клиентских конфигов
	clientKeyGenTimeout time.Duration // Таймаут для команд генерации КЛИЕНТСКИХ ключей ('wg genkey')
}

// NewConfigService создает новый экземпляр ConfigService.
func NewConfigService(
	repo repository.Repo,
	serverPublicKey string, // Публичный ключ интерфейса этого сервера
	serverEndpoint string, // Публичный эндпоинт этого сервера (для клиентов)
	clientKeyGenTimeout time.Duration, // Таймаут для генерации ключей клиентов
) *ConfigService {
	if repo == nil {
		logger.Logger.Fatal("Repository cannot be nil for ConfigService")
	}
	if serverPublicKey == "" {
		// Это должно быть поймано ServerKeyManager, но проверка не помешает
		logger.Logger.Fatal("Server public key is empty in ConfigService initialization.")
	}

	if clientKeyGenTimeout <= 0 {
		logger.Logger.Warn("Provided client key generation timeout is invalid, using default",
			zap.Duration("providedTimeout", clientKeyGenTimeout),
			zap.Duration("defaultTimeout", DefaultKeyGenTimeout))
		clientKeyGenTimeout = DefaultKeyGenTimeout
	}

	s := &ConfigService{
		repo:                repo,
		serverBasePublicKey: serverPublicKey, // Сохраняем публичный ключ сервера
		serverBaseEndpoint:  serverEndpoint,
		clientKeyGenTimeout: clientKeyGenTimeout,
	}

	logger.Logger.Info("ConfigService initialized",
		zap.String("serverPublicKeyFirstChars", s.serverBasePublicKey[:min(10, len(s.serverBasePublicKey))]+"..."),
		zap.Bool("serverEndpointSet", s.serverBaseEndpoint != ""),
		zap.Duration("clientKeyGenTimeout", s.clientKeyGenTimeout),
	)
	return s
}

// min - вспомогательная функция.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetAll получает все конфигурации пиров.
func (s *ConfigService) GetAll() ([]domain.Config, error) {
	configs, err := s.repo.ListConfigs()
	if err != nil {
		logger.Logger.Error("Service: Failed to get all configs from repository", zap.Error(err))
		return nil, err
	}
	logger.Logger.Debug("Service: Successfully retrieved all configs", zap.Int("count", len(configs)))
	return configs, nil
}

// Get получает конфигурацию одного пира по его публичному ключу.
func (s *ConfigService) Get(publicKey string) (*domain.Config, error) {
	if publicKey == "" {
		logger.Logger.Warn("Service: Get config called with empty public key")
		return nil, errors.New("public key cannot be empty for Get operation")
	}
	config, err := s.repo.GetConfig(publicKey)
	if err != nil {
		if errors.Is(err, repository.ErrPeerNotFound) {
			logger.Logger.Info("Service: Peer not found in repository", zap.String("publicKey", publicKey))
		} else {
			logger.Logger.Error("Service: Failed to get config by public key from repository", zap.String("publicKey", publicKey), zap.Error(err))
		}
		return nil, err
	}
	logger.Logger.Debug("Service: Successfully retrieved config by public key", zap.String("publicKey", publicKey))
	return config, nil
}

// CreateWithNewKeys генерирует новую пару ключей, создает пира и возвращает его конфигурацию, включая приватный ключ.
func (s *ConfigService) CreateWithNewKeys(allowedIPs []string, presharedKey string, persistentKeepalive int) (*domain.Config, error) {
	if len(allowedIPs) == 0 {
		logger.Logger.Info("Service: Creating new peer with empty AllowedIPs. This might be acceptable depending on WG configuration.")
	}

	newPrivKey, newPubKey, err := s.generateKeyPair() // Использует s.clientKeyGenTimeout
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair for new peer: %w", err)
	}

	newPeerCfg := domain.Config{
		PublicKey:           newPubKey,
		PrivateKey:          newPrivKey, // Важно вернуть клиенту!
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}

	// Передаем в репозиторий только ту часть domain.Config, которую он ожидает для 'wg set'
	// (т.е. без PrivateKey клиента)
	repoPeerCfg := domain.Config{
		PublicKey:           newPubKey,
		AllowedIps:          allowedIPs,
		PreSharedKey:        presharedKey,
		PersistentKeepalive: persistentKeepalive,
	}
	if err := s.repo.CreateConfig(repoPeerCfg); err != nil {
		return nil, fmt.Errorf("failed to add new peer %s to WireGuard: %w", newPubKey, err)
	}

	logger.Logger.Info("Service: Successfully created new peer with generated keys.",
		zap.String("newPublicKey", newPeerCfg.PublicKey))
	// Возвращаем newPeerCfg, который содержит и PrivateKey для клиента
	return &newPeerCfg, nil
}

// UpdateAllowedIPs обновляет разрешенные IP для существующего пира.
func (s *ConfigService) UpdateAllowedIPs(publicKey string, ips []string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: UpdateAllowedIPs called with empty public key")
		return errors.New("public key is required for updating allowed IPs")
	}
	err := s.repo.UpdateAllowedIPs(publicKey, ips)
	if err != nil {
		logger.Logger.Error("Service: Failed to update allowed IPs in repository",
			zap.String("publicKey", publicKey),
			zap.Strings("newIPs", ips),
			zap.Error(err))
		return err
	}
	logger.Logger.Info("Service: Successfully updated allowed IPs", zap.String("publicKey", publicKey), zap.Strings("newIPs", ips))
	return nil
}

// Delete удаляет пира.
func (s *ConfigService) Delete(publicKey string) error {
	if publicKey == "" {
		logger.Logger.Warn("Service: Delete config called with empty public key")
		return errors.New("public key is required for deleting a peer")
	}
	err := s.repo.DeleteConfig(publicKey)
	if err != nil {
		logger.Logger.Error("Service: Failed to delete config in repository", zap.String("publicKey", publicKey), zap.Error(err))
		return err
	}
	logger.Logger.Info("Service: Successfully deleted config", zap.String("publicKey", publicKey))
	return nil
}

// BuildClientConfig генерирует содержимое .conf файла для клиента.
// peerCfg: Конфигурация пира с сервера (обычно из 'wg show dump').
// clientPrivateKey: Приватный ключ клиента, предоставленный внешним приложением.
func (s *ConfigService) BuildClientConfig(peerCfg *domain.Config, clientPrivateKey string) (string, error) {
	if peerCfg == nil {
		return "", errors.New("peer configuration cannot be nil for BuildClientConfig")
	}
	if clientPrivateKey == "" {
		logger.Logger.Warn("Service: BuildClientConfig called with empty clientPrivateKey", zap.String("peerPublicKey", peerCfg.PublicKey))
		return "", errors.New("client private key cannot be empty for building .conf file")
	}
	if peerCfg.PublicKey == "" { // Это публичный ключ клиента, должен быть в peerCfg
		return "", errors.New("peer public key is missing from peerCfg, cannot build client config")
	}

	var b strings.Builder

	// [Interface] секция клиента
	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", clientPrivateKey))
	if len(peerCfg.AllowedIps) > 0 {
		// Первый IP из AllowedIPs сервера для этого пира обычно используется как адрес клиента в его интерфейсе.
		// Убедись, что он содержит и маску, например "10.0.0.2/32".
		clientAddress := peerCfg.AllowedIps[0]
		b.WriteString(fmt.Sprintf("Address = %s\n", clientAddress))
	} else {
		logger.Logger.Info("Service: Building client config for peer with no server-side AllowedIPs. Client Address field will be omitted.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}
	// Можно добавить DNS:
	// b.WriteString("DNS = 1.1.1.1, 1.0.0.1\n")
	b.WriteString("\n")

	// [Peer] секция клиента (описывает сервер)
	b.WriteString("[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", s.serverBasePublicKey)) // Публичный ключ интерфейса сервера

	if s.serverBaseEndpoint != "" {
		b.WriteString(fmt.Sprintf("Endpoint = %s\n", s.serverBaseEndpoint))
	} else {
		logger.Logger.Warn("Service: Server endpoint is not configured. Client .conf file will be missing the Endpoint field.",
			zap.String("peerPublicKey", peerCfg.PublicKey))
	}

	// PresharedKey берется из конфигурации пира на сервере (если он там есть)
	if peerCfg.PreSharedKey != "" {
		b.WriteString(fmt.Sprintf("PresharedKey = %s\n", peerCfg.PreSharedKey))
	}

	// AllowedIPs для секции [Peer] в клиентском конфиге.
	// Обычно это 0.0.0.0/0 для маршрутизации всего трафика через VPN.
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\n")

	if peerCfg.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", peerCfg.PersistentKeepalive))
	}

	logger.Logger.Info("Service: Successfully built client config content using provided client private key.",
		zap.String("peerPublicKey", peerCfg.PublicKey))
	return b.String(), nil
}

// generateKeyPair генерирует новую пару ключей (приватный/публичный).
func (s *ConfigService) generateKeyPair() (privKey, pubKey string, err error) {
	logger.Logger.Debug("Service: Generating new key pair for a client", zap.Duration("timeout", s.clientKeyGenTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), s.clientKeyGenTimeout)
	defer cancel()

	genCmd := exec.CommandContext(ctx, "wg", "genkey")
	privKeyBytes, err := genCmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg genkey' for client key.")
		return "", "", fmt.Errorf("wg genkey timed out: %w", repository.ErrWgTimeout)
	}
	if err != nil {
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg genkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg genkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate client private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg)
	}
	privKey = strings.TrimSpace(string(privKeyBytes))
	if privKey == "" {
		logger.Logger.Error("Service: 'wg genkey' produced an empty private key for client.")
		return "", "", errors.New("wg genkey produced empty private key")
	}

	pubCmd := exec.CommandContext(ctx, "wg", "pubkey")
	pubCmd.Stdin = strings.NewReader(privKey)
	pubKeyBytes, err := pubCmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		logger.Logger.Error("Service: Timeout during 'wg pubkey' for client key.")
		return "", "", fmt.Errorf("wg pubkey timed out: %w", repository.ErrWgTimeout)
	}
	if err != nil {
		var exitError *exec.ExitError
		errMsg := fmt.Sprintf("wg pubkey command failed: %s", err.Error())
		if errors.As(err, &exitError) {
			errMsg = fmt.Sprintf("wg pubkey command failed with exit code %d: %s. Stderr: %s", exitError.ExitCode(), err.Error(), string(exitError.Stderr))
		}
		logger.Logger.Error("Service: Failed to generate client public key from private key", zap.String("details", errMsg), zap.Error(err))
		return "", "", fmt.Errorf(errMsg)
	}
	pubKey = strings.TrimSpace(string(pubKeyBytes))
	if pubKey == "" {
		logger.Logger.Error("Service: 'wg pubkey' produced an empty public key for client.")
		return "", "", errors.New("wg pubkey produced empty public key")
	}

	logger.Logger.Info("Service: Successfully generated new client key pair.")
	return privKey, pubKey, nil
}

// RotatePeerKey ротирует ключи для существующего пира.
func (s *ConfigService) RotatePeerKey(oldPublicKey string) (*domain.Config, error) {
	if oldPublicKey == "" {
		logger.Logger.Warn("Service: RotatePeerKey called with empty old public key")
		return nil, errors.New("old public key cannot be empty for key rotation")
	}
	logger.Logger.Info("Service: Attempting to rotate peer key", zap.String("oldPublicKey", oldPublicKey))

	oldCfg, err := s.repo.GetConfig(oldPublicKey)
	if err != nil {
		logger.Logger.Error("Service (Rotate): Failed to get old peer config", zap.String("oldPublicKey", oldPublicKey), zap.Error(err))
		if errors.Is(err, repository.ErrPeerNotFound) {
			return nil, fmt.Errorf("cannot rotate key for peer %s: %w", oldPublicKey, repository.ErrPeerNotFound)
		}
		return nil, fmt.Errorf("failed to retrieve config for peer %s before rotation: %w", oldPublicKey, err)
	}

	newPrivKey, newPubKey, err := s.generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("key pair generation failed during rotation for %s: %w", oldPublicKey, err)
	}
	logger.Logger.Debug("Service (Rotate): Generated new key pair",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	newPeerDomainCfg := domain.Config{
		PublicKey:           newPubKey,
		PrivateKey:          newPrivKey, // Для ответа клиенту
		AllowedIps:          oldCfg.AllowedIps,
		PreSharedKey:        oldCfg.PreSharedKey, // Сохраняем старый PSK по умолчанию
		PersistentKeepalive: oldCfg.PersistentKeepalive,
	}

	repoPeerCfgForCreate := domain.Config{ // Конфиг для передачи в репозиторий (без приватного ключа)
		PublicKey:           newPubKey,
		AllowedIps:          oldCfg.AllowedIps,
		PreSharedKey:        oldCfg.PreSharedKey,
		PersistentKeepalive: oldCfg.PersistentKeepalive,
	}
	if err := s.repo.CreateConfig(repoPeerCfgForCreate); err != nil {
		logger.Logger.Error("Service (Rotate): Failed to create new peer config with rotated keys",
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		return nil, fmt.Errorf("failed to apply new configuration for rotated peer %s (new key %s): %w", oldPublicKey, newPubKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully applied new config for peer with new keys",
		zap.String("oldPublicKey", oldPublicKey),
		zap.String("newPublicKey", newPubKey))

	if err := s.repo.DeleteConfig(oldPublicKey); err != nil {
		logger.Logger.Error("CRITICAL (Rotate): New peer config applied, but FAILED TO DELETE OLD PEER CONFIG. Manual cleanup may be needed.",
			zap.String("oldPublicKey", oldPublicKey),
			zap.String("newPublicKey", newPubKey),
			zap.Error(err))
		return &newPeerDomainCfg, fmt.Errorf("new peer %s (rotated from %s) created, but failed to delete old peer: %w", newPubKey, oldPublicKey, err)
	}
	logger.Logger.Info("Service (Rotate): Successfully deleted old peer config", zap.String("oldPublicKey", oldPublicKey))

	return &newPeerDomainCfg, nil
}

// Метод Create, если бы мы хотели, чтобы клиент предоставлял свой PublicKey, а не генерировал на сервере.
// Пока он не используется, так как CreateWithNewKeys является основным.
/*
func (s *ConfigService) Create(cfg domain.Config) error {
	if cfg.PublicKey == "" {
		return errors.New("public key is required for creating a peer")
	}
	// Валидация cfg.PublicKey, cfg.AllowedIps и т.д.
	// Если используется cfg.PrivateKey для чего-то на этом этапе (маловероятно)

	// В репозиторий передается cfg без PrivateKey клиента, если он там не нужен
	err := s.repo.CreateConfig(cfg)
	if err != nil {
		logger.Logger.Error("Service: Failed to create config in repository", zap.String("publicKey", cfg.PublicKey), zap.Error(err))
		return err
	}
	logger.Logger.Info("Service: Successfully created config", zap.String("publicKey", cfg.PublicKey))
	return nil
}
*/
