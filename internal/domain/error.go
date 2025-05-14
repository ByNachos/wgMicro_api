package domain

// ErrorResponse представляет стандартную структуру ошибки API
type ErrorResponse struct {
	Error string `json:"error" example:"Peer не найден"`
}
