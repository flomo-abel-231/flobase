-- Network restrictions: the addresses allowed to reach a project's
-- database and pooler ports. Empty = anyone may try to connect (the
-- password still applies). FloBase enforces them on those ports only.
alter table projects add column if not exists allowed_cidrs text[] not null default '{}';
