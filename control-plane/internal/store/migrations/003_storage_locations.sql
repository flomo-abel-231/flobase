-- Off-site backup storage locations added from the dashboard. At most one
-- is active: the one every project's backups are copied to.
create table if not exists storage_locations (
  id             uuid primary key default gen_random_uuid(),
  name           text not null,
  provider       text not null,
  endpoint       text not null,
  region         text not null default '',
  bucket         text not null,
  prefix         text not null default '',
  access_key     text not null,
  secret_key     text not null,
  encryption_key text not null default '',
  active         boolean not null default false,
  created_at     timestamptz not null default now()
);

create unique index if not exists storage_locations_one_active on storage_locations (active) where active;
