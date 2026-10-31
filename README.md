# FloBase

Open-source cloud databases: every project gets its own Postgres, MySQL or MongoDB-compatible database, with API keys and a dashboard. Postgres projects also get a REST and GraphQL API, user sign-up and login, connection pooling, and backups with point-in-time recovery, compatible with the Supabase client libraries. One codebase runs self-hosted on a single machine or as a managed multi-cloud service.

This covers Phase 1 and the core of Phase 2 of the [feature plan](https://claude.ai/artifact/F4DZHUW5PtAGfXaofZmCkf).

## Run it

Needs Docker with Compose.

```bash
make up
```

This writes `.env` with random secrets, prints your API token, and starts FloBase: the dashboard on http://localhost:8080 and the project API gateway on http://localhost:8000. Sign in with the token, then create a project. The first project on each engine downloads its images, which takes a few minutes; after that a Postgres project is ready in under a minute.

## Use a Postgres project from your app

```js
import { createClient } from "@supabase/supabase-js";

const db = createClient("http://localhost:8000/project/<ref>", "<anon key>");
await db.auth.signUp({ email: "ada@example.com", password: "correct-horse-42" });
const { data } = await db.from("todos").select("*");
```

The project URL and keys are on the project's Overview tab. GraphQL is at `<project URL>/graphql/v1`.

## What is in the repo

| Path | What it is |
| --- | --- |
| `control-plane/` | Go API: projects, regions, plans, API keys, provisioning. Never on the request path to a database. |
| `control-plane/cmd/flobase-gateway/` | API gateway: routes `/project/<ref>/rest/v1`, `/auth/v1` and `/graphql/v1`, checks API keys, adds CORS. |
| `control-plane/internal/provisioner/` | Docker driver (self-hosted, local region). A Kubernetes driver for cloud regions comes next. |
| `dashboard/` | React + Vite dashboard, built on the design system. |
| `packages/design-system/` | FloBase design tokens: CSS variables, Tailwind v4 theme, typed tokens. |
| `docker-compose.yml`, `Dockerfile` | Self-hosted stack: control plane + dashboard, the gateway, and the platform's own Postgres. |

## Engines

| Engine | Runs as | Licence |
| --- | --- | --- |
| Postgres 17 | `supabase/postgres` with PgBouncer, PostgREST and Supabase Auth beside it | PostgreSQL, ISC, MIT |
| MySQL 8.4 LTS | `mysql:8.4` | GPLv2 |
| MongoDB-compatible | FerretDB 2 on Postgres + DocumentDB | Apache 2.0 / MIT |

MongoDB itself is under the SSPL, which would force the whole service's source open; FerretDB avoids that and works with MongoDB drivers.

## Modes

- `FLOBASE_MODE=self-hosted` (default): one organisation, the local Docker region, sizes without prices.
- `FLOBASE_MODE=cloud`: plans with prices (estimated cloud cost × `FLOBASE_MARKUP`, default 1.6) and the AWS, GCP and Azure regions, shown as "Coming soon" until their cells exist.

## Develop

```bash
make dev-db        # platform Postgres on :5433
make dev-api       # API on :8080 (cloud mode, token dev-token-change-me)
make dev-dashboard # dashboard on :5173
make test
```

## API

All routes except `health` and `config` need `Authorization: Bearer $FLOBASE_API_TOKEN`.

| Method and path | Does |
| --- | --- |
| `GET /api/v1/catalog` | Engines, plans, regions |
| `GET /api/v1/projects` | List projects |
| `POST /api/v1/projects` | Create: `{"name", "engine", "region", "plan"}`; returns 202 and provisions in the background |
| `GET /api/v1/projects/{ref}` | Project with connection details and anon / service_role keys |
| `DELETE /api/v1/projects/{ref}` | Delete the database and its data |
| `POST /api/v1/projects/{ref}/sql` | Run SQL as the database owner (Postgres, MySQL) |
| `GET/POST /api/v1/projects/{ref}/tables` | List or create tables in the public schema (Postgres) |
| `GET/POST/PATCH/DELETE /api/v1/projects/{ref}/tables/{table}/rows` | Browse and edit rows by primary key (Postgres) |
| `GET/POST /api/v1/projects/{ref}/users`, `DELETE …/users/{id}` | Manage the project's auth users (Postgres) |
| `GET /api/v1/projects/{ref}/logs?service=db` | Last 300 log lines of a service |
| `GET/POST /api/v1/projects/{ref}/backups` | List full backups and the restore window, or take a backup now (Postgres) |
| `POST /api/v1/projects/{ref}/restore` | Restore into a new project: `{"latest": true}`, `{"time": "<RFC 3339>"}` or `{"backup": "<name>"}` |

## Backups

Postgres projects archive every change to a `flobase-<ref>-backups` Docker volume within a minute, and take a full backup when created and then daily. A restore replays archived changes on top of the nearest full backup into a **new** project; the original is never changed. Backups are kept for the plan's window (Free 1 day, Starter and Pro 7, Business 14) and are deleted with the project. In cloud mode, Free has no backups and point-in-time recovery starts on Pro.

### Off-site copies

Set `FLOBASE_BACKUP_S3_BUCKET` and its keys in `.env` (see `.env.example`) and every Postgres project gets a small rclone container that copies its backups to S3-compatible storage (AWS S3, Cloudflare R2, Backblaze B2, Wasabi, MinIO) every 30 seconds. With `FLOBASE_BACKUP_ENCRYPTION_KEY` set, contents and file names are encrypted before they leave the machine; keep that key safe, because the copies cannot be restored without it.

Off-site copies outlive the project. If the machine is lost, start FloBase on a new one with the same bucket settings: the Projects page lists every project found in the bucket, and **Restore latest** rebuilds it as a new project. No passwords from the old machine are needed. Without off-site storage, backups only protect against mistakes, not against losing the machine.

## Security notes

- Auth confirms email addresses automatically because no email server is configured yet. Set one up before real users sign up.
- The control plane mounts the Docker socket, which is root on the host. Keep the API token secret and do not expose port 8080 to the internet without TLS in front.
- Database passwords and JWT secrets are stored in the platform database unencrypted. Encrypt them (KMS or OpenBao) before running a public service.

## Licence

Apache 2.0.
