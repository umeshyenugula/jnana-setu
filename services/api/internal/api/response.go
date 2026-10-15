package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// Error describes a machine-readable API failure.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Meta contains request-scoped metadata returned with every response.
type Meta struct {
	CorrelationID string `json:"correlationId"`
	Timestamp     string `json:"timestamp"`
	Version       string `json:"version"`
	Path          string `json:"path"`
}

// SuccessResponse is the standard envelope for successful API calls.
type SuccessResponse struct {
	Success bool           `json:"success"`
	Data    any            `json:"data"`
	Meta    Meta           `json:"meta"`
	Extra   map[string]any `json:"-"`
}

// ErrorResponse is the standard envelope for failed API calls.
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   Error  `json:"error"`
	Meta    Meta   `json:"meta"`
}

// WriteJSON writes a JSON response with the supplied status code.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newMeta(r *http.Request, correlationID, version string) Meta {
	return Meta{
		CorrelationID: correlationID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Version:       version,
		Path:          r.URL.Path,
	}
}

func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data any, correlationID, version string) {
	WriteJSON(w, status, SuccessResponse{
		Success: true,
		Data:    data,
		Meta:    newMeta(r, correlationID, version),
	})
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message, correlationID, version string) {
	WriteJSON(w, status, ErrorResponse{
		Success: false,
		Error:   Error{Code: code, Message: message},
		Meta:    newMeta(r, correlationID, version),
	})
}
