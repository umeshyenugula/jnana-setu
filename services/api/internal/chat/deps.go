package chat

import (
	"context"

	"example.com/ai-knowledge-assistant/services/api/internal/identity"
	"example.com/ai-knowledge-assistant/services/api/internal/inference"
	"example.com/ai-knowledge-assistant/services/api/internal/retrieval"
)

// TenantResolver is the documented Module 1 interface Module 6 resolves
// tenants through. Satisfied by *identity.Service via IdentityAdapter; tests
// substitute a stub. Module 6 never reads Module 1 tables directly and never
// trusts a browser-supplied tenant identifier.
type TenantResolver interface {
	// ResolveWidget validates that the widget exists and is active, and
	// returns the bound tenant plus the configured origin allow-list.
	ResolveWidget(ctx context.Context, publicWidgetID string) (WidgetAccess, error)
}

// WidgetAccess is the trusted tenant context for one chat request.
type WidgetAccess struct {
	OrganizationID string
	WidgetID       string
	DisplayName    string
	Active         bool
	AllowedOrigins []string
}

// IdentityAdapter binds Module 6 to Module 1's public service methods only
// (ResolveTenant + WidgetConfig). No Module 1 internals are touched.
type IdentityAdapter struct {
	Service *identity.Service
}

func (a *IdentityAdapter) ResolveWidget(ctx context.Context, publicWidgetID string) (WidgetAccess, error) {
	tenant, err := a.Service.ResolveTenant(ctx, publicWidgetID)
	if err != nil {
		return WidgetAccess{}, err
	}
	cfg, err := a.Service.WidgetConfig(ctx, publicWidgetID)
	if err != nil {
		return WidgetAccess{}, err
	}
	origins, _ := cfg["allowedOrigins"].([]string)
	if origins == nil {
		if raw, ok := cfg["allowedOrigins"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					origins = append(origins, s)
				}
			}
		}
	}
	name, _ := cfg["displayName"].(string)
	return WidgetAccess{
		OrganizationID: tenant.OrganizationID,
		WidgetID:       tenant.WidgetID,
		DisplayName:    name,
		Active:         tenant.Active,
		AllowedOrigins: origins,
	}, nil
}

// Retriever is the documented Module 4 contract. Satisfied by
// *retrieval.Service; tests substitute a stub.
type Retriever interface {
	Retrieve(ctx context.Context, q retrieval.Query) (retrieval.Result, error)
}

// Generator is the documented Module 5 contract. Satisfied by
// *inference.Service; tests substitute a stub.
type Generator interface {
	Generate(ctx context.Context, req inference.GenerationRequest) (inference.GenerationResult, error)
}
