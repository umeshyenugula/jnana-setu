package identity

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type OrganizationStatus string

const (
	OrganizationActive     OrganizationStatus = "active"
	OrganizationSuspended OrganizationStatus = "suspended"
)

type Organization struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Slug          string             `json:"slug"`
	CreatorUserID string             `json:"creatorUserId"`
	Status        OrganizationStatus `json:"status"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

type Membership struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	UserID         string    `json:"userId"`
	Role           Role      `json:"role"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Widget struct {
	ID              string    `json:"id"`
	OrganizationID  string    `json:"organizationId"`
	PublicWidgetID  string    `json:"publicWidgetId"`
	DisplayName     string    `json:"displayName"`
	AllowedOrigins  []string  `json:"allowedOrigins"`
	Active          bool      `json:"active"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type TenantResolution struct {
	OrganizationID string
	WidgetID       string
	DisplayName    string
	Active         bool
}

type ServiceError struct {
	Code    string
	Message string
	Status  int
}

func (e *ServiceError) Error() string { return e.Message }
