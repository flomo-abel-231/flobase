-- Region servers: each cloud region is one Docker host somewhere (a Hetzner
-- server in Frankfurt, a Vultr one in Johannesburg) that the control plane
-- drives over SSH or TLS with client certificates. The local region is the
-- host the control plane runs on and has no row here.
--
-- Project services on a region server are reached through its relay, a
-- small TLS proxy the control plane starts there; only holders of the
-- relay's client certificate (the control plane and the gateway) get in.
-- As with storage locations, the secrets never leave the server's API.

create table if not exists region_servers (
  region_id         text primary key references regions (id) on delete cascade,
  transport         text not null check (transport in ('ssh', 'tls')),
  -- ssh://user@host:22 or tcp://host:2376
  endpoint          text not null,
  ssh_key           text not null default '',
  -- authorized_keys form; recorded on first connect when left empty.
  ssh_host_key      text not null default '',
  tls_ca            text not null default '',
  tls_cert          text not null default '',
  tls_key           text not null default '',
  -- Host name customers' database connections use.
  public_host       text not null,
  relay_port        integer not null,
  relay_server_cert text not null,
  relay_server_key  text not null,
  relay_client_cert text not null,
  relay_client_key  text not null,
  created_at        timestamptz not null default now()
);

-- The aws, gcp and azure regions were placeholders that never ran a
-- project; real regions are region servers now.
delete from regions r
where r.provider in ('aws', 'gcp', 'azure')
  and not exists (select 1 from projects p where p.region = r.id);
