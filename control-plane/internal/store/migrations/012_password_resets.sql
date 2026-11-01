-- Password resets: a one-time link sent by email (or made by the
-- platform's owner) that sets a new password. Only token hashes are kept,
-- one live link per account.
create table if not exists password_resets (
  token_hash text primary key,
  user_id    uuid not null unique references users (id) on delete cascade,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null
);
