package domain

// ErrorResponse represents a generic JSON error response body for API errors.
// It provides a simple structure with a single "error" field containing a message.
type ErrorResponse struct {
	// Error contains a human-readable message describing the error.
	// This message is intended for the API consumer.
	// Example: "Peer not found" or "Invalid input: public key is malformed"
	Error string `json:"error" example:"Peer not found"`
}
