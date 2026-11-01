-- The assistant: language models people add with their own API keys,
-- conversations with it, and automations that run it on a schedule.

create table if not exists llm_providers (
  id         uuid primary key default gen_random_uuid(),
  name       text not null,
  kind       text not null,             -- anthropic or openai (OpenAI-compatible)
  preset     text not null default '',  -- which form preset it came from
  base_url   text not null default '',
  api_key    text not null default '',
  model      text not null,
  is_default boolean not null default false,
  created_at timestamptz not null default now()
);

create unique index if not exists llm_providers_one_default on llm_providers (is_default) where is_default;

-- Small settings that are not worth a table each.
create table if not exists settings (
  key   text primary key,
  value text not null
);

create table if not exists automations (
  id            uuid primary key default gen_random_uuid(),
  name          text not null,
  prompt        text not null,
  provider_id   uuid references llm_providers (id) on delete set null,
  every         text not null,          -- hour, day or week
  enabled       boolean not null default true,
  next_run_at   timestamptz not null default now(),
  last_run_at   timestamptz,
  last_chat_id  uuid,
  created_at    timestamptz not null default now()
);

create table if not exists assistant_chats (
  id            uuid primary key default gen_random_uuid(),
  title         text not null,
  provider_id   uuid references llm_providers (id) on delete set null,
  automation_id uuid references automations (id) on delete set null,
  status        text not null default 'idle', -- idle, running, waiting, failed
  transcript    jsonb not null default '[]',
  state         jsonb not null default '{}',
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

create index if not exists assistant_chats_updated on assistant_chats (updated_at desc);
