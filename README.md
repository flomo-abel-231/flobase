# FloBase

[![CI](https://github.com/flomo-abel-231/flobase/actions/workflows/ci.yml/badge.svg)](https://github.com/flomo-abel-231/flobase/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/flomo-abel-231/flobase)](https://github.com/flomo-abel-231/flobase/releases/latest) [![Licence](https://img.shields.io/badge/licence-Apache%202.0-blue)](LICENSE)

Open-source databases you host yourself: every project gets its own Postgres, MySQL or MongoDB-compatible database, with API keys and a dashboard. Postgres projects also get a REST and GraphQL API, user sign-up and login, connection pooling, and backups with point-in-time recovery, compatible with the Supabase client libraries. Run it on one machine, or add servers in other locations as regions.

FloBase is free under the Apache 2.0 licence, with no paid plans. It is funded by donations, grants and support contracts: see [Support FloBase](#support-flobase).

This covers Phase 1 and the core of Phase 2 of the [feature plan](https://claude.ai/artifact/F4DZHUW5PtAGfXaofZmCkf).

## Run it on your computer

| | Download |
| --- | --- |
| macOS, Intel and Apple silicon (M1–M4) | [FloBase-macOS.dmg](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-macOS.dmg) |
| Windows x64 | [FloBase-Windows-x64-setup.exe](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-Windows-x64-setup.exe) |
| Windows on ARM | [FloBase-Windows-arm64.zip](https://github.com/flomo-abel-231/flobase/releases/latest/download/FloBase-Windows-arm64.zip) |

Or the `flobase` CLI, on macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.sh | sh
```

and on Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.ps1 | iex
```

FloBase Desktop and the CLI install everything FloBase needs on first start, including a Docker engine (its own on macOS, Docker Desktop on Windows), then open the dashboard signed in. See [running FloBase on your computer](docs/local.md).

## Install on a server

On a Linux server with 2 GB of memory or more:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh | sudo bash
```

The installer sets up Docker if needed, starts the latest release from `ghcr.io/flomo-abel-231/flobase` in `/data/flobase`, and prints the dashboard URL and your sign-in token. FloBase then updates itself when a new release is published, backing up its own database first and rolling back if the new version does not start.

The [self-hosting guide](docs/self-hosting.md) covers requirements, every setting, HTTPS with a domain, manual and automatic updates, backups and uninstalling.

## Use a Postgres project from your app

```js
import { createClient } from "@supabase/supabase-js";

const db = createClient("http://localhost:28471/project/<ref>", "<anon key>");
await db.auth.signUp({ email: "ada@example.com", password: "correct-horse-42" });
const { data } = await db.from("todos").select("*");
```

The project URL and keys are under the project's Configuration → API. GraphQL is at `<project URL>/graphql/v1`.

## What is in the repo

| Path | What it is |
| --- | --- |
| `control-plane/` | Go API: projects, regions, plans, API keys, provisioning. Never on the request path to a database. |
| `control-plane/cmd/flobase/`, `control-plane/local/` | The `flobase` CLI and the local runner it shares with FloBase Desktop: installs a Docker engine, runs FloBase on a laptop. |
| `desktop/` | FloBase Desktop for macOS and Windows (Wails). |
| `control-plane/cmd/flobase-gateway/` | API gateway: routes `/project/<ref>/rest/v1`, `/auth/v1` and `/graphql/v1`, checks API keys, adds CORS. |
| `control-plane/internal/provisioner/` | Docker driver, for the local host and for region servers reached over SSH or TLS; the router sends each project's calls to its region. |
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

FloBase is offered as self-hosted software only. `FLOBASE_MODE=self-hosted` is the default and the supported mode: plan sizes without prices, and accounts by invitation.

`FLOBASE_MODE=cloud` is kept in the code for a possible hosted service later and is not offered: it opens public sign-up and shows prices (estimated server cost in the region × `FLOBASE_MARKUP`, default 1.6, rounded up to .99).

## Regions

Customers pick a location for each project. Besides the machine FloBase runs on, each region is a Docker server the owner adds under **Regions**, for example a Hetzner server in Frankfurt or a Vultr one in Johannesburg. Give it a name and location label, how FloBase reaches Docker there, and the host name customers' database connections use:

- **SSH**: `ssh://root@203.0.113.10` and a private key; FloBase talks to the Docker socket through the SSH connection, like `docker -H ssh://`. Paste the server's host key (`ssh-keyscan -t ed25519 <host>`) or let FloBase trust the one it sees first.
- **TLS**: `tcp://203.0.113.10:2376` with `dockerd --tlsverify`, and the CA, client certificate and client key.

To prepare a fresh Ubuntu or Debian server, run as root on it:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/region-server.sh | \
  bash -s -- --control-plane <your FloBase's IP> --key "$(cat flobase-region.pub)"
```

It installs Docker, adds a `flobase` user that signs in with that key only, turns off SSH passwords, limits the relay port to the control plane (in Docker's own firewall chain, since Docker-published ports skip ufw) and prints what to paste into the form.

When the server is added, FloBase starts a **relay** there: a stock nginx container that joins every project network on that server and forwards TLS connections to the service named in them. The control plane and the gateway reach REST, auth and the SQL editor through it, and only they get in: both sides use certificates FloBase made for this server, and each accepts only the other's. Open the relay port (default 28474) to your FloBase's address only; customers' database ports are published as on the local host. New regions start closed: press **Check**, then **Open** to offer them. A region's **cost multiplier** scales its prices for servers that cost more there. A project stays on its region's server for its whole life, including backups, restores, password resets and logs; a region server can be removed once it has no projects.

## Develop

Build and run from this checkout (the dashboard on http://localhost:28470, project APIs on http://localhost:28471):

```bash
make up
```

Or run the pieces on the host:

```bash
make dev-db        # platform Postgres on :5433
make dev-api       # API on :28470 (cloud mode, token dev-token-change-me)
make dev-dashboard # dashboard on :5173
make test
```

## API

All routes except `health`, `config` and the sign-in routes under `/api/v1/auth` need `Authorization: Bearer <token>`: a person's access token from signing in, or `$FLOBASE_API_TOKEN`, which has the owner's rights.

| Method and path | Does |
| --- | --- |
| `POST /api/v1/auth/token?grant_type=password` | Sign in with `{"email", "password"}`; returns `access_token` (a JWT valid for 1 hour) and `refresh_token`, as Supabase Auth does |
| `POST /api/v1/auth/token?grant_type=refresh_token` | Exchange `{"refresh_token"}` for a new pair; each refresh token works once |
| `POST /api/v1/auth/logout?scope=local\|global\|others` | Sign out this session, all of them, or all but this one |
| `GET /api/v1/me`, `PATCH /api/v1/me`, `POST /api/v1/me/password` | Who is signed in; change your name or password |
| `GET /api/v1/team`, `POST /api/v1/team/invites`, `PATCH`/`DELETE /api/v1/team/users/{id}` | People and invites; inviting, roles and removing need the owner or an admin |
| `GET /api/v1/catalog` | Engines, plans, regions |
| `GET /api/v1/projects` | List projects |
| `POST /api/v1/projects` | Create: `{"name", "engine", "region", "plan"}`; returns 202 and provisions in the background |
| `GET /api/v1/projects/{ref}` | Project with connection details and anon / service_role keys |
| `DELETE /api/v1/projects/{ref}` | Delete the database and its data; the body must be `{"confirm": "<project name>"}` |
| `POST /api/v1/projects/{ref}/password` | Replace the database password with a new random one; services restart on the same ports and open connections end |
| `GET /api/v1/projects/{ref}/delete-check` | What the project holds, and whether it can be backed up to cloud storage first |
| `POST /api/v1/projects/{ref}/backups/offsite` | Take a backup and copy it to cloud storage now (Postgres) |
| `POST /api/v1/projects/{ref}/sql` | Run SQL as the database owner (Postgres, MySQL) |
| `GET/POST /api/v1/projects/{ref}/tables` | List or create tables in the public schema (Postgres) |
| `GET/POST/PATCH/DELETE /api/v1/projects/{ref}/tables/{table}/rows` | Browse and edit rows by primary key (Postgres) |
| `GET/POST /api/v1/projects/{ref}/users`, `DELETE …/users/{id}` | Manage the project's auth users (Postgres) |
| `GET /api/v1/projects/{ref}/logs?service=db` | Last 300 log lines of a service |
| `GET/POST /api/v1/projects/{ref}/backups` | List full backups and the restore window, or take a backup now (Postgres) |
| `POST /api/v1/projects/{ref}/restore` | Restore into a new project: `{"latest": true}`, `{"time": "<RFC 3339>"}` or `{"backup": "<name>"}` |
| `GET/PUT /api/v1/assistant/settings` | The assistant's models and permission mode (`read`, `ask` or `auto`) |
| `POST /api/v1/assistant/providers`, `PATCH/DELETE …/providers/{id}`, `POST …/providers/{id}/default`, `POST …/providers/test`, `POST …/providers/models` | Add, test and manage models; saving tests the key first |
| `GET/POST /api/v1/assistant/chats`, `GET/DELETE …/chats/{id}`, `POST …/chats/{id}/messages`, `POST …/chats/{id}/decisions` | Conversations with the assistant; it works in the background, and `decisions` approves or refuses a waiting action |
| `GET/POST /api/v1/automations`, `PATCH/DELETE …/automations/{id}`, `POST …/automations/{id}/run` | Prompts the assistant runs every hour, day or week |
| `GET/POST /api/v1/storages`, `POST …/storages/test`, `POST …/storages/{id}/activate`, `POST …/storages/deactivate`, `DELETE …/storages/{id}` | Manage storage locations for off-site copies; saving tests the bucket first |

## Backups

Postgres projects archive every change to a `flobase-<ref>-backups` Docker volume within a minute, and take a full backup when created and then daily. A restore replays archived changes on top of the nearest full backup into a **new** project; the original is never changed. Backups are kept for the plan's window (Free 1 day, Starter and Pro 7, Business 14) and are deleted with the project. In cloud mode, Free has no backups and point-in-time recovery starts on Pro.

### Off-site copies

On **Off-site backups**, add a storage location: an S3-compatible bucket (AWS S3, Cloudflare R2, Backblaze B2, Wasabi, MinIO). FloBase writes, reads and deletes a test file before saving it. The active location gets every Postgres project's backups through a small rclone container per project, every 30 seconds. With an encryption key (the form can generate one), contents and file names are encrypted before they leave the machine; keep that key somewhere other than the server, because the copies cannot be restored without it. Keys are kept in FloBase's database and never shown again, only their last four characters.

You can also set `FLOBASE_BACKUP_S3_BUCKET` and its keys in `.env` (see `.env.example`). That location then takes precedence and is shown read-only.

Off-site copies outlive the project. If the machine is lost, start FloBase on a new one with the same bucket settings: add the same location (and encryption key): Off-site backups lists every project found in the bucket, and **Restore latest** rebuilds it as a new project. No passwords from the old machine are needed. Without off-site storage, backups only protect against mistakes, not against losing the machine.

## Assistant

The assistant works on your FloBase with a model you choose and your own API key. It can list and create projects, run SQL, manage tables and auth users, take backups, restore, and read logs. Add a model under **Assistant → Models & permissions**:

- **Claude**, through Anthropic's API.
- **Any OpenAI-compatible service:** OpenAI, Google Gemini, OpenRouter, Groq, Mistral, DeepSeek, xAI, or **Ollama** on your own machine (no key; FloBase reaches it as `host.docker.internal`).

FloBase tests a key before saving it. Keys stay in FloBase's database and are never shown again, only their last four characters.

What it may do without asking is your choice:

| Setting | Runs at once | Waits for your approval |
| --- | --- | --- |
| Read only | Reads, read-only SQL | Nothing; changes are refused |
| Ask before changes (default) | Reads | Every change and delete |
| Ask before deletes | Reads and changes | Deletes: projects, users, `DROP`, `DELETE`, `TRUNCATE` |

SQL is sorted by its words, erring towards "change". A query the assistant runs as a read also runs in a read-only transaction, so the database refuses any change that slipped through.

**Automations** run a prompt every hour, day or week, such as "check every project's backups and report any older than a day". Each run is a conversation you can open. Nobody is there to approve, so a run only does what the setting allows without asking.

What you write, and what the assistant reads from your projects, goes to the model's service. Database passwords, API keys and connection strings are removed before anything is sent. Data from your databases is marked as data, not instructions, but a model can still be misled by text it reads. Keep "Ask before changes" for databases that hold content other people write.

## Accounts and workspaces

A **workspace** is one organization: its own projects, members, invites, assistant models and keys, conversations and automations. Nothing is shared between workspaces, and the API checks membership on every request, so one organization can never read or change another's projects, even by guessing a project's ref. People can belong to several workspaces and switch between them in the sidebar.

- **The owner** runs this FloBase. On a new install, the first person to open the dashboard creates the owner account: with the email set as `FLOBASE_OWNER_EMAIL` in `.env` (the install script takes it as a variable), or with the install's API token (`FLOBASE_API_TOKEN`). Either way nobody else can claim a public server first. The owner and platform admins also see **Infrastructure → Regions** and **Off-site backups**, and updates under Settings. They do not see other workspaces' projects.
- **Everyone else** joins by invitation; there is no public sign-up. (Cloud mode, which is not offered, would open sign-up with a workspace per account.)

A workspace's owner and admins invite people under **Members**: FloBase makes a one-time link, valid for 7 days, that you send yourself (FloBase does not send email yet). Someone new picks a name and password; someone who already has an account accepts with their password. The workspace's owner can rename it, and delete it once its projects are gone, under **Workspace**.

Signing in works like Supabase Auth: a JWT access token valid for 1 hour and a refresh token that is exchanged for a new pair before it runs out. Each refresh token works once, and reusing one ends its session. The dashboard keeps the refresh token in an httpOnly cookie and the access token in memory only. Signing out, removing someone and changing a password take effect at once. After 10 wrong passwords for an email, sign-in for it pauses for 15 minutes, and one IP address can create at most 10 accounts in 15 minutes.

The API token works with the platform owner's rights across every workspace: the `flobase` CLI and FloBase Desktop sign in with it. Send `X-FloBase-Workspace: <id>` with it to act in one workspace only; the assistant does this, so its tools reach only the conversation's workspace.

## Support FloBase

FloBase has no paid plans, so it depends on people and organizations who find it useful:

- **Sponsor on GitHub** at [github.com/sponsors/flomo-abel-231](https://github.com/sponsors/flomo-abel-231), monthly or once.
- **Donate** once through Stripe, $1 to $500: open **Support FloBase** in the dashboard's sidebar, or use a link directly: [$5](https://donate.stripe.com/eVqcN42AfbMg1ow4d84Ja02) · [$20](https://donate.stripe.com/28E6oG2Af7w0aZ6dNI4Ja04) · [$100](https://donate.stripe.com/28EcN4deTcQkc3a2504Ja06). Donations go to Manevia LLC, which develops FloBase.
- **Grants and sponsorship:** foundations, public programmes and companies can fund the roadmap. Sponsors are thanked here and in the release notes.
- **Paid support:** help setting FloBase up and a direct line when something breaks.
- **For free:** star the repository, report bugs, improve the docs, send pull requests.

Maintainers set the GitHub Sponsors account, the Stripe Payment Links and the contact address in [`dashboard/src/support.ts`](dashboard/src/support.ts), and the repository's Sponsor button in [`.github/FUNDING.yml`](.github/FUNDING.yml). Payments go straight to GitHub or Stripe; FloBase never handles them.

## Security notes

- Turn on the server firewall: `curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/firewall.sh | sudo bash`. It keeps SSH, HTTP/HTTPS and the project database ports open and refuses everything else, including Docker-published ports that would otherwise skip ufw. `--db-from <CIDR>` limits who can reach the databases, `--allow-port` keeps another port open. It turns itself off after 5 minutes unless you confirm it still lets you in.

- Auth confirms email addresses automatically because no email server is configured yet, for project users and FloBase accounts alike. Set one up before opening sign-up widely.
- The control plane mounts the Docker socket, which is root on the host. Keep the API token secret and do not expose the dashboard port (28470) to the internet without TLS in front.
- Database passwords and JWT secrets are stored in the platform database unencrypted. Encrypt them (KMS or OpenBao) before running a public service.

## Releasing

Pushing a `vX.Y.Z` tag builds the images, publishes a GitHub Release, and with it updates every self-hosted instance that has automatic updates on. See [RELEASING.md](RELEASING.md) and the [changelog](CHANGELOG.md).

## Licence

Apache 2.0.
