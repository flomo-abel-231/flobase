-- In the cloud, free projects are deleted a month after they are created.
-- expires_at is when; warned_days is the last warning sent (15, 10, 5, 3
-- or 1 days before), so each goes out once.
alter table projects add column if not exists expires_at timestamptz;
alter table projects add column if not exists warned_days int;

-- Storage: each project's measured size, and the most storage a
-- workspace's projects may take, in MB (null = no limit), set by the
-- platform's owner.
alter table projects add column if not exists size_bytes bigint;
alter table workspaces add column if not exists storage_limit_mb int;
