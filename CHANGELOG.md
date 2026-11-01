# Changelog

All notable changes to FloBase. Versions follow [semantic versioning](https://semver.org).

## Unreleased

- **Server firewall:** `scripts/firewall.sh` turns on ufw with only SSH, HTTP/HTTPS and the project database ports open, and applies the same allowlist to Docker-published ports, which otherwise skip ufw. `--db-from` limits database access to given addresses, `--allow-port` keeps more ports open, and a 5-minute safety timer turns it off again unless confirmed.

## 0.8.0 — 2026-11-01

- **FloBase is self-hosted software, funded by donations.** There are no public cloud plans: self-hosted is the supported mode, and cloud mode stays in the code, not offered.
- **Support FloBase** page in the sidebar: sponsor on GitHub, give once through Stripe ($1, $2, $5, $10, $20, $50, $100 or $500), ask about grants, sponsorship or paid support, or help for free. The links are set in `dashboard/src/support.ts`.
- New dashboard layout modelled on Coolify's: a fixed top bar with the workspace switcher, a sidebar in sections (Workspace, Manage, Infrastructure), near-black panels in dark mode, rounded project cards with an engine tile and a status footer, and buttons and inputs with 8px corners. On phones the menu opens as a drawer from the right.
- The owner account can be created with the email set as `FLOBASE_OWNER_EMAIL`, without the API token. The install script takes `FLOBASE_OWNER_EMAIL` as a variable.
- Fixed: someone removed from the workspace their browser last used was signed out on every visit; the dashboard now forgets that workspace and opens another.

## 0.7.0 — 2026-11-01

- **Workspaces:** each workspace is its own organization with its own projects, members, invites, assistant models, conversations and automations, and nothing is shared between them. The API checks membership on every project request. People can belong to several workspaces and switch in the sidebar; **Workspace** creates, renames and deletes them.
- **Sign-up in the cloud:** anyone can create an account, which comes with a workspace named after their organization. No API token is needed to sign in. The platform owner is the account with `FLOBASE_OWNER_EMAIL`, or the first one. Regions, off-site backups and updates are under **Platform**, for the owner and platform admins only.
- Invites are per workspace. People who already have an account accept with their password.
- Upgrading moves an existing team, its projects and its assistant into one workspace, and the first owner to sign in takes it over.

## 0.6.0 — 2026-11-01

- **Regions on your own servers:** the owner adds a Docker server per location under **Regions** (reached over SSH, or TLS with client certificates), with a name, location label, public host for database connections and a cost multiplier, then opens or closes it. Customers pick the location when creating a project, with prices for that region (estimated cost × multiplier × `FLOBASE_MARKUP`). Every project operation — create, delete, backups, restores, password reset, logs, SQL editor, table editor, auth users — goes to the project's own server. The gateway and dashboard reach remote projects through a small relay FloBase starts on each server, which only lets in FloBase's own certificate. The AWS, GCP and Azure placeholder regions are gone. `scripts/region-server.sh` prepares a fresh Ubuntu or Debian server in one command.
- **Team accounts:** people sign in with email and password instead of sharing the API token. The first person creates the owner account (with the API token, so nobody else can claim a new public server), and the owner and admins invite others with a one-time link under **Team**, change their roles or remove them. **Account** changes your name and password and signs out everywhere.
- Sessions work like Supabase Auth: a JWT access token valid for 1 hour, renewed with a refresh token that works once. The dashboard keeps the refresh token in an httpOnly cookie, never in browser storage. Signing out, removing someone and changing a password take effect at once, and 10 wrong passwords pause sign-in for that email for 15 minutes.
- The API token keeps working with the owner's rights, for the `flobase` CLI, FloBase Desktop and automations. Existing installs ask for the owner account at the next sign-in.

## 0.5.1 — 2026-11-01

- **Reset password** under a project's Configuration → Connect replaces a leaked database password with a new random one, for Postgres, MySQL and MongoDB projects. Every database user that had the old password gets the new one, the project's services restart with it on the same ports, and open connections are closed.

## 0.5.0 — 2026-11-01

- **Assistant:** bring your own model and API key, and let it work on your FloBase. It can list and create projects, run SQL, manage tables and auth users, take backups, restore, and read logs. Supports Claude (Anthropic) and any OpenAI-compatible service: OpenAI, Gemini, OpenRouter, Groq, Mistral, DeepSeek, xAI, or Ollama on your machine.
- A permission setting decides what it may do without asking: read only, ask before changes (the default), or ask before deletes. Waiting actions show Approve and Refuse in the conversation. Passwords, keys and connection strings are never sent to the model.
- **Automations:** prompts the assistant runs every hour, day or week, each run kept as a conversation.
- The SQL API takes `"read_only": true` to run a query in read-only transactions.
- Light and dark mode use neutral greys for backgrounds, borders and text; sky blue is only the accent (buttons, links, the selected item, focus rings).

## 0.4.0 — 2026-11-01

- FloBase Desktop and `flobase` CLI installs update themselves, like server installs: the control plane checks GitHub Releases every 6 hours and runs `upgrade.sh` in `~/.flobase`, which backs up FloBase's own database and goes back to the previous version if the new one does not come up healthy. The dashboard's Settings shows automatic updates as on, and **Update to …** works there. Installs from 0.3.0 and before turn this on with their next update (Check for updates, or `flobase update`) or the next start of the new app. To turn it off, set `FLOBASE_AUTO_UPDATE=false` in `~/.flobase/.env`. On Windows, Docker cannot mount the install folder, so update from the desktop app or with `flobase update`; the dashboard now says so instead of pointing at `upgrade.sh`.
- `flobase update` and the desktop app's Check for updates also back up FloBase's own database first and go back to the previous version if the new one fails to start.
- FloBase Desktop says when a newer version of the app is available, with a link to download it. `flobase update` prints the install command when a newer CLI is out.
- `upgrade.sh` works for any install folder, Compose project and `FLOBASE_NAME`, and does not rely on the container names of an install in `/data/flobase`.
- `FLOBASE_LOCAL_NAME` runs a second local FloBase with its own Compose project and container names.

## 0.3.0 — 2026-11-01

- Storage locations: add S3-compatible buckets for off-site backup copies on the Off-site backups page, with presets for AWS S3, Cloudflare R2, Backblaze B2, Wasabi and MinIO. FloBase tests the bucket before saving, and you can switch the active location or stop copying without a restart. Before, off-site storage could only be set in `.env`, which still works and takes precedence.
- Deleting a project asks you to type its name. When the project holds data, FloBase offers to back it up to cloud storage first, so it can be restored from Off-site backups later. The API's delete now needs `{"confirm": "<project name>"}`.
- New sky blue look, in light and dark mode, for the dashboard, the desktop app and its icon.
- A cloud backup finishes in seconds instead of about a minute: FloBase no longer waits for the WAL file Postgres is still writing.

## 0.2.1 — 2026-11-01

- FloBase Desktop on macOS finds an installed Docker Desktop or OrbStack. Apps opened from Finder do not get the terminal's PATH, so 0.2.0 installed its own engine even when Docker Desktop was there.
- FloBase checks that the dashboard on its port is its own, signing in with its token. When another program or FloBase holds port 8080 or 8000, it now says so instead of opening that program's page, where the sign-in link was rejected.
- New default ports: the dashboard on **28470** and the project APIs on **28471**, instead of 8080 and 8000, which other development tools usually hold. Desktop and CLI installs pick the next free pair at first setup when those are taken. Existing installs keep the ports in their `.env`.
- Desktop and CLI installs use their own container names (`flobase-local-…`, set by the new `FLOBASE_NAME`), so they run beside a server install or a source checkout on the same Docker engine.

## 0.2.0 — 2026-11-01

- FloBase Desktop for macOS (one app for Intel and Apple silicon M1–M4) and Windows (x64 installer, ARM64 app).
- `flobase` CLI for macOS, Windows and Linux on x64 and ARM64: `setup`, `start`, `stop`, `status`, `open`, `logs`, `update`, `uninstall`.
- Both install what FloBase needs on first start: on macOS its own Docker engine (Colima, no admin rights), on Windows WSL 2 and Docker Desktop, on Linux Docker Engine.
- The dashboard signs in from a link the CLI and desktop app open, without asking for the token.
- Releases build the CLI and desktop apps and publish only when every file is attached.

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
