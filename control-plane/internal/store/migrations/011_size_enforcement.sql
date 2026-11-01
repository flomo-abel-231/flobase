-- Size enforcement: where a project stands against its plan's database
-- size. '' = fine, 'warn80' and 'warn90' = warned at 80% and 90%,
-- 'readonly' = over the size, so writes are refused until it is back under.
alter table projects add column if not exists size_state text not null default '';
