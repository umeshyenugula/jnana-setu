package identity

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu              sync.Mutex
	organizations   map[string]Organization
	memberships     map[string]Membership // key org:user
	widgets         map[string]Widget     // key widget id
	publicWidgets   map[string]string    // public id -> widget id
	knownUsers      map[string]bool
	UserExistsFunc  func(context.Context, string) (bool, error)
	SlugExistsFunc  func(string) bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		organizations: map[string]Organization{},
		memberships:   map[string]Membership{},
		widgets:       map[string]Widget{},
		publicWidgets: map[string]string{},
		knownUsers:    map[string]bool{},
	}
}

func newID() string {
	b := make([]byte, 16)
	// math/rand is acceptable for non-secret identifiers in tests/dev storage.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	_, _ = rng.Read(b)
	return fmt.Sprintf("%x", b)
}

func (s *MemoryStore) SetKnownUsers(users ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.knownUsers == nil {
		s.knownUsers = map[string]bool{}
	}
	for _, u := range users {
		s.knownUsers[u] = true
	}
}

func (s *MemoryStore) userExists(ctx context.Context, userID string) (bool, error) {
	if s.UserExistsFunc != nil {
		return s.UserExistsFunc(ctx, userID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.knownUsers) == 0 {
		return true, nil
	}
	return s.knownUsers[userID], nil
}

func (s *MemoryStore) CreateOrganization(ctx context.Context, params CreateOrganizationParams) (Organization, Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, org := range s.organizations {
		if org.Slug == params.Slug {
			return Organization{}, Membership{}, &ServiceError{Code: "CONFLICT", Message: "organization slug already exists", Status: 409}
		}
	}
	now := time.Now().UTC()
	org := Organization{
		ID:            newID(),
		Name:          params.Name,
		Slug:          params.Slug,
		CreatorUserID: params.CreatorUserID,
		Status:        OrganizationActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	membership := Membership{
		ID:             newID(),
		OrganizationID: org.ID,
		UserID:         params.CreatorUserID,
		Role:           RoleOwner,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.organizations[org.ID] = org
	s.memberships[org.ID+":"+params.CreatorUserID] = membership
	return org, membership, nil
}

func (s *MemoryStore) GetOrganization(ctx context.Context, id string) (Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.organizations[id]
	if !ok {
		return Organization{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	return org, nil
}

func (s *MemoryStore) ListOrganizationsForUser(ctx context.Context, userID string) ([]Organization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Organization
	for _, m := range s.memberships {
		if m.UserID == userID {
			if org, ok := s.organizations[m.OrganizationID]; ok {
				result = append(result, org)
			}
		}
	}
	if result == nil {
		result = []Organization{}
	}
	return result, nil
}

func (s *MemoryStore) ListMembers(ctx context.Context, organizationID string) ([]Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.organizations[organizationID]; !ok {
		return nil, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	var result []Membership
	for _, m := range s.memberships {
		if m.OrganizationID == organizationID {
			result = append(result, m)
		}
	}
	if result == nil {
		result = []Membership{}
	}
	return result, nil
}

func (s *MemoryStore) GetMembership(ctx context.Context, organizationID, userID string) (Membership, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memberships[organizationID+":"+userID]
	return m, ok, nil
}

func (s *MemoryStore) CreateMembership(ctx context.Context, params CreateMembershipParams) (Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.organizations[params.OrganizationID]; !ok {
		return Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	if _, exists := s.memberships[params.OrganizationID+":"+params.UserID]; exists {
		return Membership{}, &ServiceError{Code: "CONFLICT", Message: "membership already exists", Status: 409}
	}
	now := time.Now().UTC()
	m := Membership{ID: newID(), OrganizationID: params.OrganizationID, UserID: params.UserID, Role: params.Role, CreatedAt: now, UpdatedAt: now}
	s.memberships[params.OrganizationID+":"+params.UserID] = m
	return m, nil
}

func (s *MemoryStore) UpdateMembership(ctx context.Context, organizationID, userID string, role Role) (Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memberships[organizationID+":"+userID]
	if !ok {
		return Membership{}, &ServiceError{Code: "NOT_FOUND", Message: "membership not found", Status: 404}
	}
	m.Role = role
	m.UpdatedAt = time.Now().UTC()
	s.memberships[organizationID+":"+userID] = m
	return m, nil
}

func (s *MemoryStore) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.memberships[organizationID+":"+userID]; !ok {
		return &ServiceError{Code: "NOT_FOUND", Message: "membership not found", Status: 404}
	}
	delete(s.memberships, organizationID+":"+userID)
	return nil
}

func (s *MemoryStore) CountOwners(ctx context.Context, organizationID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, m := range s.memberships {
		if m.OrganizationID == organizationID && m.Role == RoleOwner {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) CreateWidget(ctx context.Context, params CreateWidgetParams) (Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.organizations[params.OrganizationID]; !ok {
		return Widget{}, &ServiceError{Code: "NOT_FOUND", Message: "organization not found", Status: 404}
	}
	pid := newWidgetPublicID()
	w := Widget{
		ID:             newID(),
		OrganizationID: params.OrganizationID,
		PublicWidgetID: pid,
		DisplayName:    params.DisplayName,
		AllowedOrigins: params.AllowedOrigins,
		Active:         true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	if w.AllowedOrigins == nil {
		w.AllowedOrigins = []string{}
	}
	s.widgets[w.ID] = w
	s.publicWidgets[pid] = w.ID
	return w, nil
}

func (s *MemoryStore) GetWidget(ctx context.Context, organizationID, widgetID string) (Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.widgets[widgetID]
	if !ok || w.OrganizationID != organizationID {
		return Widget{}, &ServiceError{Code: "NOT_FOUND", Message: "widget not found", Status: 404}
	}
	return w, nil
}

func (s *MemoryStore) ListWidgets(ctx context.Context, organizationID string) ([]Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Widget
	for _, w := range s.widgets {
		if w.OrganizationID == organizationID {
			result = append(result, w)
		}
	}
	if result == nil {
		result = []Widget{}
	}
	return result, nil
}

func (s *MemoryStore) UpdateWidget(ctx context.Context, organizationID, widgetID string, params UpdateWidgetParams) (Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.widgets[widgetID]
	if !ok || w.OrganizationID != organizationID {
		return Widget{}, &ServiceError{Code: "NOT_FOUND", Message: "widget not found", Status: 404}
	}
	if params.DisplayName != nil {
		w.DisplayName = *params.DisplayName
	}
	if params.AllowedOrigins != nil {
		w.AllowedOrigins = *params.AllowedOrigins
	}
	if params.Active != nil {
		w.Active = *params.Active
	}
	w.UpdatedAt = time.Now().UTC()
	s.widgets[widgetID] = w
	return w, nil
}

func (s *MemoryStore) GetWidgetByPublicID(ctx context.Context, publicWidgetID string) (Widget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.publicWidgets[publicWidgetID]
	if !ok {
		return Widget{}, &ServiceError{Code: "NOT_FOUND", Message: "widget not found", Status: 404}
	}
	w := s.widgets[id]
	return w, nil
}

func (s *MemoryStore) ResolveTenant(ctx context.Context, publicWidgetID string) (TenantResolution, error) {
	w, err := s.GetWidgetByPublicID(ctx, publicWidgetID)
	if err != nil {
		return TenantResolution{}, err
	}
	if !w.Active {
		return TenantResolution{}, &ServiceError{Code: "NOT_FOUND", Message: "widget not available", Status: 404}
	}
	return TenantResolution{OrganizationID: w.OrganizationID, WidgetID: w.ID, DisplayName: w.DisplayName, Active: w.Active}, nil
}

func (s *MemoryStore) UserExists(ctx context.Context, userID string) (bool, error) {
	return s.userExists(ctx, userID)
}

func newWidgetPublicID() string { return "wdg_" + strings.TrimLeft(newID(), "0")[:24] }
