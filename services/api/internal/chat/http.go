package chat

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
)

// Handlers exposes the public chat API for Module 7. The endpoint is public
// by design (anonymous visitors); protection comes from widget binding,
// origin allow-lists, rate limits, and server-side tenant resolution —
// never from browser-supplied tenant identifiers.
type Handlers struct {
	Service *Service
	Version string
}

func (h *Handlers) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if se, ok := err.(*ServiceError); ok {
		api.WriteError(w, r, se.Status, se.Code, se.Message, api.CorrelationID(r.Context()), h.Version)
		return
	}
	api.WriteError(w, r, http.StatusInternalServerError, CodeInternalError, "chat is temporarily unavailable", api.CorrelationID(r.Context()), h.Version)
}

// Chat handles POST /api/v1/chat.
func (h *Handlers) Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.Service.cfg.MaxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		api.WriteError(w, r, http.StatusBadRequest, CodeInvalidRequest, "invalid request body", api.CorrelationID(r.Context()), h.Version)
		return
	}
	res, err := h.Service.Chat(r.Context(), IncomingChat{
		WidgetID:       req.WidgetID,
		SessionID:      req.SessionID,
		Message:        req.Message,
		IdempotencyKey: req.IdempotencyKey,
		Origin:         r.Header.Get("Origin"),
		ClientIP:       clientIP(r),
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, res, api.CorrelationID(r.Context()), api.APIVersion)
}

// History handles GET /api/v1/chat/sessions/{session}?widgetId=....
func (h *Handlers) History(w http.ResponseWriter, r *http.Request, sessionID string) {
	widgetID := strings.TrimSpace(r.URL.Query().Get("widgetId"))
	msgs, err := h.Service.History(r.Context(), widgetID, sessionID, r.Header.Get("Origin"))
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	api.WriteSuccess(w, r, http.StatusOK, map[string]any{"messages": msgs}, api.CorrelationID(r.Context()), api.APIVersion)
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, _ := strings.Cut(forwarded, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
