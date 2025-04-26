package domain

// ErrorResponse стандартная структура ответа с ошибкой
type ErrorResponse struct {
	Error string `json:"error"`
}
