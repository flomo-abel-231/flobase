-- Workspaces: each is its own organization, with its own members,
-- projects, invites, assistant models, chats and automations. Nothing is
-- shared between workspaces. users.role is now the person's role on the
-- platform itself (owner = whoever runs this FloBase); a workspace role is
-- per membership.

create table if not exists workspaces (
  id         uuid primary key default gen_random_uuid(),
  name       text not null,
  created_at timestamptz not null default now()
);

create table if not exists workspace_members (
  workspace_id uuid not null references workspaces (id) on delete cascade,
  user_id      uuid not null references users (id) on delete cascade,
  role         text not null check (role in ('owner', 'admin', 'member')),
  created_at   timestamptz not null default now(),
  primary key (workspace_id, user_id)
);

create index if not exists workspace_members_user on workspace_members (user_id);

alter table projects add column if not exists workspace_id uuid references workspaces (id);
alter table invites add column if not exists workspace_id uuid references workspaces (id) on delete cascade;
alter table llm_providers add column if not exists workspace_id uuid references workspaces (id) on delete cascade;
alter table automations add column if not exists workspace_id uuid references workspaces (id) on delete cascade;
alter table assistant_chats add column if not exists workspace_id uuid references workspaces (id) on delete cascade;

create index if not exists projects_workspace on projects (workspace_id);

-- An address can be invited to several workspaces, once each; each
-- workspace has its own default model.
drop index if exists invites_email;
create unique index if not exists invites_workspace_email on invites (workspace_id, lower(email));
drop index if exists llm_providers_one_default;
create unique index if not exists llm_providers_workspace_default on llm_providers (workspace_id) where is_default;

-- Installs from 0.6.0 had one team: it becomes the first workspace, with
-- everyone in it keeping their role.
do $$
declare ws uuid;
begin
  if not exists (select 1 from workspaces) and (
       exists (select 1 from users) or exists (select 1 from projects)
       or exists (select 1 from llm_providers) or exists (select 1 from automations)
       or exists (select 1 from assistant_chats)) then
    insert into workspaces (name) values ('Default workspace') returning id into ws;
    insert into workspace_members (workspace_id, user_id, role) select ws, id, role from users;
    update projects set workspace_id = ws where workspace_id is null;
    update invites set workspace_id = ws where workspace_id is null;
    update llm_providers set workspace_id = ws where workspace_id is null;
    update automations set workspace_id = ws where workspace_id is null;
    update assistant_chats set workspace_id = ws where workspace_id is null;
    insert into settings (key, value)
      select 'assistant_mode:' || ws, value from settings where key = 'assistant_mode'
      on conflict (key) do nothing;
  end if;
end $$;
