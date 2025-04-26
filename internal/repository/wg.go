package repository

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
)

// Repo описывает методы работы с WireGuard-интерфейсом.
// Любая реализация с таким набором методов подходит сервису.
type Repo interface {
	ListConfigs() ([]domain.Config, error)
	GetConfig(publicKey string) (*domain.Config, error)
	CreateConfig(cfg domain.Config) error
	UpdateAllowedIPs(publicKey string, allowedIps []string) error
	DeleteConfig(publicKey string) error
}

// WGRepository инкапсулирует имя WireGuard-интерфейса и методы работы с ним.
type WGRepository struct {
	iface string
}

// NewWGRepository создаёт новый репозиторий для указанного интерфейса, например "wg0".
func NewWGRepository(iface string) *WGRepository {
	return &WGRepository{iface: iface}
}

// ListConfigs возвращает список всех peer-конфигураций интерфейса,
// парся вывод команды: wg show <iface> dump
func (r *WGRepository) ListConfigs() ([]domain.Config, error) {
	logger.Logger.Debug("Executing wg show dump", zap.String("iface", r.iface))
	out, err := exec.Command("wg", "show", r.iface, "dump").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute wg show dump: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var configs []domain.Config

	for _, line := range lines {
		parts := strings.Fields(line)
		// если команда возвращает имя интерфейса в первой колонке - пропускаем её
		idx := 0
		if parts[0] == r.iface {
			idx = 1
		}
		// ждём минимум 8 полей после возможного имени iface
		if len(parts) < idx+8 {
			continue
		}

		// парсим поля в соответствии с документацией wg dump:
		// publicKey, presharedKey, endpoint, allowedIPs, latestHandshake, rxBytes, txBytes, persistentKeepalive
		publicKey := parts[idx]
		presharedKey := parts[idx+1]
		endpoint := parts[idx+2]
		allowedIps := strings.Split(parts[idx+3], ",")
		latestHandshake, _ := strconv.ParseInt(parts[idx+4], 10, 64)
		receiveBytes, _ := strconv.ParseUint(parts[idx+5], 10, 64)
		transmitBytes, _ := strconv.ParseUint(parts[idx+6], 10, 64)

		// persistentKeepalive может быть "off" или число секунд
		keepaliveStr := parts[idx+7]
		var persistentKeepalive int
		if keepaliveStr != "off" {
			if v, err := strconv.Atoi(keepaliveStr); err == nil {
				persistentKeepalive = v
			}
		}

		cfg := domain.Config{
			PublicKey:           publicKey,
			PreSharedKey:        presharedKey,
			Endpoint:            endpoint,
			AllowedIps:          allowedIps,
			LatestHandshake:     latestHandshake,
			ReceiveBytes:        receiveBytes,
			TransmitBytes:       transmitBytes,
			PersistentKeepalive: persistentKeepalive,
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

// GetConfig возвращает конфигурацию одного peer’а, фильтруя результат ListConfigs.
func (r *WGRepository) GetConfig(publicKey string) (*domain.Config, error) {
	all, err := r.ListConfigs()
	if err != nil {
		return nil, err
	}
	for _, cfg := range all {
		if cfg.PublicKey == publicKey {
			return &cfg, nil
		}
	}
	return nil, fmt.Errorf("config with publicKey %s not found", publicKey)
}

// CreateConfig добавляет нового peer’а с заданным publicKey и allowed-ips.
func (r *WGRepository) CreateConfig(cfg domain.Config) error {
	args := []string{
		"set", r.iface, "peer", cfg.PublicKey,
		"allowed-ips", strings.Join(cfg.AllowedIps, ","),
	}
	logger.Logger.Debug("Executing wg set peer", zap.Strings("args", args))
	if out, err := exec.Command("wg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("wg set peer failed: %v, output: %s", err, out)
	}
	return nil
}

// UpdateAllowedIPs полностью заменяет список allowed-ips у существующего peer’а.
func (r *WGRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	args := []string{
		"set", r.iface, "peer", publicKey,
		"allowed-ips", strings.Join(allowedIps, ","),
	}
	logger.Logger.Debug("Executing wg set allowed-ips", zap.Strings("args", args))
	if out, err := exec.Command("wg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("wg set allowed-ips failed: %v, output: %s", err, out)
	}
	return nil
}

// DeleteAllowedIP удаляет один IP из allowed-ips peer’а.
func (r *WGRepository) DeleteAllowedIP(publicKey, ip string) error {
	args := []string{"set", r.iface, "peer", publicKey, "remove-allowed-ips", ip}
	logger.Logger.Debug("Executing wg set remove-allowed-ips", zap.Strings("args", args))
	if out, err := exec.Command("wg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("wg set remove-allowed-ips failed: %v, output: %s", err, out)
	}
	return nil
}

// DeleteConfig полностью удаляет peer’а из интерфейса.
func (r *WGRepository) DeleteConfig(publicKey string) error {
	args := []string{"set", r.iface, "peer", publicKey, "remove"}
	logger.Logger.Debug("Executing wg set peer remove", zap.Strings("args", args))
	if out, err := exec.Command("wg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("wg set peer remove failed: %v, output: %s", err, out)
	}
	return nil
}
