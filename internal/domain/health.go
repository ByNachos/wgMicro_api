package domain

// HealthResponse возвращается на /healthz
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ReadinessResponse возвращается на /readyz
type ReadinessResponse struct {
	Status string `json:"status" example:"ready"`
	Error  string `json:"error,omitempty" example:"wg command failed"`
}
