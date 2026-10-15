-- Module 6: Chat orchestration persistence.
-- Owns chat_conversations and chat_messages only. Tenant/widget resolution
-- stays in Module 1, retrieval in Module 4, generation in Module 5.
-- Conversations are bound to (organization, widget) server-side; sessions
-- carry opaque server-issued tokens, never browser-chosen identifiers.

create extension if not exists pgcrypto;

create table if not exists public.chat_conversations (
    id uuid primary key default gen_random_uuid(),
    organization_id uuid not null references public.organizations(id) on delete cascade,
    widget_id uuid not null references public.website_widgets(id) on delete cascade,
    session_token text not null unique check (session_token ~ '^ses_[0-9a-f]{32}$'),
    status text not null default 'active' check (status in ('active', 'closed')),
    last_message_at timestamptz null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists public.chat_messages (
    id uuid primary key default gen_random_uuid(),
    conversation_id uuid not null references public.chat_conversations(id) on delete cascade,
    organization_id uuid not null references public.organizations(id) on delete cascade,
    role text not null check (role in ('user', 'assistant')),
    content text not null check (char_length(content) between 1 and 8000),
    status text not null default 'ok'
        check (status in ('ok', 'fallback', 'error')),
    sources jsonb not null default '[]'::jsonb,
    idempotency_key text null check (idempotency_key is null or char_length(idempotency_key) between 1 and 128),
    created_at timestamptz not null default now(),
    unique (conversation_id, idempotency_key),
    check (jsonb_typeof(sources) = 'array')
);

create index if not exists chat_conversations_org_idx
    on public.chat_conversations(organization_id);
create index if not exists chat_conversations_session_idx
    on public.chat_conversations(session_token);
create index if not exists chat_messages_conversation_idx
    on public.chat_messages(conversation_id, created_at);
create index if not exists chat_messages_org_idx
    on public.chat_messages(organization_id);

drop trigger if exists chat_conversations_set_updated_at on public.chat_conversations;
create trigger chat_conversations_set_updated_at
before update on public.chat_conversations
for each row execute function public.set_updated_at();

alter table public.chat_conversations enable row level security;
alter table public.chat_messages enable row level security;

-- Reads are limited to rows visible through active membership, mirroring the
-- other modules. All chat writes go through the Go API (service_role).
drop policy if exists chat_conversations_select_member on public.chat_conversations;
create policy chat_conversations_select_member on public.chat_conversations
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = chat_conversations.organization_id
          and m.user_id = auth.uid()
    )
);

drop policy if exists chat_messages_select_member on public.chat_messages;
create policy chat_messages_select_member on public.chat_messages
for select to authenticated
using (
    exists (
        select 1 from public.organization_memberships m
        where m.organization_id = chat_messages.organization_id
          and m.user_id = auth.uid()
    )
);

grant usage on schema public to anon, authenticated, service_role;
grant select on public.chat_conversations, public.chat_messages to authenticated;
grant all on public.chat_conversations, public.chat_messages to service_role;

-- Retention: delete conversations (messages cascade) older than the
-- configured window. Run on a schedule, e.g. daily cron:
--   delete from public.chat_conversations
--   where created_at < now() - make_interval(days => 90);
