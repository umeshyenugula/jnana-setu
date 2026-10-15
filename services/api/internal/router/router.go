package router

import (
	"net/http"
	"strings"

	"example.com/ai-knowledge-assistant/services/api/internal/api"
	"example.com/ai-knowledge-assistant/services/api/internal/auth"
	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/knowledge"
	"example.com/ai-knowledge-assistant/services/api/internal/processing"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
)

type Deps struct {
	Version    string
	Auth       *auth.Verifier
	Handlers   *identity.Handlers
	Knowledge  *knowledge.Handlers
	Processing *processing.Handlers
	Retrieval  *retrieval.Handlers
}

func New(deps Deps) http.Handler {
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		segments := strings.Split(strings.Trim(path, "/"), "/")
		// /api/v1/...
		if len(segments) < 2 || segments[0] != "api" || segments[1] != "v1" {
			api.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.", api.CorrelationID(r.Context()), api.APIVersion)
			return
		}
		if len(segments) > 3 && segments[2] == "organizations" && isKnowledgePath(segments) && deps.Knowledge == nil {
			api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "knowledge service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
			return
		}
		rest := segments[2:]

		switch {
		case len(rest) == 1 && rest[0] == "health":
			if r.Method != http.MethodGet {
				api.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The request method is not supported for this resource.", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			api.Health(w, r, api.CorrelationID(r.Context()), deps.Version)

		case len(rest) == 1 && rest[0] == "me":
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.Me(w, r) })).ServeHTTP(w, r)

		case len(rest) == 1 && rest[0] == "organizations":
			switch r.Method {
			case http.MethodGet:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.ListOrganizations(w, r) })).ServeHTTP(w, r)
			case http.MethodPost:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.CreateOrganization(w, r) })).ServeHTTP(w, r)
			default:
				api.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The request method is not supported for this resource.", api.CorrelationID(r.Context()), api.APIVersion)
			}

		case len(rest) == 2 && rest[0] == "organizations":
			orgID := rest[1]
			if r.Method != http.MethodGet {
				api.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The request method is not supported for this resource.", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.GetOrganization(w, r, orgID) })).ServeHTTP(w, r)

		case len(rest) == 3 && rest[0] == "organizations" && rest[2] == "members":
			orgID := rest[1]
			switch r.Method {
			case http.MethodGet:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.ListMembers(w, r, orgID) })).ServeHTTP(w, r)
			case http.MethodPost:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.AddMember(w, r, orgID) })).ServeHTTP(w, r)
			default:
				methodNotAllowed(w, r)
			}

		case len(rest) == 4 && rest[0] == "organizations" && rest[2] == "members":
			orgID := rest[1]
			userID := rest[3]
			switch r.Method {
			case http.MethodPatch:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.UpdateMember(w, r, orgID, userID) })).ServeHTTP(w, r)
			case http.MethodDelete:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.DeleteMember(w, r, orgID, userID) })).ServeHTTP(w, r)
			default:
				methodNotAllowed(w, r)
			}

		case len(rest) == 3 && rest[0] == "organizations" && rest[2] == "widgets":
			orgID := rest[1]
			switch r.Method {
			case http.MethodGet:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.ListWidgets(w, r, orgID) })).ServeHTTP(w, r)
			case http.MethodPost:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.CreateWidget(w, r, orgID) })).ServeHTTP(w, r)
			default:
				methodNotAllowed(w, r)
			}

		case len(rest) == 4 && rest[0] == "organizations" && rest[2] == "widgets":
			orgID := rest[1]
			widgetID := rest[3]
			if r.Method != http.MethodPatch {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { deps.Handlers.UpdateWidget(w, r, orgID, widgetID) })).ServeHTTP(w, r)

		case len(rest) == 2 && rest[0] == "widget-config":
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			deps.Handlers.WidgetConfig(w, r, rest[1])

		case len(rest) == 4 && rest[0] == "organizations" && rest[2] == "knowledge" && rest[3] == "text":
			orgID := rest[1]
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Knowledge.CreateText(w, r, orgID)
			})).ServeHTTP(w, r)

		case len(rest) == 4 && rest[0] == "organizations" && rest[2] == "knowledge" && rest[3] == "files":
			orgID := rest[1]
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Knowledge.CreateFile(w, r, orgID)
			})).ServeHTTP(w, r)

		case len(rest) == 3 && rest[0] == "organizations" && rest[2] == "knowledge":
			orgID := rest[1]
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Knowledge.List(w, r, orgID)
			})).ServeHTTP(w, r)

		case len(rest) == 4 && rest[0] == "organizations" && rest[2] == "knowledge":
			orgID, docID := rest[1], rest[3]
			switch r.Method {
			case http.MethodGet:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deps.Knowledge.Get(w, r, orgID, docID)
				})).ServeHTTP(w, r)
			case http.MethodPatch:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deps.Knowledge.Patch(w, r, orgID, docID)
				})).ServeHTTP(w, r)
			case http.MethodDelete:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deps.Knowledge.Delete(w, r, orgID, docID)
				})).ServeHTTP(w, r)
			default:
				methodNotAllowed(w, r)
			}

		case len(rest) == 5 && rest[0] == "organizations" && rest[2] == "knowledge" && rest[4] == "versions":
			orgID, docID := rest[1], rest[3]
			switch r.Method {
			case http.MethodGet:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deps.Knowledge.ListVersions(w, r, orgID, docID)
				})).ServeHTTP(w, r)
			case http.MethodPost:
				auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					deps.Knowledge.CreateVersion(w, r, orgID, docID)
				})).ServeHTTP(w, r)
			default:
				methodNotAllowed(w, r)
			}

		case len(rest) == 5 && rest[0] == "organizations" && rest[2] == "knowledge" && rest[4] == "status":
			orgID, docID := rest[1], rest[3]
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Knowledge.Status(w, r, orgID, docID)
			})).ServeHTTP(w, r)

		case len(rest) == 5 && rest[0] == "organizations" && rest[2] == "knowledge" && rest[4] == "reprocess":
			orgID, docID := rest[1], rest[3]
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Knowledge.Reprocess(w, r, orgID, docID)
			})).ServeHTTP(w, r)

		case len(rest) == 2 && rest[0] == "processing" && rest[1] == "jobs":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Processing.Submit(w, r)
			})).ServeHTTP(w, r)

		case len(rest) == 3 && rest[0] == "processing" && rest[1] == "jobs":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			jobID := rest[2]
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Processing.Get(w, r, jobID)
			})).ServeHTTP(w, r)

		case len(rest) == 4 && rest[0] == "processing" && rest[1] == "jobs" && rest[3] == "retry":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			jobID := rest[2]
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Processing.Retry(w, r, jobID)
			})).ServeHTTP(w, r)

		case len(rest) == 4 && rest[0] == "processing" && rest[1] == "jobs" && rest[3] == "cancel":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			jobID := rest[2]
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			auth.Middleware(deps.Auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deps.Processing.Cancel(w, r, jobID)
			})).ServeHTTP(w, r)

		case len(rest) == 2 && rest[0] == "processing" && rest[1] == "health":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			deps.Processing.Health(w, r)

		case len(rest) == 2 && rest[0] == "processing" && rest[1] == "ready":
			if deps.Processing == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "processing service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			if r.Method != http.MethodGet {
				methodNotAllowed(w, r)
				return
			}
			deps.Processing.Readiness(w, r)

		case len(rest) == 3 && rest[0] == "internal" && rest[1] == "retrieval" && rest[2] == "query":
			if deps.Retrieval == nil {
				api.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "retrieval service is not configured", api.CorrelationID(r.Context()), api.APIVersion)
				return
			}
			if r.Method != http.MethodPost {
				methodNotAllowed(w, r)
				return
			}
			deps.Retrieval.Query(w, r)

		default:
			api.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "The requested resource was not found.", api.CorrelationID(r.Context()), api.APIVersion)
		}
	})

	var handler http.Handler = root
	handler = api.RecoveryMiddleware(deps.Version, handler)
	handler = api.CorrelationMiddleware(handler)
	return handler
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	api.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The request method is not supported for this resource.", api.CorrelationID(r.Context()), api.APIVersion)
}

// isKnowledgePath reports whether the path targets the knowledge sub-resource.
func isKnowledgePath(segments []string) bool {
	rest := segments[2:]
	return len(rest) >= 3 && rest[0] == "organizations" && rest[2] == "knowledge"
}
