package observability

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Audit conventions (Task E). Business modules own their audit events; this
// package supplies the shared schema, validation, and sinks so records stay
// consistent. Customer content is never copied into audit records: details
// carry identifiers, counts, and outcomes only.

// Event names owned by their modules (reference; emit from the owning module).
const (
	EventDocumentUploaded    = "document.uploaded"
	EventDocumentPublished   = "document.published"
	EventDocumentDeactivated = "document.deactivated"
	EventDocumentDeleted     = "document.deleted"
	EventIngestionFailed     = "ingestion.failed_final"
	EventWidgetChanged       = "widget.changed"
	EventMembershipChanged   = "membership.changed"
	EventAuthFailures        = "auth.repeated_failures"
	EventModelUnavailable    = "model.unavailable"
	EventConfigChanged       = "config.changed"
)

// Event is one durable operational record.
type Event struct {
	Service       string         `json:"service"`
	Name          string         `json:"name"`
	CorrelationID string         `json:"correlationId"`
	OrganizationID *string       `json:"organizationId,omitempty"`
	Actor         *string        `json:"actor,omitempty"`
	Outcome       string         `json:"outcome"` // ok | error
	Detail        map[string]any `json:"detail,omitempty"`
	OccurredAt    time.Time      `json:"occurredAt"`
}

// blockedDetailKeys must never appear in audit details. Identifier keys
// (document_id, version_id, chunk_id, idempotency_key) are explicitly
// allowed; content and credential keys are not.
var blockedDetailKeys = []string{
	"password", "secret", "token", "authorization", "api_key", "apikey",
	"prompt", "message", "content", "answer", "question", "query",
	"embedding", "session",
}

// ValidateEvent rejects malformed or sensitive audit events before any sink
// sees them. Untrusted callers can never fabricate privileged events because
// only server-side code constructs Events; HTTP handlers must not accept
// Event payloads from clients.
func ValidateEvent(e Event) error {
	if e.Service == "" || e.Name == "" || e.Outcome == "" {
		return &AuditError{Msg: "service, name, and outcome are required"}
	}
	if e.Outcome != "ok" && e.Outcome != "error" {
		return &AuditError{Msg: "outcome must be ok or error"}
	}
	raw, err := json.Marshal(e.Detail)
	if err != nil {
		return &AuditError{Msg: "detail is not JSON-encodable"}
	}
	if len(raw) > 4096 {
		return &AuditError{Msg: "detail exceeds 4KiB; store identifiers, not content"}
	}
	for k := range e.Detail {
		lower := strings.ToLower(k)
		for _, b := range blockedDetailKeys {
			if strings.Contains(lower, b) {
				return &AuditError{Msg: "detail key blocked: " + k}
			}
		}
	}
	return nil
}

// AuditError is a validation failure.
type AuditError struct{ Msg string }

func (e *AuditError) Error() string { return "invalid audit event: " + e.Msg }

// Emitter persists validated audit events.
type Emitter interface {
	Emit(ctx context.Context, e Event) error
}

// LogEmitter records events as structured log lines (default sink; always
// available, survives database outages).
type LogEmitter struct {
	Logger *Logger
}

func (l *LogEmitter) Emit(ctx context.Context, e Event) error {
	if err := ValidateEvent(e); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	attrs := []slog.Attr{
		slog.String("audit_outcome", e.Outcome),
	}
	if e.OrganizationID != nil {
		attrs = append(attrs, slog.String("organization_id", *e.OrganizationID))
	}
	if e.Actor != nil {
		attrs = append(attrs, slog.String("actor", *e.Actor))
	}
	for k, v := range e.Detail {
		attrs = append(attrs, slog.Any("detail_"+k, v))
	}
	l.Logger.Event(ctx, "audit."+e.Name, attrs...)
	return nil
}

// PostgresEmitter writes events to public.operational_events (migration
// 0006). Access is service-role only; no browser role can read or write it.
type PostgresEmitter struct {
	Pool *pgxpool.Pool
}

// Emit validates then inserts. Validation failures are returned; insert
// failures are returned so callers can fall back to the log sink.
func (p *PostgresEmitter) Emit(ctx context.Context, e Event) error {
	if err := ValidateEvent(e); err != nil {
		return err
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return &AuditError{Msg: "detail is not JSON-encodable"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = p.Pool.Exec(ctx, `
		insert into public.operational_events
		    (service, name, correlation_id, organization_id, actor, outcome, detail, occurred_at)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`,
		e.Service, e.Name, nullIfEmpty(e.CorrelationID), nullIfEmpty(ptrStr(e.OrganizationID)),
		nullIfEmpty(ptrStr(e.Actor)), e.Outcome, detail, e.OccurredAt)
	return err
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
