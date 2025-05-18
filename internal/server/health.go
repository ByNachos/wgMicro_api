package server

import (
	"errors"   // For errors.Is
	"net/http" // Standard HTTP status codes and utilities

	// For simulating work or timeouts if needed in probes
	"wgMicro_api/internal/domain"     // For HealthResponse and ReadinessResponse structures
	"wgMicro_api/internal/logger"     // For logging within probes if necessary
	"wgMicro_api/internal/repository" // For repository.Repo interface and specific errors

	"github.com/gin-gonic/gin" // Gin framework
	"go.uber.org/zap"
)

// HealthLiveness godoc
// @Summary      Liveness probe for the service
// @Description  Indicates if the application process is running and responsive.
// @Description  A 200 OK response means the service is live.
// @Tags         health
// @Produce      json
// @Success      200  {object}  domain.HealthResponse  "Service is live."
// @Router       /healthz [get]
func HealthLiveness(c *gin.Context) {
	// This probe should be very lightweight and quick.
	// It confirms the HTTP server is up and the handler is reachable.
	response := domain.HealthResponse{Status: "ok"}
	c.JSON(http.StatusOK, response)
}

// HealthReadiness godoc
// @Summary      Readiness probe for the service
// @Description  Indicates if the application is ready to accept and process new requests.
// @Description  This typically involves checking dependencies like database connections or, in this case, WireGuard utility accessibility.
// @Tags         health
// @Produce      json
// @Success      200  {object}  domain.ReadinessResponse "Service is ready to handle requests."
// @Failure      503  {object}  domain.ReadinessResponse "Service is not ready, e.g., WireGuard is inaccessible or command timed out."
// @Router       /readyz [get]
func HealthReadiness(repo repository.Repo) gin.HandlerFunc {
	if repo == nil {
		// This is a programming error; repo should always be provided.
		// Log fatal, as the readiness probe cannot function.
		logger.Logger.Fatal("HealthReadiness probe initialized with a nil repository")
		// To prevent panic in handler if we didn't Fatal above:
		// return func(c *gin.Context) {
		// 	 c.JSON(http.StatusInternalServerError, domain.ReadinessResponse{
		// 		 Status: "error",
		// 		 Error:  "Readiness probe misconfigured: repository is nil",
		// 	 })
		// }
	}

	return func(c *gin.Context) {
		// Attempt a lightweight operation to check WireGuard accessibility.
		// ListConfigs is suitable as it performs a 'wg show dump'.
		_, err := repo.ListConfigs() // Timeout for this is handled by the repository's cmdTimeout.

		if err != nil {
			// If ListConfigs fails, the service is not ready.
			logger.Logger.Warn("Readiness probe failed: Error connecting to or querying WireGuard.",
				zap.Error(err))

			errMsg := "WireGuard utility is not accessible or responding."
			// Provide more specific error message if it's a known type.
			if errors.Is(err, repository.ErrWgTimeout) {
				errMsg = "WireGuard command timed out during readiness check."
			} else if err.Error() != "" { // Use error from repo if it's not a timeout and not empty
				errMsg = "WireGuard check failed: " + err.Error()
			}

			response := domain.ReadinessResponse{
				Status: "not ready",
				Error:  errMsg,
			}
			c.JSON(http.StatusServiceUnavailable, response)
			return
		}

		// If ListConfigs succeeds, WireGuard is accessible.
		response := domain.ReadinessResponse{Status: "ready"}
		c.JSON(http.StatusOK, response)
	}
}
