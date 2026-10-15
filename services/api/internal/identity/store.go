package identity

import "context"

type Store interface {
	UserExists(ctx context.Context, userID string) (bool, error)
	CreateOrganization(ctx context.Context, params CreateOrganizationParams) (Organization, Membership, error)
	GetOrganization(ctx context.Context, id string) (Organization, error)
	ListOrganizationsForUser(ctx context.Context, userID string) ([]Organization, error)
	ListMembers(ctx context.Context, organizationID string) ([]Membership, error)
	GetMembership(ctx context.Context, organizationID, userID string) (Membership, bool, error)
	CreateMembership(ctx context.Context, params CreateMembershipParams) (Membership, error)
	UpdateMembership(ctx context.Context, organizationID, userID string, role Role) (Membership, error)
	DeleteMembership(ctx context.Context, organizationID, userID string) error
	CountOwners(ctx context.Context, organizationID string) (int, error)
	CreateWidget(ctx context.Context, params CreateWidgetParams) (Widget, error)
	GetWidget(ctx context.Context, organizationID, widgetID string) (Widget, error)
	ListWidgets(ctx context.Context, organizationID string) ([]Widget, error)
	UpdateWidget(ctx context.Context, organizationID, widgetID string, params UpdateWidgetParams) (Widget, error)
	GetWidgetByPublicID(ctx context.Context, publicWidgetID string) (Widget, error)
	ResolveTenant(ctx context.Context, publicWidgetID string) (TenantResolution, error)
}

type CreateOrganizationParams struct {
	Name          string
	Slug          string
	CreatorUserID string
}

type CreateMembershipParams struct {
	OrganizationID string
	UserID         string
	Role           Role
}

type CreateWidgetParams struct {
	OrganizationID string
	DisplayName    string
	AllowedOrigins []string
}

type UpdateWidgetParams struct {
	DisplayName    *string
	AllowedOrigins *[]string
	Active         *bool
}
