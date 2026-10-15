package identity

import (
	"context"
	"net"
	"net/url"
	"regexp"
	"strings"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service { return &Service{store: store} }

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validSlug(slug string) bool {
	return slugRe.MatchString(slug) && len(slug) <= 64
}

func validName(name string) bool {
	name = strings.TrimSpace(name)
	return len(name) >= 1 && len(name) <= 120
}

func roleRank(role Role) int {
	switch role {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleMember:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

func validRole(role Role) bool { return roleRank(role) > 0 }

func (s *Service) CreateOrganization(ctx context.Context, creatorUserID, name, slug string) (Organization, Membership, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(strings.ToLower(slug))
	if !validName(name) {
		return Organization{}, Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid organization name", Status: 400}
	}
	if !validSlug(slug) {
		return Organization{}, Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid organization slug", Status: 400}
	}
	exists, err := s.store.UserExists(ctx, creatorUserID)
	if err != nil {
		return Organization{}, Membership{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify user", Status: 500}
	}
	if !exists {
		return Organization{}, Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "creator does not exist", Status: 400}
	}
	org, membership, err := s.store.CreateOrganization(ctx, CreateOrganizationParams{Name: name, Slug: slug, CreatorUserID: creatorUserID})
	if err != nil {
		return Organization{}, Membership{}, err
	}
	return org, membership, nil
}

func (s *Service) GetOrganization(ctx context.Context, actorUserID, orgID string) (Organization, error) {
	membership, err := s.requireMembership(ctx, orgID, actorUserID)
	if err != nil {
		return Organization{}, err
	}
	_ = membership
	org, err := s.store.GetOrganization(ctx, orgID)
	if err != nil {
		return Organization{}, err
	}
	return org, nil
}

func (s *Service) ListOrganizations(ctx context.Context, userID string) ([]Organization, error) {
	return s.store.ListOrganizationsForUser(ctx, userID)
}

func (s *Service) ListMembers(ctx context.Context, actorUserID, orgID string) ([]Membership, error) {
	if _, err := s.requireMembership(ctx, orgID, actorUserID); err != nil {
		return nil, err
	}
	return s.store.ListMembers(ctx, orgID)
}

func (s *Service) AddMember(ctx context.Context, actorUserID, orgID, targetUserID string, role Role) (Membership, error) {
	actor, err := s.requireMembership(ctx, orgID, actorUserID)
	if err != nil {
		return Membership{}, err
	}
	if roleRank(actor.Role) < roleRank(RoleAdmin) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	if !validRole(role) {
		return Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid role", Status: 400}
	}
	if roleRank(role) > roleRank(actor.Role) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "cannot assign a role higher than your own", Status: 403}
	}
	if targetUserID == actorUserID {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "cannot modify your own membership", Status: 403}
	}
	exists, err := s.store.UserExists(ctx, targetUserID)
	if err != nil {
		return Membership{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify user", Status: 500}
	}
	if !exists {
		return Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "target user does not exist", Status: 400}
	}
	_, existsMembership, err := s.store.GetMembership(ctx, orgID, targetUserID)
	if err != nil {
		return Membership{}, err
	}
	if existsMembership {
		return Membership{}, &ServiceError{Code: "CONFLICT", Message: "membership already exists", Status: 409}
	}
	return s.store.CreateMembership(ctx, CreateMembershipParams{OrganizationID: orgID, UserID: targetUserID, Role: role})
}

func (s *Service) UpdateMember(ctx context.Context, actorUserID, orgID, targetUserID string, role Role) (Membership, error) {
	actor, err := s.requireMembership(ctx, orgID, actorUserID)
	if err != nil {
		return Membership{}, err
	}
	if roleRank(actor.Role) < roleRank(RoleAdmin) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	if !validRole(role) {
		return Membership{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid role", Status: 400}
	}
	target, ok, err := s.store.GetMembership(ctx, orgID, targetUserID)
	if err != nil {
		return Membership{}, err
	}
	if !ok {
		return Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "membership not found", Status: 404}
	}
	if target.UserID == actorUserID {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "cannot modify your own membership", Status: 403}
	}
	if roleRank(target.Role) >= roleRank(RoleOwner) && roleRank(actor.Role) < roleRank(RoleOwner) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "only owners can modify owner memberships", Status: 403}
	}
	if roleRank(role) > roleRank(actor.Role) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "cannot assign a role higher than your own", Status: 403}
	}
	if target.Role == RoleOwner && role != RoleOwner {
		count, err := s.store.CountOwners(ctx, orgID)
		if err != nil {
			return Membership{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify owners", Status: 500}
		}
		if count <= 1 {
			return Membership{}, &ServiceError{Code: "CONFLICT", Message: "cannot remove the last owner", Status: 409}
		}
	}
	return s.store.UpdateMembership(ctx, orgID, targetUserID, role)
}

func (s *Service) DeleteMember(ctx context.Context, actorUserID, orgID, targetUserID string) error {
	actor, err := s.requireMembership(ctx, orgID, actorUserID)
	if err != nil {
		return err
	}
	if roleRank(actor.Role) < roleRank(RoleAdmin) {
		return &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	target, ok, err := s.store.GetMembership(ctx, orgID, targetUserID)
	if err != nil {
		return err
	}
	if !ok {
		return &ServiceError{Code: "NOT_FOUND", Message: "membership not found", Status: 404}
	}
	if targetUserID == actorUserID {
		return &ServiceError{Code: "FORBIDDEN", Message: "cannot remove your own membership through this endpoint", Status: 403}
	}
	if target.Role == RoleOwner && roleRank(actor.Role) < roleRank(RoleOwner) {
		return &ServiceError{Code: "FORBIDDEN", Message: "only owners can remove owners", Status: 403}
	}
	if target.Role == RoleOwner {
		count, err := s.store.CountOwners(ctx, orgID)
		if err != nil {
			return &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify owners", Status: 500}
		}
		if count <= 1 {
			return &ServiceError{Code: "CONFLICT", Message: "cannot remove the last owner", Status: 409}
		}
	}
	return s.store.DeleteMembership(ctx, orgID, targetUserID)
}

func (s *Service) CreateWidget(ctx context.Context, actorUserID, orgID string, params CreateWidgetParams) (Widget, error) {
	if _, err := s.requireRole(ctx, orgID, actorUserID, RoleAdmin); err != nil {
		return Widget{}, err
	}
	params.DisplayName = strings.TrimSpace(params.DisplayName)
	if !validName(params.DisplayName) {
		return Widget{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid display name", Status: 400}
	}
	if err := validateOrigins(params.AllowedOrigins); err != nil {
		return Widget{}, err
	}
	params.OrganizationID = orgID
	return s.store.CreateWidget(ctx, params)
}

func (s *Service) ListWidgets(ctx context.Context, actorUserID, orgID string) ([]Widget, error) {
	if _, err := s.requireMembership(ctx, orgID, actorUserID); err != nil {
		return nil, err
	}
	return s.store.ListWidgets(ctx, orgID)
}

func (s *Service) UpdateWidget(ctx context.Context, actorUserID, orgID, widgetID string, params UpdateWidgetParams) (Widget, error) {
	if _, err := s.requireRole(ctx, orgID, actorUserID, RoleAdmin); err != nil {
		return Widget{}, err
	}
	if params.DisplayName != nil {
		name := strings.TrimSpace(*params.DisplayName)
		if !validName(name) {
			return Widget{}, &ServiceError{Code: "INVALID_REQUEST", Message: "invalid display name", Status: 400}
		}
		params.DisplayName = &name
	}
	if params.AllowedOrigins != nil {
		if err := validateOrigins(*params.AllowedOrigins); err != nil {
			return Widget{}, err
		}
	}
	return s.store.UpdateWidget(ctx, orgID, widgetID, params)
}

func (s *Service) WidgetConfig(ctx context.Context, publicWidgetID string) (map[string]any, error) {
	tenant, err := s.store.ResolveTenant(ctx, publicWidgetID)
	if err != nil {
		return nil, err
	}
	widget, err := s.store.GetWidgetByPublicID(ctx, publicWidgetID)
	if err != nil {
		return nil, err
	}
	if !widget.Active {
		return nil, &ServiceError{Code: "NOT_FOUND", Message: "widget not available", Status: 404}
	}
	_ = tenant
	return map[string]any{
		"publicWidgetId": widget.PublicWidgetID,
		"displayName":    widget.DisplayName,
		"allowedOrigins": widget.AllowedOrigins,
		"active":         widget.Active,
	}, nil
}

func (s *Service) ResolveTenant(ctx context.Context, publicWidgetID string) (TenantResolution, error) {
	return s.store.ResolveTenant(ctx, publicWidgetID)
}

func (s *Service) requireMembership(ctx context.Context, orgID, userID string) (Membership, error) {
	m, ok, err := s.store.GetMembership(ctx, orgID, userID)
	if err != nil {
		return Membership{}, &ServiceError{Code: "INTERNAL_ERROR", Message: "could not verify membership", Status: 500}
	}
	if !ok {
		return Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	org, err := s.store.GetOrganization(ctx, orgID)
	if err != nil {
		return Membership{}, err
	}
	if org.Status != OrganizationActive {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "organization is not active", Status: 403}
	}
	return m, nil
}

func (s *Service) requireRole(ctx context.Context, orgID, userID string, min Role) (Membership, error) {
	m, err := s.requireMembership(ctx, orgID, userID)
	if err != nil {
		return Membership{}, err
	}
	if roleRank(m.Role) < roleRank(min) {
		return Membership{}, &ServiceError{Code: "FORBIDDEN", Message: "insufficient permissions", Status: 403}
	}
	return m, nil
}

func validateOrigins(origins []string) error {
	if len(origins) == 0 {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "at least one allowed origin is required", Status: 400}
	}
	if len(origins) > 20 {
		return &ServiceError{Code: "INVALID_REQUEST", Message: "too many allowed origins", Status: 400}
	}
	seen := map[string]bool{}
	for _, raw := range origins {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "invalid website origin", Status: 400}
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "website origin must use http or https", Status: 400}
		}
		if u.Path != "" && u.Path != "/" {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "website origin must not include a path", Status: 400}
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "website origin must not include query or fragment", Status: 400}
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "invalid website origin host", Status: 400}
		}
		key := u.Scheme + "://" + u.Host
		if seen[key] {
			return &ServiceError{Code: "INVALID_REQUEST", Message: "duplicate website origin", Status: 400}
		}
		seen[key] = true
	}
	return nil
}
