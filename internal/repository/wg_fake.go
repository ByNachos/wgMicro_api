package repository

import (
	"fmt"
	"wgMicro_api/internal/domain"
)

// FakeWGRepository удовлетворяет тому же API, что и WGRepository
type FakeWGRepository struct {
	Data map[string]domain.Config
}

func NewFakeWGRepository() *FakeWGRepository {
	return &FakeWGRepository{Data: make(map[string]domain.Config)}
}

func (f *FakeWGRepository) ListConfigs() ([]domain.Config, error) {
	var out []domain.Config
	for _, cfg := range f.Data {
		out = append(out, cfg)
	}
	return out, nil
}

func (f *FakeWGRepository) GetConfig(key string) (*domain.Config, error) {
	cfg, ok := f.Data[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &cfg, nil
}

func (f *FakeWGRepository) CreateConfig(cfg domain.Config) error {
	f.Data[cfg.PublicKey] = cfg
	return nil
}

func (f *FakeWGRepository) UpdateAllowedIPs(key string, ips []string) error {
	cfg, ok := f.Data[key]
	if !ok {
		return fmt.Errorf("not found")
	}
	cfg.AllowedIps = ips
	f.Data[key] = cfg
	return nil
}

func (f *FakeWGRepository) DeleteConfig(key string) error {
	delete(f.Data, key)
	return nil
}
