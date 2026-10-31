create table if not exists regions (
  id               text primary key,
  name             text not null,
  provider         text not null,
  location         text not null,
  status           text not null,
  price_multiplier numeric not null default 1
);

create table if not exists projects (
  id            uuid primary key default gen_random_uuid(),
  ref           text not null unique,
  name          text not null,
  engine        text not null,
  region        text not null references regions (id),
  plan          text not null,
  status        text not null,
  status_detail text not null default '',
  db_host       text not null default '',
  db_port       integer not null default 0,
  db_user       text not null,
  db_password   text not null,
  db_name       text not null,
  jwt_secret    text not null,
  anon_key      text not null,
  service_key   text not null,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now()
);

create index if not exists projects_created_at on projects (created_at desc);
