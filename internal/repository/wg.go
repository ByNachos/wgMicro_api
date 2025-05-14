// internal/repository/wg.go
package repository

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
)

// ErrWgTimeout возвращается, если команда wg не успела выполниться.
var ErrWgTimeout = errors.New("wireguard command timed out")

const cmdTimeout = 5 * time.Second

// Repo описывает методы работы с WireGuard-интерфейсом.
type Repo interface {
	ListConfigs() ([]domain.Config, error)
	GetConfig(publicKey string) (*domain.Config, error)
	CreateConfig(cfg domain.Config) error
	UpdateAllowedIPs(publicKey string, allowedIps []string) error
	DeleteConfig(publicKey string) error
}

// WGRepository работает с утилитой wg через os/exec.
type WGRepository struct {
	iface string
}

// NewWGRepository создаёт репозиторий для данного WireGuard-интерфейса.
func NewWGRepository(iface string) *WGRepository {
	return &WGRepository{iface: iface}
}

// ListConfigs вызывает wg show <iface> dump с таймаутом и парсит вывод.
func (r *WGRepository) ListConfigs() ([]domain.Config, error) {
	logger.Logger.Debug("Executing wg show dump", zap.String("iface", r.iface))

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", "show", r.iface, "dump")
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, ErrWgTimeout
	}
	if err != nil {
		return nil, fmt.Errorf("failed to execute wg show dump: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var configs []domain.Config

	for _, line := range lines {
		parts := strings.Fields(line)
		idx := 0
		if parts[0] == r.iface {
			idx = 1
		}
		if len(parts) < idx+8 {
			continue
		}

		latest, _ := strconv.ParseInt(parts[idx+4], 10, 64)
		rx, _ := strconv.ParseUint(parts[idx+5], 10, 64)
		tx, _ := strconv.ParseUint(parts[idx+6], 10, 64)

		keep := parts[idx+7]
		var pk int
		if keep != "off" {
			if v, e := strconv.Atoi(keep); e == nil {
				pk = v
			}
		}

		cfg := domain.Config{
			PublicKey:           parts[idx],
			PreSharedKey:        parts[idx+1],
			Endpoint:            parts[idx+2],
			AllowedIps:          strings.Split(parts[idx+3], ","),
			LatestHandshake:     latest,
			ReceiveBytes:        rx,
			TransmitBytes:       tx,
			PersistentKeepalive: pk,
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// GetConfig возвращает одну конфигурацию, фильтруя ListConfigs.
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

// CreateConfig выполняет wg set <iface> peer <key> allowed-ips ....
func (r *WGRepository) CreateConfig(cfg domain.Config) error {
	args := []string{"set", r.iface, "peer", cfg.PublicKey, "allowed-ips", strings.Join(cfg.AllowedIps, ",")}
	logger.Logger.Debug("Executing wg set peer", zap.Strings("args", args))

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return ErrWgTimeout
	}
	if err != nil {
		return fmt.Errorf("wg set peer failed: %v, output: %s", err, out)
	}
	return nil
}

// UpdateAllowedIPs выполняет wg set <iface> peer <key> allowed-ips ....
func (r *WGRepository) UpdateAllowedIPs(publicKey string, allowedIps []string) error {
	args := []string{"set", r.iface, "peer", publicKey, "allowed-ips", strings.Join(allowedIps, ",")}
	logger.Logger.Debug("Executing wg set allowed-ips", zap.Strings("args", args))

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return ErrWgTimeout
	}
	if err != nil {
		return fmt.Errorf("wg set allowed-ips failed: %v, output: %s", err, out)
	}
	return nil
}

// DeleteConfig выполняет wg set <iface> peer <key> remove.
func (r *WGRepository) DeleteConfig(publicKey string) error {
	args := []string{"set", r.iface, "peer", publicKey, "remove"}
	logger.Logger.Debug("Executing wg set peer remove", zap.Strings("args", args))

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wg", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return ErrWgTimeout
	}
	if err != nil {
		return fmt.Errorf("wg set peer remove failed: %v, output: %s", err, out)
	}
	return nil
}
