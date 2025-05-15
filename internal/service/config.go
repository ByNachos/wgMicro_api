package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
)

// ConfigService отвечает за операции с WireGuard-конфигурациями.
type ConfigService struct {
	repo         repository.Repo // теперь интерфейс, а не *WGRepository
	ifacePubKey  string
	ifacePrivKey string
}

const keyGenTimeout = 5 * time.Second

// NewConfigService создаёт сервис, сразу загружая из .env ключи интерфейса.
// Принимает любую реализацию repository.Repo.
func NewConfigService(repo repository.Repo) *ConfigService {
	pub := os.Getenv("INTERFACE_PUBLIC_KEY")
	priv := os.Getenv("INTERFACE_PRIVATE_KEY")
	if pub == "" || priv == "" {
		logger.Logger.Fatal("interface keys must be set in environment")
	}
	logger.Logger.Info("Loaded interface keys from environment")
	return &ConfigService{
		repo:         repo,
		ifacePubKey:  pub,
		ifacePrivKey: priv,
	}
}

// GetAll возвращает все peer-конфигурации.
func (s *ConfigService) GetAll() ([]domain.Config, error) {
	return s.repo.ListConfigs()
}

// Get возвращает одну конфигурацию по publicKey.
func (s *ConfigService) Get(publicKey string) (*domain.Config, error) {
	return s.repo.GetConfig(publicKey)
}

// Create создаёт нового peer-а с заданными параметрами.
func (s *ConfigService) Create(cfg domain.Config) error {
	return s.repo.CreateConfig(cfg)
}

// UpdateAllowedIPs заменяет список allowed-ips у существующего peer-а.
func (s *ConfigService) UpdateAllowedIPs(publicKey string, ips []string) error {
	return s.repo.UpdateAllowedIPs(publicKey, ips)
}

// Delete удаляет peer-а из интерфейса.
func (s *ConfigService) Delete(publicKey string) error {
	return s.repo.DeleteConfig(publicKey)
}

// BuildClientConfig собирает текстовый .conf-файл для клиента-пира.
// Шаблон собирается из ключей интерфейса (из .env) и полей cfg.
func (s *ConfigService) BuildClientConfig(cfg *domain.Config) (string, error) {
	// В domain.Config можно предусмотреть поле PrivateKey для клиента.
	// Если оно не задано - это ошибка.
	if cfg.PrivateKey == "" {
		return "", fmt.Errorf("client private key is required for building config")
	}

	var b strings.Builder

	// 1) Секция [Interface] - данные клиента
	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", cfg.PrivateKey))
	if len(cfg.AllowedIps) > 0 {
		b.WriteString(fmt.Sprintf("Address = %s\n", strings.Join(cfg.AllowedIps, ",")))
	}
	b.WriteString("\n")

	// 2) Секция [Peer] - параметры сервера
	b.WriteString("[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", s.ifacePubKey))
	// Endpoint (серверный адрес) можно хранить в .env, например SERVER_ENDPOINT
	if ep := os.Getenv("SERVER_ENDPOINT"); ep != "" {
		b.WriteString(fmt.Sprintf("Endpoint = %s\n", ep))
	}
	// Опциональный PresharedKey
	if cfg.PreSharedKey != "" {
		b.WriteString(fmt.Sprintf("PresharedKey = %s\n", cfg.PreSharedKey))
	}
	// AllowedIPs для клиента обычно 0.0.0.0/0 или специфичные сети
	if len(cfg.AllowedIps) > 0 {
		b.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(cfg.AllowedIps, ",")))
	}
	// Опция keepalive
	if cfg.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", cfg.PersistentKeepalive))
	}

	logger.Logger.Debug("Built client config file", zap.String("publicKey", cfg.PublicKey))
	return b.String(), nil
}

// Rotate удаляет пир publicKey и создаёт новый с теми же AllowedIps,
// генерируя новую пару ключей.
func (s *ConfigService) Rotate(publicKey string) (*domain.Config, error) {
	// 1. Получаем старую конфигурацию
	old, err := s.repo.GetConfig(publicKey)
	if err != nil {
		return nil, fmt.Errorf("rotate: fetch old config: %w", err)
	}
	// 2. Удаляем старого пира
	if err := s.repo.DeleteConfig(publicKey); err != nil {
		return nil, fmt.Errorf("rotate: delete old peer: %w", err)
	}
	// 3. Генерируем новую пару ключей
	priv, pub, err := generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("rotate: gen keypair: %w", err)
	}
	// 4. Собираем новый domain.Config и создаём его
	newCfg := domain.Config{
		PrivateKey: priv,
		PublicKey:  pub,
		AllowedIps: old.AllowedIps,
	}
	if err := s.repo.CreateConfig(newCfg); err != nil {
		return nil, fmt.Errorf("rotate: create new peer: %w", err)
	}
	return &newCfg, nil
}

// generateKeyPair вызывает wg genkey и wg pubkey с таймаутом.
func generateKeyPair() (priv, pub string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), keyGenTimeout)
	defer cancel()

	// генерируем приватный ключ
	gen := exec.CommandContext(ctx, "wg", "genkey")
	outPriv, err := gen.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Errorf("genkey timeout")
	}
	if err != nil {
		return "", "", err
	}
	priv = strings.TrimSpace(string(outPriv))

	// генерируем публичный, передавая приватный во stdin
	cmdPub := exec.CommandContext(ctx, "wg", "pubkey")
	cmdPub.Stdin = bytes.NewReader([]byte(priv))
	outPub, err := cmdPub.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Errorf("pubkey timeout")
	}
	if err != nil {
		return "", "", err
	}
	pub = strings.TrimSpace(string(outPub))
	return priv, pub, nil
}
