-- Module 8: durable operational audit records.
-- Business modules own their events; this table is the shared durable sink
-- (see internal/observability/audit.go). Details carry identifiers, counts,
-- and outcomes only — never customer content, prompts, or credentials.
-- High-volume request logs do NOT belong here; use structured logs and
-- Prometheus for those.

create table if not exists public.operational_events (
    id uuid primary key default gen_random_uuid(),
    service text not null check (char_length(service) between 1 and 64),
    name text not null check (char_length(name) between 1 and 128),
    correlation_id text null check (correlation_id is null or char_length(correlation_id) <= 128),
    organization_id uuid null references public.organizations(id) on delete set null,
    actor text null check (actor is null or char_length(actor) <= 256),
    outcome text not null check (outcome in ('ok', 'error')),
    detail jsonb not null default '{}'::jsonb,
    occurred_at timestamptz not null default now(),
    check (jsonb_typeof(detail) = 'object')
);

create index if not exists operational_events_name_time_idx
    on public.operational_events(name, occurred_at desc);
create index if not exists operational_events_org_time_idx
    on public.operational_events(organization_id, occurred_at desc);

alter table public.operational_events enable row level security;

-- Audit access is service-role only. No browser role may read or write
-- audit records; operator access goes through the database, not the API.
revoke all on public.operational_events from public, anon, authenticated;
grant all on public.operational_events to service_role;

-- Retention: operational events are operator data, not customer content.
-- Keep 180 days by default; adjust per deployment policy:
--   delete from public.operational_events
--   where occurred_at < now() - make_interval(days => 180);
