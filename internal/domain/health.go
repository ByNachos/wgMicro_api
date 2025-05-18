package domain

// HealthResponse is the JSON response for the /healthz liveness probe.
// It indicates that the service process is running.
type HealthResponse struct {
	// Status indicates the health of the service.
	// Expected value is "ok" for a healthy service.
	// Example: "ok"
	Status string `json:"status" example:"ok"`
}

// ReadinessResponse is the JSON response for the /readyz readiness probe.
// It indicates if the service is ready to accept traffic (e.g., can connect to WireGuard).
type ReadinessResponse struct {
	// Status indicates the readiness of the service.
	// Expected values: "ready" or "not ready".
	// Example: "ready"
	Status string `json:"status" example:"ready"`
	// Error contains a message if the service is not ready, explaining the reason.
	// This field is omitted if the status is "ready".
	// Example: "wg command failed: wireguard command timed out"
	Error string `json:"error,omitempty" example:"wg command failed"`
}
