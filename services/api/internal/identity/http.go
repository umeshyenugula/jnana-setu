package identity

import (
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"example.com/ai-knowledge-assistant/services/api/internal/auth"
)

type Handlers struct {
	Service *Service
	Version string
}

type createOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type addMemberRequest struct {
	UserID string `json:"userId"`
	Role   Role   `json:"role"`
}

type updateMemberRequest struct {
	Role Role `json:"role"`
}

type createWidgetRequest struct {
	DisplayName    string   `json:"displayName"`
	AllowedOrigins  []string `json:"allowedOrigins"`
}

type updateWidgetRequest struct {
	DisplayName    *string  `json:"displayName"`
	AllowedOrigins *[]string `json:"allowedOrigins"`
	Active         *bool    `json:"active"`
}

func (h *Handlers) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
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

func (h *Handlers) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req createOrgRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	org, membership, err := h.Service.CreateOrganization(r.Context(), principal.UserID, req.Name, req.Slug)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, map[string]any{"organization": org, "membership": membership}, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	orgs, err := h.Service.ListOrganizations(r.Context(), principal.UserID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"organizations": orgs}, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) GetOrganization(w http.ResponseWriter, r *http.Request, orgID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	org, err := h.Service.GetOrganization(r.Context(), principal.UserID, orgID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, org, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) ListMembers(w http.ResponseWriter, r *http.Request, orgID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	members, err := h.Service.ListMembers(r.Context(), principal.UserID, orgID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"members": members}, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) AddMember(w http.ResponseWriter, r *http.Request, orgID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req addMemberRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	membership, err := h.Service.AddMember(r.Context(), principal.UserID, orgID, req.UserID, req.Role)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, membership, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) UpdateMember(w http.ResponseWriter, r *http.Request, orgID, userID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req updateMemberRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	membership, err := h.Service.UpdateMember(r.Context(), principal.UserID, orgID, userID, req.Role)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, membership, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) DeleteMember(w http.ResponseWriter, r *http.Request, orgID, userID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	if err := h.Service.DeleteMember(r.Context(), principal.UserID, orgID, userID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"deleted": true}, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) CreateWidget(w http.ResponseWriter, r *http.Request, orgID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req createWidgetRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	widget, err := h.Service.CreateWidget(r.Context(), principal.UserID, orgID, CreateWidgetParams{DisplayName: req.DisplayName, AllowedOrigins: req.AllowedOrigins})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, widget, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) ListWidgets(w http.ResponseWriter, r *http.Request, orgID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	widgets, err := h.Service.ListWidgets(r.Context(), principal.UserID, orgID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"widgets": widgets}, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) UpdateWidget(w http.ResponseWriter, r *http.Request, orgID, widgetID string) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req updateWidgetRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	widget, err := h.Service.UpdateWidget(r.Context(), principal.UserID, orgID, widgetID, UpdateWidgetParams{DisplayName: req.DisplayName, AllowedOrigins: req.AllowedOrigins, Active: req.Active})
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, widget, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) WidgetConfig(w http.ResponseWriter, r *http.Request, publicWidgetID string) {
	cfg, err := h.Service.WidgetConfig(r.Context(), publicWidgetID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, cfg, api.CorrelationID(r.Context()), api.APIVersion)
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.principal(w, r)
	if !ok {
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"userId": principal.UserID, "email": principal.Email}, api.CorrelationID(r.Context()), api.APIVersion)
}

// splitPath returns trimmed, non-empty path segments.
func splitPath(path string) []string {
	var out []string
	for _, p := range strings.Split(strings.Trim(path, "/"), "/") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
