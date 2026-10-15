-- Module 1: Identity and tenant management.
-- Supabase Auth owns passwords and user records in auth.users.

create extension if not exists pgcrypto;

create table if not exists public.organizations (
    id uuid primary key default gen_random_uuid(),
    name text not null check (char_length(name) between 1 and 120),
    slug text not null unique check (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$' and char_length(slug) <= 64),
    creator_user_id uuid not null references auth.users(id) on delete restrict,
    status text not null check (status in ('active', 'suspended')),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists public.organization_memberships (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    user_id uuid not null references auth.users(id) on delete cascade,
    role text not null check (role in ('owner', 'admin', 'member', 'viewer')),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (organization_id, user_id)
);

create table if not exists public.website_widgets (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    public_widget_id text not null unique,
    display_name text not null check (char_length(display_name) between 1 and 120),
    allowed_origins jsonb not null default '[]'::jsonb,
    active boolean not null default true,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (jsonb_typeof(allowed_origins) = 'array')
);

create index if not exists organizations_status_idx on public.organizations(status);
create index if not exists organization_memberships_user_idx on public.organization_memberships(user_id);
create index if not exists organization_memberships_org_role_idx on public.organization_memberships(organization_id, role);
create index if not exists website_widgets_org_idx on public.website_widgets(organization_id);
create index if not exists website_widgets_public_id_idx on public.website_widgets(public_widget_id);

-- Keep updated_at current. The function is generic and safe for these tables.
create or replace function public.set_updated_at()
returns trigger
language plpgsql
as $$
begin
    new.updated_at = now();
    return new;
end;
$$;

drop trigger if exists organizations_set_updated_at on public.organizations;
create trigger organizations_set_updated_at
before update on public.organizations
for each row execute function public.set_updated_at();

drop trigger if exists organization_memberships_set_updated_at on public.organization_memberships;
create trigger organization_memberships_set_updated_at
before update on public.organization_memberships
for each row execute function public.set_updated_at();

drop trigger if exists website_widgets_set_updated_at on public.website_widgets;
create trigger website_widgets_set_updated_at
before update on public.website_widgets
for each row execute function public.set_updated_at();

alter table public.organizations enable row level security;
alter table public.organization_memberships enable row level security;
alter table public.website_widgets enable row level security;

-- Direct client reads are limited to rows visible through active membership.
create policy organizations_select_member on public.organizations
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = organizations.id
          and m.user_id = auth.uid()
    )
);

create policy organization_memberships_select_member on public.organization_memberships
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships self
        where self.organization_id = organization_memberships.organization_id
          and self.user_id = auth.uid()
    )
);

create policy website_widgets_select_member on public.website_widgets
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = website_widgets.organization_id
          and m.user_id = auth.uid()
    )
);

-- Writes are performed by the Go API using the privileged service-role database identity.
-- service_role bypasses RLS by design; all authorization for those operations is enforced in Go.
grant usage on schema public to anon, authenticated, service_role;
grant select on public.organizations, public.organization_memberships, public.website_widgets to authenticated;
grant all on public.organizations, public.organization_memberships, public.website_widgets to service_role;
