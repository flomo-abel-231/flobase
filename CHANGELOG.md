# Changelog

All notable changes to FloBase. Versions follow [semantic versioning](https://semver.org).

## 0.1.1 — 2026-10-31

- README shows CI, latest-release and licence badges.
- First release delivered through automatic updates: instances on 0.1.0 install it by themselves.

## 0.1.0 — 2026-10-31

First release.

- Projects on Postgres 17, MySQL 8.4 or a MongoDB-compatible engine (FerretDB 2), each in its own containers.
- Postgres projects with Supabase's database image, PgBouncer pooling, a REST and GraphQL API (PostgREST, pg_graphql) and user auth (Supabase Auth), behind an API gateway that works with supabase-js unchanged.
- Dashboard with table editor, SQL editor, auth users, backups and logs, in light and dark themes.
- Backups for Postgres projects: continuous archiving, daily full backups, and point-in-time restore into a new project.
- Off-site backup copies to S3-compatible storage, with optional client-side encryption and restore after losing the server.
- One-line installer, Docker Compose deployment, and automatic updates from GitHub Releases with rollback.
