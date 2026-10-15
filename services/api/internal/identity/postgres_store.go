package identity

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 5*time.Second)
}

func (s *PostgresStore) UserExists(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var exists bool
	err := s.pool.QueryRow(ctx, `select exists(select 1 from auth.users where id = $1)`, userID).Scan(&exists)
	return exists, err
}

func (s *PostgresStore) CreateOrganization(ctx context.Context, params CreateOrganizationParams) (Organization, Membership, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Organization{}, Membership{}, err
	}
	defer tx.Rollback(ctx)
	var org Organization
	err = tx.QueryRow(ctx, `
		insert into public.organizations (name, slug, creator_user_id, status)
		values ($1, $2, $3, 'active')
		returning id, name, slug, creator_user_id, status, created_at, updated_at`,
		params.Name, params.Slug, params.CreatorUserID).Scan(&org.ID, &org.Name, &org.Slug, &org.CreatorUserID, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return Organization{}, Membership{}, err
	}
	var m Membership
	err = tx.QueryRow(ctx, `
		insert into public.organization_memberships (organization_id, user_id, role)
		values ($1, $2, 'owner')
		returning id, organization_id, user_id, role, created_at, updated_at`,
		org.ID, params.CreatorUserID).Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Organization{}, Membership{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Organization{}, Membership{}, err
	}
	return org, m, nil
}

func (s *PostgresStore) GetOrganization(ctx context.Context, id string) (Organization, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var org Organization
	err := s.pool.QueryRow(ctx, `
		select id, name, slug, creator_user_id, status, created_at, updated_at
		from public.organizations where id = $1`, id).Scan(&org.ID, &org.Name, &org.Slug, &org.CreatorUserID, &org.Status, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		return Organization{}, notFoundOr(err, "organization not found")
	}
	return org, nil
}

func (s *PostgresStore) ListOrganizationsForUser(ctx context.Context, userID string) ([]Organization, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select o.id, o.name, o.slug, o.creator_user_id, o.status, o.created_at, o.updated_at
		from public.organizations o
		join public.organization_memberships m on m.organization_id = o.id
		where m.user_id = $1 order by o.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.CreatorUserID, &org.Status, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, org)
	}
	if result == nil {
		result = []Organization{}
	}
	return result, rows.Err()
}

func (s *PostgresStore) ListMembers(ctx context.Context, organizationID string) ([]Membership, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select id, organization_id, user_id, role, created_at, updated_at
		from public.organization_memberships where organization_id = $1 order by created_at`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	if result == nil {
		result = []Membership{}
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMembership(ctx context.Context, organizationID, userID string) (Membership, bool, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var m Membership
	err := s.pool.QueryRow(ctx, `
		select id, organization_id, user_id, role, created_at, updated_at
		from public.organization_memberships
		where organization_id = $1 and user_id = $2`, organizationID, userID).Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Membership{}, false, nil
	}
	return m, true, nil
}

func (s *PostgresStore) CreateMembership(ctx context.Context, params CreateMembershipParams) (Membership, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var m Membership
	err := s.pool.QueryRow(ctx, `
		insert into public.organization_memberships (organization_id, user_id, role)
		values ($1, $2, $3)
		returning id, organization_id, user_id, role, created_at, updated_at`,
		params.OrganizationID, params.UserID, params.Role).Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (s *PostgresStore) UpdateMembership(ctx context.Context, organizationID, userID string, role Role) (Membership, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var m Membership
	err := s.pool.QueryRow(ctx, `
		update public.organization_memberships
		set role = $3, updated_at = now()
		where organization_id = $1 and user_id = $2
		returning id, organization_id, user_id, role, created_at, updated_at`,
		organizationID, userID, role).Scan(&m.ID, &m.OrganizationID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return Membership{}, notFoundOr(err, "membership not found")
	}
	return m, nil
}

func (s *PostgresStore) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	cmd, err := s.pool.Exec(ctx, `delete from public.organization_memberships where organization_id=$1 and user_id=$2`, organizationID, userID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return &ServiceError{Code: "NOT_FOUND", Message: "membership not found", Status: 404}
	}
	return nil
}

func (s *PostgresStore) CountOwners(ctx context.Context, organizationID string) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	var count int
	err := s.pool.QueryRow(ctx, `select count(*) from public.organization_memberships where organization_id=$1 and role='owner'`, organizationID).Scan(&count)
	return count, err
}

func (s *PostgresStore) CreateWidget(ctx context.Context, params CreateWidgetParams) (Widget, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	origins, _ := json.Marshal(params.AllowedOrigins)
	if params.AllowedOrigins == nil {
		origins = []byte("[]")
	}
	publicID := newID()
	// Keep a readable prefix for operators without encoding a secret.
	publicID = "wdg_" + publicID
	var w Widget
	err := s.pool.QueryRow(ctx, `
		insert into public.website_widgets
		(organization_id, public_widget_id, display_name, allowed_origins, active)
		values ($1, $2, $3, $4::jsonb, true)
		returning id, organization_id, public_widget_id, display_name, allowed_origins, active, created_at, updated_at`,
		params.OrganizationID, publicID, params.DisplayName, origins).Scan(
		&w.ID, &w.OrganizationID, &w.PublicWidgetID, &w.DisplayName, &origins, &w.Active, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Widget{}, err
	}
	_ = json.Unmarshal(origins, &w.AllowedOrigins)
	if w.AllowedOrigins == nil {
		w.AllowedOrigins = []string{}
	}
	return w, nil
}

func (s *PostgresStore) GetWidget(ctx context.Context, organizationID, widgetID string) (Widget, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return s.scanWidget(ctx, s.pool.QueryRow(ctx, `
		select id, organization_id, public_widget_id, display_name, allowed_origins, active, created_at, updated_at
		from public.website_widgets where id=$1 and organization_id=$2`, widgetID, organizationID))
}

func (s *PostgresStore) ListWidgets(ctx context.Context, organizationID string) ([]Widget, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	rows, err := s.pool.Query(ctx, `
		select id, organization_id, public_widget_id, display_name, allowed_origins, active, created_at, updated_at
		from public.website_widgets where organization_id=$1 order by created_at`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Widget
	for rows.Next() {
		w, err := scanWidgetRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	if result == nil {
		result = []Widget{}
	}
	return result, rows.Err()
}

func (s *PostgresStore) UpdateWidget(ctx context.Context, organizationID, widgetID string, params UpdateWidgetParams) (Widget, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	current, err := s.GetWidget(ctx, organizationID, widgetID)
	if err != nil {
		return Widget{}, err
	}
	if params.DisplayName != nil {
		current.DisplayName = *params.DisplayName
	}
	if params.AllowedOrigins != nil {
		current.AllowedOrigins = *params.AllowedOrigins
	}
	if params.Active != nil {
		current.Active = *params.Active
	}
	origins, _ := json.Marshal(current.AllowedOrigins)
	var w Widget
	err = s.pool.QueryRow(ctx, `
		update public.website_widgets
		set display_name=$3, allowed_origins=$4::jsonb, active=$5, updated_at=now()
		where id=$1 and organization_id=$2
		returning id, organization_id, public_widget_id, display_name, allowed_origins, active, created_at, updated_at`,
		widgetID, organizationID, current.DisplayName, origins, current.Active).Scan(
		&w.ID, &w.OrganizationID, &w.PublicWidgetID, &w.DisplayName, &origins, &w.Active, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Widget{}, notFoundOr(err, "widget not found")
	}
	_ = json.Unmarshal(origins, &w.AllowedOrigins)
	if w.AllowedOrigins == nil {
		w.AllowedOrigins = []string{}
	}
	return w, nil
}

func (s *PostgresStore) GetWidgetByPublicID(ctx context.Context, publicWidgetID string) (Widget, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	return s.scanWidget(ctx, s.pool.QueryRow(ctx, `
		select id, organization_id, public_widget_id, display_name, allowed_origins, active, created_at, updated_at
		from public.website_widgets where public_widget_id=$1`, publicWidgetID))
}

func (s *PostgresStore) ResolveTenant(ctx context.Context, publicWidgetID string) (TenantResolution, error) {
	w, err := s.GetWidgetByPublicID(ctx, publicWidgetID)
	if err != nil {
		return TenantResolution{}, err
	}
	if !w.Active {
		return TenantResolution{}, &ServiceError{Code: "NOT_FOUND", Message: "widget not available", Status: 404}
	}
	return TenantResolution{OrganizationID: w.OrganizationID, WidgetID: w.ID, DisplayName: w.DisplayName, Active: w.Active}, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func (s *PostgresStore) scanWidget(ctx context.Context, row rowScanner) (Widget, error) {
	var w Widget
	var origins []byte
	err := row.Scan(&w.ID, &w.OrganizationID, &w.PublicWidgetID, &w.DisplayName, &origins, &w.Active, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Widget{}, notFoundOr(err, "widget not found")
	}
	_ = json.Unmarshal(origins, &w.AllowedOrigins)
	if w.AllowedOrigins == nil {
		w.AllowedOrigins = []string{}
	}
	return w, nil
}

func scanWidgetRow(rows interface{ Scan(dest ...any) error }) (Widget, error) {
	var w Widget
	var origins []byte
	err := rows.Scan(&w.ID, &w.OrganizationID, &w.PublicWidgetID, &w.DisplayName, &origins, &w.Active, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Widget{}, err
	}
	_ = json.Unmarshal(origins, &w.AllowedOrigins)
	if w.AllowedOrigins == nil {
		w.AllowedOrigins = []string{}
	}
	return w, nil
}

func notFoundOr(err error, message string) error {
	// pgx returns no rows error; map all query failures on single-row lookups to not found conservatively.
	return &ServiceError{Code: "NOT_FOUND", Message: message, Status: 404}
}
