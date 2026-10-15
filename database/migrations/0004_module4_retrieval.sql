-- Module 4: Retrieval engine read contract.
-- Adds a read-only SQL function over Module 3's knowledge_chunks. The
-- function only SELECTs; it never modifies chunks or document state.
-- Eligibility (documented lifecycle contract, enforced in the query):
--   chunk is_active AND document is_active AND document not soft-deleted
--   AND chunk version == document current_version_id.
-- This excludes inactive, deleted, stale (superseded), staged/unpublished,
-- and incompletely indexed content, including the window before Module 3's
-- index purge runs (the document-side flags cover that gap).
-- No schema change to Module 2/3 tables was needed: 0002+0003 already expose
-- every column this contract requires.

create or replace function public.match_knowledge_chunks(
    p_organization_id uuid,
    p_query_embedding extensions.vector(384),
    p_match_count integer,
    p_document_ids uuid[] default null
)
returns table (
    chunk_id uuid,
    document_id uuid,
    document_version_id uuid,
    content_text text,
    token_count integer,
    source_meta jsonb,
    distance double precision
)
language sql
stable
set search_path = public, extensions
as $$
    select
        c.id,
        c.document_id,
        c.document_version_id,
        c.content_text,
        c.token_count,
        c.source_meta,
        (c.embedding <=> p_query_embedding)::double precision as distance
    from public.knowledge_chunks c
    join public.knowledge_documents d on d.id = c.document_id
    where c.organization_id = p_organization_id
      and c.is_active
      and d.is_active
      and d.deleted_at is null
      and d.current_version_id = c.document_version_id
      and (p_document_ids is null or c.document_id = any (p_document_ids))
    order by c.embedding <=> p_query_embedding
    limit greatest(1, least(p_match_count, 100))
$$;

-- Read-only contract: executable by the privileged service identity only.
-- Authenticated browser roles get no access; tenant filtering is enforced by
-- the p_organization_id argument inside the function body.
revoke all on function public.match_knowledge_chunks(uuid, extensions.vector, integer, uuid[]) from public, anon, authenticated;
grant execute on function public.match_knowledge_chunks(uuid, extensions.vector, integer, uuid[]) to service_role;
