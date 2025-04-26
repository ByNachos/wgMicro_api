package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"wgMicro_api/internal/domain"
)

// mockService реализует только методы, нужные для теста GetAll
type mockService struct{}

func (m *mockService) GetAll() ([]domain.Config, error) {
	return []domain.Config{
		{PublicKey: "key1", AllowedIps: []string{"10.0.0.1/32"}},
	}, nil
}

// остальным методам достаточно пустых реализаций,
// чтобы satisfy интерфейс ServiceInterface:
func (m *mockService) Get(string) (*domain.Config, error)               { return nil, nil }
func (m *mockService) Create(domain.Config) error                       { return nil }
func (m *mockService) UpdateAllowedIPs(string, []string) error          { return nil }
func (m *mockService) Delete(string) error                              { return nil }
func (m *mockService) BuildClientConfig(*domain.Config) (string, error) { return "", nil }

func TestGetAllHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// создаём handler с mockService
	h := NewConfigHandler(&mockService{})

	// настраиваем роутер
	r := gin.New()
	r.GET("/configs", h.GetAll)

	// выполняем запрос
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/configs", nil)
	r.ServeHTTP(w, req)

	// проверяем ответ
	assert.Equal(t, http.StatusOK, w.Code)

	var resp []domain.Config
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Len(t, resp, 1)
	assert.Equal(t, "key1", resp[0].PublicKey)
}
