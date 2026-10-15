package processing

import (
	"encoding/json"
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"example.com/ai-knowledge-assistant/services/api/internal/auth"
)

// Handlers exposes internal processing operations under Module 0 conventions.
// These are application-level endpoints (JWT + membership), not public search.
type Handlers struct {
	Service *Service
	Version string
}

func (h *Handlers) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if se, ok := err.(*ServiceError); ok {
		api.WriteError(w, r, se.Status, se.Code, se.Message, api.CorrelationID(r.Context()), h.Version)
		return
	}
	api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred", api.CorrelationID(r.Context()), h.Version)
}

func (h *Handlers) principal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		api.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", api.CorrelationID(r.Context()), h.Version)
		return auth.Principal{}, false
	}
	return p, true
}

type submitJobRequest struct {
	OrganizationID string  `json:"organizationId"`
	DocumentID     string  `json:"documentId"`
	VersionID      string  `json:"versionId"`
	Operation      string  `json:"operation"`
	IdempotencyKey string  `json:"idempotencyKey"`
	RequestedBy    *string `json:"requestedBy"`
}

// Submit POST /api/v1/processing/jobs
func (h *Handlers) Submit(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req submitJobRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	job, err := h.Service.SubmitJob(r.Context(), p.UserID, SubmitInput{
		OrganizationID: req.OrganizationID, DocumentID: req.DocumentID, VersionID: req.VersionID,
		Operation: req.Operation, IdempotencyKey: key, RequestedBy: req.RequestedBy,
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	status := http.StatusCreated
	if job.Attempts > 0 {
		status = http.StatusOK
	}
	api.WriteSuccess(w, r, status, map[string]any{"job": job}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Get GET /api/v1/processing/jobs/{jobId}
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request, jobID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	job, err := h.Service.GetJob(r.Context(), p.UserID, jobID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"job": job}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Retry POST /api/v1/processing/jobs/{jobId}/retry
func (h *Handlers) Retry(w http.ResponseWriter, r *http.Request, jobID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	job, err := h.Service.RetryJob(r.Context(), p.UserID, jobID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusAccepted, map[string]any{"job": job}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Cancel POST /api/v1/processing/jobs/{jobId}/cancel
func (h *Handlers) Cancel(w http.ResponseWriter, r *http.Request, jobID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	job, err := h.Service.CancelJob(r.Context(), p.UserID, jobID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"job": job}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Health GET /api/v1/processing/health (public liveness, no auth).
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, r, http.StatusOK, h.Service.Health(), api.CorrelationID(r.Context()), api.APIVersion)
}

// Readiness GET /api/v1/processing/ready (checks local embedding worker).
func (h *Handlers) Readiness(w http.ResponseWriter, r *http.Request) {
	data, err := h.Service.Readiness(r.Context())
	if err != nil {
		api.WriteError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "embedding worker is not ready", api.CorrelationID(r.Context()), h.Version)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, data, api.CorrelationID(r.Context()), api.APIVersion)
}
