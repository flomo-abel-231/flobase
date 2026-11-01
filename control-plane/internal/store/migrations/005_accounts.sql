-- Team accounts: people sign in with email and password and get JWT
-- sessions, as with Supabase Auth. The first account is the owner; the
-- owner and admins invite the rest.

create table if not exists users (
  id              uuid primary key default gen_random_uuid(),
  email           text not null,
  name            text not null default '',
  password_hash   text not null,
  role            text not null check (role in ('owner', 'admin', 'member')),
  created_at      timestamptz not null default now(),
  last_sign_in_at timestamptz
);

create unique index if not exists users_email on users (lower(email));
create unique index if not exists users_one_owner on users (role) where role = 'owner';

-- A session is one signed-in browser or client. Only hashes of refresh
-- tokens are kept; previous_hash catches a refresh token used twice.
create table if not exists sessions (
  id            uuid primary key default gen_random_uuid(),
  user_id       uuid not null references users (id) on delete cascade,
  refresh_hash  text not null unique,
  previous_hash text not null default '',
  user_agent    text not null default '',
  created_at    timestamptz not null default now(),
  refreshed_at  timestamptz not null default now(),
  expires_at    timestamptz not null
);

create index if not exists sessions_previous on sessions (previous_hash) where previous_hash <> '';

create table if not exists invites (
  id         uuid primary key default gen_random_uuid(),
  email      text not null,
  role       text not null check (role in ('admin', 'member')),
  token_hash text not null unique,
  invited_by uuid references users (id) on delete set null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null
);

create unique index if not exists invites_email on invites (lower(email));
