package identity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateOrganizationOwner(t *testing.T) {
	store := NewMemoryStore()
	store.SetKnownUsers("user-1")
	service := NewService(store)

	org, membership, err := service.CreateOrganization(context.Background(), "user-1", "Acme", "acme")
	require.NoError(t, err)
	require.Equal(t, "acme", org.Slug)
	require.Equal(t, RoleOwner, membership.Role)
	require.Equal(t, org.ID, membership.OrganizationID)
}

func TestDuplicateSlug(t *testing.T) {
	store := NewMemoryStore()
	store.SetKnownUsers("user-1", "user-2")
	service := NewService(store)

	_, _, err := service.CreateOrganization(context.Background(), "user-1", "Acme", "acme")
	require.NoError(t, err)
	_, _, err = service.CreateOrganization(context.Background(), "user-2", "Also Acme", "acme")
	require.Error(t, err)
}

func TestCrossTenantDenial(t *testing.T) {
	store := NewMemoryStore()
	store.SetKnownUsers("owner", "stranger")
	service := NewService(store)

	org, _, err := service.CreateOrganization(context.Background(), "owner", "Acme", "acme")
	require.NoError(t, err)

	_, err = service.GetOrganization(context.Background(), "stranger", org.ID)
	require.Error(t, err)
}

func TestLastOwnerProtection(t *testing.T) {
	store := NewMemoryStore()
	store.SetKnownUsers("owner", "admin-user")
	service := NewService(store)

	org, _, err := service.CreateOrganization(context.Background(), "owner", "Acme", "acme")
	require.NoError(t, err)

	_, err = service.AddMember(context.Background(), "owner", org.ID, "admin-user", RoleAdmin)
	require.NoError(t, err)
	_, err = service.UpdateMember(context.Background(), "owner", org.ID, "owner", RoleAdmin)
	require.Error(t, err)
	require.NoError(t, service.DeleteMember(context.Background(), "owner", org.ID, "admin-user"))
}

func TestWidgetTenantResolution(t *testing.T) {
	store := NewMemoryStore()
	store.SetKnownUsers("owner")
	service := NewService(store)

	org, _, err := service.CreateOrganization(context.Background(), "owner", "Acme", "acme")
	require.NoError(t, err)

	widget, err := service.CreateWidget(context.Background(), "owner", org.ID, CreateWidgetParams{
		DisplayName:    "Chat",
		AllowedOrigins: []string{"https://example.com"},
	})
	require.NoError(t, err)
	require.True(t, widget.Active)

	tenant, err := service.ResolveTenant(context.Background(), widget.PublicWidgetID)
	require.NoError(t, err)
	require.Equal(t, org.ID, tenant.OrganizationID)

	_, _ = service.UpdateWidget(context.Background(), "owner", org.ID, widget.ID, UpdateWidgetParams{Active: boolPtr(false)})
	_, err = service.ResolveTenant(context.Background(), widget.PublicWidgetID)
	require.Error(t, err)
}

func boolPtr(v bool) *bool { return &v }
