-- Module 2: Knowledge management.
-- Version-storage strategy (explicit):
--   Original bytes for EVERY version (uploaded files AND submitted text) are
--   retained verbatim as objects in the private bucket `knowledge-originals`.
--   Text submissions are stored as UTF-8 objects (<version>.txt) in the same
--   bucket so arbitrarily large document text NEVER lives in metadata columns.
--   Postgres stores only metadata + storage_path pointers.
--   Object layout (server-generated, never from client filenames):
--     org/<organization_id>/doc/<document_id>/v<version_number>/<randomhex>[-<safe-ext>]
--   The original client filename is preserved in metadata only.

create extension if not exists pgcrypto;

-- ---------------------------------------------------------------- documents
create table if not exists public.knowledge_documents (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    creator_user_id uuid not null references auth.users(id) on delete restrict,
    title text not null check (char_length(title) between 1 and 200),
    source_type text not null check (source_type in ('text', 'file')),
    current_version_id uuid null,
    is_active boolean not null default true,
    processing_status text not null default 'pending'
        check (processing_status in ('pending', 'processing', 'ready', 'failed')),
    deleted_at timestamptz null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (deleted_at is null or is_active = false)
);

-- ---------------------------------------------------------------- versions
create table if not exists public.knowledge_document_versions (
    id uuid primary key default gen_random_uuid(),
    document_id uuid not null references public.knowledge_documents(id) on delete cascade,
    version_number integer not null check (version_number > 0),
    content_type text not null check (char_length(content_type) between 1 and 127),
    original_filename text null check (original_filename is null or char_length(original_filename) between 1 and 255),
    storage_path text not null check (char_length(storage_path) between 1 and 1024),
    content_size bigint not null check (content_size >= 0),
    checksum_sha256 text null check (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    creator_user_id uuid not null references auth.users(id) on delete restrict,
    created_at timestamptz not null default now(),
    unique (document_id, version_number),
    unique (storage_path)
);

-- current_version_id references versions(id); added after versions exists.
do $$
begin
    if not exists (
        select 1 from pg_constraint where conname = 'knowledge_documents_current_version_fk'
    ) then
        alter table public.knowledge_documents
            add constraint knowledge_documents_current_version_fk
            foreign key (current_version_id)
            references public.knowledge_document_versions(id)
            on delete set null
            deferrable initially deferred;
    end if;
end
$$;

-- ------------------------------------------------- processing outbox (M3)
-- Retry-safe handoff: document/version mutations and their processing request
-- are committed atomically in one transaction. A relay (Module 3 worker or a
-- future dispatcher) reads pending rows, delivers them, then marks dispatched.
create table if not exists public.knowledge_processing_outbox (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    document_id uuid not null references public.knowledge_documents(id) on delete cascade,
    version_id uuid not null references public.knowledge_document_versions(id) on delete cascade,
    operation text not null check (operation in ('index', 'reindex', 'delete')),
    idempotency_key text not null unique check (char_length(idempotency_key) between 1 and 128),
    status text not null default 'pending' check (status in ('pending', 'dispatched', 'done', 'failed')),
    attempts integer not null default 0 check (attempts >= 0),
    last_error text null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- ---------------------------------------------------------------- indexes
create index if not exists knowledge_documents_org_idx
    on public.knowledge_documents(organization_id);
create index if not exists knowledge_documents_org_active_idx
    on public.knowledge_documents(organization_id, is_active) where deleted_at is null;
create index if not exists knowledge_documents_status_idx
    on public.knowledge_documents(processing_status);
create index if not exists knowledge_document_versions_doc_idx
    on public.knowledge_document_versions(document_id, version_number);
create index if not exists knowledge_processing_outbox_status_idx
    on public.knowledge_processing_outbox(status, created_at);
create index if not exists knowledge_processing_outbox_doc_idx
    on public.knowledge_processing_outbox(document_id);

-- ------------------------------------------------------------ updated_at
drop trigger if exists knowledge_documents_set_updated_at on public.knowledge_documents;
create trigger knowledge_documents_set_updated_at
before update on public.knowledge_documents
for each row execute function public.set_updated_at();

drop trigger if exists knowledge_processing_outbox_set_updated_at on public.knowledge_processing_outbox;
create trigger knowledge_processing_outbox_set_updated_at
before update on public.knowledge_processing_outbox
for each row execute function public.set_updated_at();

-- ------------------------------------------------------------------- RLS
alter table public.knowledge_documents enable row level security;
alter table public.knowledge_document_versions enable row level security;
alter table public.knowledge_processing_outbox enable row level security;

-- Direct browser reads are limited to rows visible through membership.
-- (The Go API uses service_role and enforces authorization in code.)
drop policy if exists knowledge_documents_select_member on public.knowledge_documents;
create policy knowledge_documents_select_member on public.knowledge_documents
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = knowledge_documents.organization_id
          and m.user_id = auth.uid()
    )
);

drop policy if exists knowledge_document_versions_select_member on public.knowledge_document_versions;
create policy knowledge_document_versions_select_member on public.knowledge_document_versions
for select to authenticated
using (
    exists (
        select 1 from public.knowledge_documents d
        join public.organization_memberships m on m.organization_id = d.organization_id
        where d.id = knowledge_document_versions.document_id
          and m.user_id = auth.uid()
    )
);

-- No direct client writes: mutations go through the Go API (service_role).
-- No INSERT/UPDATE/DELETE policies are created for anon/authenticated.

grant usage on schema public to anon, authenticated, service_role;
grant select on public.knowledge_documents, public.knowledge_document_versions to authenticated;
grant all on public.knowledge_documents, public.knowledge_document_versions, public.knowledge_processing_outbox to service_role;

-- ---------------------------------------------------------------- storage
-- Private bucket for retained originals. Objects are addressed ONLY with the
-- service-role key from the Go API; browsers never receive that credential.
insert into storage.buckets (id, name, public)
values ('knowledge-originals', 'knowledge-originals', false)
on conflict (id) do nothing;

-- Deny-by-default is inherent (no policy => no access). Grant full control to
-- service_role and explicitly deny anon/authenticated direct access so that
-- organization-scoped paths are enforced by backend authorization instead.
drop policy if exists "knowledge-originals service_role all" on storage.objects;
create policy "knowledge-originals service_role all" on storage.objects
for all to service_role
using (bucket_id = 'knowledge-originals')
with check (bucket_id = 'knowledge-originals');

-- NOTE: no policies for anon/authenticated on this bucket => private.
