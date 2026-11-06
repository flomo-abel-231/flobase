-- Password reset tokens for the forgot-password flow. Each user has at most
-- one pending reset; requesting a new one replaces the old.

create table if not exists password_resets (
  id         uuid primary key default gen_random_uuid(),
  user_id    uuid not null references users (id) on delete cascade,
  token_hash text not null unique,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null
);

create unique index if not exists password_resets_user on password_resets (user_id);
