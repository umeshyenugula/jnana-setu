package knowledge

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"example.com/ai-knowledge-assistant/services/api/internal/auth"
)

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

func idemKey(r *http.Request, body string) string {
	if v := strings.TrimSpace(r.Header.Get("Idempotency-Key")); v != "" {
		return v
	}
	return strings.TrimSpace(body)
}

type createTextRequest struct {
	Title          string `json:"title"`
	ContentText    string `json:"contentText"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// CreateText POST /api/v1/organizations/{org}/knowledge/text
func (h *Handlers) CreateText(w http.ResponseWriter, r *http.Request, orgID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+1024)
	var req createTextRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	doc, ver, out, err := h.Service.CreateFromText(r.Context(), orgID, p.UserID, CreateTextInput{
		Title: req.Title, ContentText: req.ContentText, IdempotencyKey: idemKey(r, req.IdempotencyKey),
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, map[string]any{"document": doc, "version": ver, "processing": out}, api.CorrelationID(r.Context()), api.APIVersion)
}

// CreateFile POST /api/v1/organizations/{org}/knowledge/files (multipart)
func (h *Handlers) CreateFile(w http.ResponseWriter, r *http.Request, orgID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+4<<20)
	if err := r.ParseMultipartForm(MaxUploadBytes + 1<<20); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid multipart upload", api.CorrelationID(r.Context()), h.Version)
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing file part named 'file'", api.CorrelationID(r.Context()), h.Version)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes+1))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	doc, ver, out, err := h.Service.CreateFromFile(r.Context(), orgID, p.UserID, CreateFileInput{
		Title: r.FormValue("title"), Filename: header.Filename,
		DeclaredContentType: header.Header.Get("Content-Type"),
		Data: data, IdempotencyKey: idemKey(r, r.FormValue("idempotencyKey")),
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, map[string]any{"document": doc, "version": ver, "processing": out}, api.CorrelationID(r.Context()), api.APIVersion)
}

// List GET /api/v1/organizations/{org}/knowledge
func (h *Handlers) List(w http.ResponseWriter, r *http.Request, orgID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	includeInactive := q.Get("includeInactive") == "true"
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	docs, err := h.Service.List(r.Context(), orgID, p.UserID, includeInactive, limit, offset)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"documents": docs}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Get GET /api/v1/organizations/{org}/knowledge/{doc}
func (h *Handlers) Get(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	doc, ver, err := h.Service.Get(r.Context(), orgID, p.UserID, docID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"document": doc, "currentVersion": ver}, api.CorrelationID(r.Context()), api.APIVersion)
}

// ListVersions GET .../knowledge/{doc}/versions
func (h *Handlers) ListVersions(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	vers, err := h.Service.ListVersions(r.Context(), orgID, p.UserID, docID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"versions": vers}, api.CorrelationID(r.Context()), api.APIVersion)
}

type createVersionJSON struct {
	ContentText    string `json:"contentText"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// CreateVersion POST .../knowledge/{doc}/versions (multipart file OR JSON text)
func (h *Handlers) CreateVersion(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	ct := r.Header.Get("Content-Type")
	var in CreateVersionInput
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+4<<20)
		if err := r.ParseMultipartForm(MaxUploadBytes + 1<<20); err != nil {
			api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid multipart upload", api.CorrelationID(r.Context()), h.Version)
			return
		}
		f, header, err := r.FormFile("file")
		if err != nil {
			api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "missing file part named 'file'", api.CorrelationID(r.Context()), h.Version)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes+1))
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		in = CreateVersionInput{IsFile: true, Filename: header.Filename, DeclaredContentType: header.Header.Get("Content-Type"), Data: data, IdempotencyKey: idemKey(r, r.FormValue("idempotencyKey"))}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+1024)
		var req createVersionJSON
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
			return
		}
		in = CreateVersionInput{ContentText: req.ContentText, IdempotencyKey: idemKey(r, req.IdempotencyKey)}
	}
	ver, doc, out, err := h.Service.CreateVersion(r.Context(), orgID, p.UserID, docID, in)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusCreated, map[string]any{"version": ver, "document": doc, "processing": out}, api.CorrelationID(r.Context()), api.APIVersion)
}

type patchDocRequest struct {
	Title    *string `json:"title"`
	IsActive *bool   `json:"isActive"`
}

// Patch PATCH .../knowledge/{doc}
func (h *Handlers) Patch(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req patchDocRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	var doc interface{}
	var err error
	if req.Title != nil && req.IsActive != nil {
		renamed, rerr := h.Service.Rename(r.Context(), orgID, p.UserID, docID, *req.Title)
		if rerr != nil {
			h.writeErr(w, r, rerr)
			return
		}
		updated, uerr := h.Service.SetActive(r.Context(), orgID, p.UserID, docID, *req.IsActive)
		if uerr != nil {
			h.writeErr(w, r, uerr)
			return
		}
		_ = renamed
		doc = updated
		err = nil
	} else if req.Title != nil {
		doc, err = h.Service.Rename(r.Context(), orgID, p.UserID, docID, *req.Title)
	} else if req.IsActive != nil {
		doc, err = h.Service.SetActive(r.Context(), orgID, p.UserID, docID, *req.IsActive)
	} else {
		api.WriteError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "nothing to update", api.CorrelationID(r.Context()), h.Version)
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"document": doc}, api.CorrelationID(r.Context()), api.APIVersion)
}

type reprocessRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
}

// Reprocess POST .../knowledge/{doc}/reprocess
func (h *Handlers) Reprocess(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	var req reprocessRequest
	if r.ContentLength != 0 {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req)
	}
	out, err := h.Service.RequestReprocessing(r.Context(), orgID, p.UserID, docID, idemKey(r, req.IdempotencyKey))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusAccepted, map[string]any{"processing": out}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Delete DELETE .../knowledge/{doc}
func (h *Handlers) Delete(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	out, err := h.Service.Delete(r.Context(), orgID, p.UserID, docID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"deleted": true, "processing": out}, api.CorrelationID(r.Context()), api.APIVersion)
}

// Status GET .../knowledge/{doc}/status
func (h *Handlers) Status(w http.ResponseWriter, r *http.Request, orgID, docID string) {
	p, ok := h.principal(w, r)
	if !ok {
		return
	}
	st, err := h.Service.Status(r.Context(), orgID, p.UserID, docID)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, st, api.CorrelationID(r.Context()), api.APIVersion)
}
