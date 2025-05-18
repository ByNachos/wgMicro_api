package server

import (
	"time"

	"github.com/gin-gonic/gin" // Gin framework for building web applications
	"go.uber.org/zap"          // Structured, leveled logging library

	"wgMicro_api/internal/handler"    // Our application's request handlers
	"wgMicro_api/internal/logger"     // Global logger instance
	"wgMicro_api/internal/repository" // Repository interface, needed for readiness probe
)

// NewRouter creates and configures a new Gin engine with all application routes.
// It sets up middleware for logging and recovery, and registers handlers for
// health checks, WireGuard configuration management, and Swagger documentation.
//
// Parameters:
//
//	cfgHandler: The handler for WireGuard configuration-related requests.
//	repo: The repository implementation, passed to the readiness probe to check WireGuard connectivity.
//
// Returns:
//
//	A pointer to the configured *gin.Engine.
func NewRouter(cfgHandler *handler.ConfigHandler, repo repository.Repo) *gin.Engine {
	if cfgHandler == nil {
		logger.Logger.Fatal("ConfigHandler cannot be nil for NewRouter")
	}
	if repo == nil {
		logger.Logger.Fatal("Repository cannot be nil for NewRouter (required for readiness probe)")
	}

	r := gin.New() // Creates a new Gin engine without any default middleware (e.g. logger, recovery).

	// Middleware:
	// gin.Recovery() recovers from any panics and writes a 500 if there was one.
	// It prevents the server from crashing.
	r.Use(gin.Recovery())
	// ZapLogger() is our custom middleware for request logging using Zap.
	r.Use(ZapLogger(logger.Logger)) // Pass the global logger to the middleware

	// Health Check Endpoints:
	// /healthz: Liveness probe - indicates if the application process is running.
	r.GET("/healthz", HealthLiveness) // Renamed for clarity
	// /readyz: Readiness probe - indicates if the application is ready to serve traffic
	// (e.g., can connect to WireGuard).
	r.GET("/readyz", HealthReadiness(repo)) // Renamed for clarity

	// API v1 Routes for WireGuard Configurations:
	// Grouping API routes under a version path is a good practice.
	// Example: apiV1 := r.Group("/api/v1")
	// For now, routes are at the root as per original structure.
	// TODO: Consider namespacing API routes under /api/v1 in the future.

	// CRUD operations for peer configurations
	r.GET("/configs", cfgHandler.GetAll)
	r.POST("/configs", cfgHandler.CreateConfig)
	r.GET("/configs/:publicKey", cfgHandler.GetByPublicKey)
	r.PUT("/configs/:publicKey/allowed-ips", cfgHandler.UpdateAllowedIPs)
	r.DELETE("/configs/:publicKey", cfgHandler.DeleteConfig)

	// Additional operations
	r.POST("/configs/client-file", cfgHandler.GenerateClientConfigFile)
	r.POST("/configs/:publicKey/rotate", cfgHandler.RotatePeer) // Rotate peer keys

	// Swagger documentation endpoint is set up in main.go as it might require
	// dynamic host configuration based on swagger annotations and app config.
	// If not, it could also be registered here.

	logger.Logger.Info("Router initialized with all routes and middleware.")
	return r
}

// ZapLogger is a Gin middleware that logs requests using a provided Zap logger.
// It logs the incoming request (method, path) and the response (status, duration).
//
// Parameters:
//
//	log: An instance of *zap.Logger to use for logging.
//
// Returns:
//
//	A gin.HandlerFunc (middleware).
func ZapLogger(log *zap.Logger) gin.HandlerFunc {
	if log == nil {
		// Fallback to a no-op function or panic, as logging is crucial.
		// For simplicity, let's assume logger.Logger is always initialized before router.
		// If a nil logger is passed, it would panic on usage.
		// A production system might initialize a default stdout logger here.
		panic("ZapLogger middleware initialized with a nil logger")
	}
	return func(c *gin.Context) {
		requestStartTime := time.Now()

		// Log incoming request details
		log.Info("Incoming request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("clientIP", c.ClientIP()),
			zap.String("userAgent", c.Request.UserAgent()),
		)

		// Process request
		c.Next()

		// Log response details after the request is handled
		duration := time.Since(requestStartTime)
		log.Info("Request handled",
			zap.String("method", c.Request.Method), // Redundant but keeps context in one log line
			zap.String("path", c.Request.URL.Path), // Redundant for same reason
			zap.Int("status", c.Writer.Status()),
			zap.Duration("duration", duration), // More common field name for duration
			// zap.Int("size", c.Writer.Size()), // Size of response body, if needed
		)
	}
}
