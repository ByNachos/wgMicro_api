package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zaptest"

	"wgMicro_api/internal/domain"
	"wgMicro_api/internal/handler"
	"wgMicro_api/internal/logger"
	"wgMicro_api/internal/repository"
	"wgMicro_api/internal/service"
)

// TestIntegration_ConfigCRUD проверяет полный цикл CRUD через HTTP
func TestIntegration_ConfigCRUD(t *testing.T) {
	// 1. Подменяем логгер
	logger.Logger = zaptest.NewLogger(t)
	gin.SetMode(gin.TestMode)

	// 2. Подготовка ENV для сервиса
	os.Setenv("INTERFACE_PUBLIC_KEY", "srvPubKey")
	os.Setenv("INTERFACE_PRIVATE_KEY", "srvPrivKey")
	os.Unsetenv("SERVER_ENDPOINT") // не нужен для CRUD

	// 3. Фейковый репозиторий и сервис
	fakeRepo := repository.NewFakeWGRepository() // возвращает *FakeWGRepository
	svc := service.NewConfigService(fakeRepo)    // OK, FakeWGRepository реализует Repo
	h := handler.NewConfigHandler(svc)

	// 4. Роутер
	router := gin.New()
	router.GET("/configs", h.GetAll)
	router.POST("/configs", h.CreateConfig)
	router.GET("/configs/:publicKey", h.GetByPublicKey)
	router.PUT("/configs/:publicKey/allowed-ips", h.UpdateAllowedIPs)
	router.DELETE("/configs/:publicKey", h.DeleteConfig)

	// 5. TEST: GET /configs → должно быть пусто
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/configs", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var list []domain.Config
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %v", list)
	}

	// 6. TEST: POST /configs
	newCfg := domain.Config{
		PublicKey:  "key1",
		AllowedIps: []string{"10.0.0.1/32"},
	}
	body, _ := json.Marshal(newCfg)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/configs", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	// 7. TEST: GET /configs → должен вернуть один элемент
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/configs", nil)
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0].PublicKey != "key1" {
		t.Fatalf("unexpected list after create: %v", list)
	}

	// 8. TEST: PUT /configs/key1/allowed-ips
	update := map[string][]string{"allowedIps": {"10.0.0.2/32"}}
	body, _ = json.Marshal(update)
	w = httptest.NewRecorder()
	url := fmt.Sprintf("/configs/%s/allowed-ips", "key1")
	req, _ = http.NewRequest(http.MethodPut, url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d", w.Code)
	}

	// 9. TEST: GET /configs/key1 → проверяем новые IP
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/configs/key1", nil)
	router.ServeHTTP(w, req)
	var single domain.Config
	json.Unmarshal(w.Body.Bytes(), &single)
	if len(single.AllowedIps) != 1 || single.AllowedIps[0] != "10.0.0.2/32" {
		t.Fatalf("update did not apply: %v", single.AllowedIps)
	}

	// 10. TEST: DELETE /configs/key1
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodDelete, "/configs/key1", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d", w.Code)
	}

	// 11. TEST: GET /configs → обратно пусто
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/configs", nil)
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("expected empty after delete, got %v", list)
	}
}
