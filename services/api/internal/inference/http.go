package inference

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

// InternalAuth guards the private generation endpoints. Only approved
// internal services (Module 6) holding the shared service token may call
// them. Fail-closed: an unconfigured token rejects every request.
type InternalAuth struct {
	Token string
}

func (a InternalAuth) authorize(r *http.Request) bool {
	if strings.TrimSpace(a.Token) == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Internal-Service-Token"))
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(a.Token)) == 1
}

// Handlers exposes the internal generation API for Module 6.
type Handlers struct {
	Service *Service
	Auth    InternalAuth
	Version string
}

func (h *Handlers) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if se, ok := err.(*ServiceError); ok {
		api.WriteError(w, r, se.Status, se.Code, se.Message, api.CorrelationID(r.Context()), h.Version)
		return
	}
	api.WriteError(w, r, http.StatusInternalServerError, CodeInternalError, "an unexpected error occurred", api.CorrelationID(r.Context()), h.Version)
}

// Generate handles POST /api/v1/internal/inference/generate.
func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	if !h.Auth.authorize(r) {
		api.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "invalid or missing service credentials", api.CorrelationID(r.Context()), h.Version)
		return
	}
	var req GenerationRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, CodeInvalidRequest, "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	res, err := h.Service.Generate(r.Context(), req)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, res, api.CorrelationID(r.Context()), api.APIVersion)
}

// Health handles GET /api/v1/internal/inference/health (service liveness).
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "inference",
		"model":   h.Service.cfg.Model,
		"metrics": h.Service.Metrics().Snapshot(),
	}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Readiness handles GET /api/v1/internal/inference/ready (model loaded check).
func (h *Handlers) Readiness(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.CheckReady(r.Context()); err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"ready":   true,
		"service": "inference",
		"model":   h.Service.cfg.Model,
		"runtime": h.Service.runtime.Name(),
	}, api.CorrelationID(r.Context()), api.APIVersion)
}
