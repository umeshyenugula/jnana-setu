-- Module 3: Document processing & embeddings.
-- Owns: ingestion_jobs, knowledge_chunks. Reads (never writes) Module 2
-- document/version metadata; original bytes come from the private
-- `knowledge-originals` bucket via the API's service-role storage client.
-- Retrieval (Module 4) consumes knowledge_chunks where is_active = true.

create extension if not exists pgcrypto;
create extension if not exists vector;

-- ------------------------------------------------------------ jobs
create table if not exists public.ingestion_jobs (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    document_id uuid not null references public.knowledge_documents(id) on delete cascade,
    document_version_id uuid not null references public.knowledge_document_versions(id) on delete cascade,
    operation text not null check (operation in ('index', 'reindex', 'delete')),
    idempotency_key text not null unique check (char_length(idempotency_key) between 1 and 128),
    status text not null default 'queued'
        check (status in ('queued', 'processing', 'completed', 'failed', 'cancelled')),
    attempts integer not null default 0 check (attempts >= 0),
    max_attempts integer not null default 5 check (max_attempts between 1 and 20),
    error_code text null check (error_code is null or char_length(error_code) between 1 and 64),
    error_message text null,
    requested_by text null,
    started_at timestamptz null,
    finished_at timestamptz null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    check (finished_at is null or status in ('completed', 'failed', 'cancelled'))
);

-- ------------------------------------------------------------ chunks
-- Staging-and-publish: new version chunks are written with is_active=false,
-- then a single transaction flips the new set active and supersedes older
-- versions of the same document. A failed replacement never leaves a partial
-- active set: staged rows stay inactive and are cleaned up by retry/purge.
create table if not exists public.knowledge_chunks (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    document_id uuid not null references public.knowledge_documents(id) on delete cascade,
    document_version_id uuid not null references public.knowledge_document_versions(id) on delete cascade,
    chunk_index integer not null check (chunk_index >= 0),
    content_text text not null check (char_length(content_text) between 1 and 8000),
    embedding extensions.vector(384) not null,
    token_count integer null check (token_count is null or token_count > 0),
    source_meta jsonb not null default '{}'::jsonb,
    is_active boolean not null default false,
    created_at timestamptz not null default now(),
    unique (organization_id, document_id, document_version_id, chunk_index),
    check (jsonb_typeof(source_meta) = 'object')
);

-- ------------------------------------------------------------ indexes
create index if not exists ingestion_jobs_org_idx
    on public.ingestion_jobs(organization_id);
create index if not exists ingestion_jobs_status_idx
    on public.ingestion_jobs(status, created_at);
create index if not exists ingestion_jobs_doc_version_idx
    on public.ingestion_jobs(document_id, document_version_id);
create index if not exists knowledge_chunks_active_lookup_idx
    on public.knowledge_chunks(organization_id, document_id, is_active)
    where is_active = true;
create index if not exists knowledge_chunks_version_idx
    on public.knowledge_chunks(document_version_id);
-- Vector index for Module 4 cosine-similarity retrieval over active chunks.
create index if not exists knowledge_chunks_embedding_hnsw_idx
    on public.knowledge_chunks using hnsw (embedding vector_cosine_ops);

-- ------------------------------------------------------------ updated_at
drop trigger if exists ingestion_jobs_set_updated_at on public.ingestion_jobs;
create trigger ingestion_jobs_set_updated_at
before update on public.ingestion_jobs
for each row execute function public.set_updated_at();

-- ------------------------------------------------------------ RLS
alter table public.ingestion_jobs enable row level security;
alter table public.knowledge_chunks enable row level security;

drop policy if exists ingestion_jobs_select_member on public.ingestion_jobs;
create policy ingestion_jobs_select_member on public.ingestion_jobs
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = ingestion_jobs.organization_id
          and m.user_id = auth.uid()
    )
);

drop policy if exists knowledge_chunks_select_member on public.knowledge_chunks;
create policy knowledge_chunks_select_member on public.knowledge_chunks
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = knowledge_chunks.organization_id
          and m.user_id = auth.uid()
    )
);

-- Writes go through the Go API / worker with service_role; no direct
-- anon/authenticated INSERT/UPDATE/DELETE policies are created.

grant usage on schema public to anon, authenticated, service_role;
grant usage on schema extensions to anon, authenticated, service_role;
grant select on public.ingestion_jobs, public.knowledge_chunks to authenticated;
grant all on public.ingestion_jobs, public.knowledge_chunks to service_role;
