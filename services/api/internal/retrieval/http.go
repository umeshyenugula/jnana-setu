package retrieval

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

// InternalAuth guards the private retrieval endpoint. Only trusted
// application services (Module 6) holding the shared service token may call
// it. There is deliberately no JWT/user path here: tenant authorization
// happened in Module 6's server-side tenant resolution before this call.
// Fail-closed: an unconfigured token rejects every request.
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

// Handlers exposes the private retrieval endpoint for Module 6.
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
	api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred", api.CorrelationID(r.Context()), h.Version)
}

type queryRequest struct {
	OrganizationID string   `json:"organizationId"`
	Query          string   `json:"query"`
	TopK           int      `json:"topK"`
	DocumentIDs    []string `json:"documentIds"`
}

// Query handles POST /api/v1/internal/retrieval/query.
func (h *Handlers) Query(w http.ResponseWriter, r *http.Request) {
	if !h.Auth.authorize(r) {
		api.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing service credentials", api.CorrelationID(r.Context()), h.Version)
		return
	}
	var req queryRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	res, err := h.Service.Retrieve(r.Context(), Query{
		OrganizationID: req.OrganizationID, Text: req.Query, TopK: req.TopK,
		Filters:       Filters{DocumentIDs: req.DocumentIDs},
		CorrelationID: api.CorrelationID(r.Context()),
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, res, api.CorrelationID(r.Context()), api.APIVersion)
}
