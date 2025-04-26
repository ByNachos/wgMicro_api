package service

import (
	"os"
	"strings"
	"testing"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"

	"go.uber.org/zap/zaptest"
)

func TestBuildClientConfig(t *testing.T) {
	// Подменяем глобальный логгер на тестовый, чтобы не засорять вывод
	logger.Logger = zaptest.NewLogger(t)

	// Готовим ENV для endpoint и интерфейсных ключей
	os.Setenv("SERVER_ENDPOINT", "1.2.3.4:51820")
	os.Setenv("INTERFACE_PUBLIC_KEY", "srvPubKey")
	os.Setenv("INTERFACE_PRIVATE_KEY", "srvPrivKey")
	defer os.Unsetenv("SERVER_ENDPOINT")
	defer os.Unsetenv("INTERFACE_PUBLIC_KEY")
	defer os.Unsetenv("INTERFACE_PRIVATE_KEY")

	// Репозиторий здесь не используется в BuildClientConfig, можно передать любой
	repo := repository.NewWGRepository("wg0")
	svc := NewConfigService(repo)

	// Формируем domain.Config с обязательным полем PrivateKey
	cfg := &domain.Config{
		PrivateKey:          "cliPrivKey",
		PublicKey:           "cliPubKey",
		PreSharedKey:        "psk123",
		AllowedIps:          []string{"10.0.0.2/32"},
		PersistentKeepalive: 25,
	}

	out, err := svc.BuildClientConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Проверяем ключевые фрагменты в сгенерированном конфиге
	tests := []string{
		"PrivateKey = cliPrivKey",
		"Address = 10.0.0.2/32",
		"PublicKey = srvPubKey",
		"Endpoint = 1.2.3.4:51820",
		"PresharedKey = psk123",
		"PersistentKeepalive = 25",
	}
	for _, fragment := range tests {
		if !strings.Contains(out, fragment) {
			t.Errorf("expected fragment %q in output, got:\n%s", fragment, out)
		}
	}
}
