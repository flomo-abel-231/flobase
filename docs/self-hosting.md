# Self-hosting FloBase

FloBase runs on one Linux server with Docker. The installer pulls the released images from GitHub, writes the settings, starts everything, and keeps it up to date.

- [Requirements](#requirements)
- [Install](#install)
- [Sign in and create a project](#sign-in-and-create-a-project)
- [Configuration](#configuration)
- [Domain and HTTPS](#domain-and-https)
- [Updates](#updates)
- [Backups](#backups)
- [Uninstall](#uninstall)
- [Troubleshooting](#troubleshooting)

## Requirements

| | Minimum | Recommended |
| --- | --- | --- |
| OS | Linux with systemd (Ubuntu 22.04+, Debian 12+, Rocky/Alma 9+) | Ubuntu 24.04 LTS |
| CPU | 2 cores, x86_64 or arm64 | 4 cores |
| Memory | 2 GB | 8 GB: each project's database uses its plan's memory |
| Disk | 30 GB | 100 GB SSD: images take about 3 GB, then data and backups |
| Docker | Engine 24+ with the Compose v2 plugin | Installed by the installer if missing |

FloBase uses uncommon ports so they do not clash with other development tools. Open these in your firewall (or only 443 behind a reverse proxy, see [Domain and HTTPS](#domain-and-https)):

| Port | What |
| --- | --- |
| 28470 | Dashboard and control-plane API |
| 28471 | Project APIs (REST, GraphQL, auth) for your apps |
| Random high ports | Each project's direct and pooled database connections, shown on its Connect page |

macOS with Docker Desktop works for trying FloBase out (`FLOBASE_INSTALL_DIR=$HOME/flobase`), but run production on Linux.

## Install

On the server, as root or with sudo:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh | sudo bash
```

The installer:

1. checks the CPU, and installs Docker with [Docker's official script](https://get.docker.com) if it is missing;
2. finds the latest release and downloads its `docker-compose.yml`, `upgrade.sh` and `env.example` into `/data/flobase`;
3. writes `/data/flobase/.env` with a new API token and database password (only if `.env` does not exist yet);
4. pulls the images from `ghcr.io/flomo-abel-231/flobase`, starts FloBase and waits until it is healthy;
5. prints the dashboard URL and your API token.

Change the defaults with variables in front of `bash`:

```bash
curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh \
  | sudo FLOBASE_VERSION=0.1.0 FLOBASE_PUBLIC_HOST=db.example.com FLOBASE_AUTO_UPDATE=false bash
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLOBASE_VERSION` | latest release | Release to install |
| `FLOBASE_INSTALL_DIR` | `/data/flobase` | Where the compose file, `.env` and platform backups live |
| `FLOBASE_PUBLIC_HOST` | the server's first IP | Host name or IP your apps use to reach project databases |
| `FLOBASE_AUTO_UPDATE` | `true` | Install new releases automatically |
| `FLOBASE_PORT` | `28470` | Dashboard port |
| `FLOBASE_GATEWAY_PORT` | `28471` | Project API port |

Running the installer again is safe: it keeps `.env` and upgrades to the requested release.

### Install without the script

If you prefer to see every step, download a release's files and start them yourself. Replace `0.1.0` with the release you want:

```bash
mkdir -p /data/flobase && cd /data/flobase
v=0.1.0
for f in docker-compose.yml upgrade.sh env.example; do
  curl -fsSLO "https://github.com/flomo-abel-231/flobase/releases/download/v$v/$f"
done
cp env.example .env && chmod 600 .env
# Edit .env: set FLOBASE_API_TOKEN and FLOBASE_PLATFORM_DB_PASSWORD to long
# random strings (openssl rand -hex 24), FLOBASE_PUBLIC_HOST and
# FLOBASE_GATEWAY_URL to this server, FLOBASE_VERSION=$v, and
# FLOBASE_INSTALL_DIR=/data/flobase to allow automatic updates.
docker compose up -d
```

## Sign in and create a project

Open `http://<server>:28470` and create the owner account: enter the API token the installer printed (also `FLOBASE_API_TOKEN` in `.env`), your email and a password. Invite the rest of your team under **Team**. Then click **+ Add** on the Projects page. The first project on each engine downloads that engine's images, which takes a few minutes; after that a Postgres project is ready in under a minute.

Use a Postgres project from your app with the Supabase client libraries:

```js
import { createClient } from "@supabase/supabase-js";

const db = createClient("http://<server>:28471/project/<ref>", "<anon key>");
```

The project URL and keys are under the project's **Configuration → API**.

## Configuration

All settings are in `/data/flobase/.env`. After a change, apply it with:

```bash
cd /data/flobase && docker compose up -d
```

| Variable | What it does |
| --- | --- |
| `FLOBASE_API_TOKEN` | Owner-level token for the API, the CLI and creating the first account. Anyone with it controls the server's Docker; keep it secret. |
| `FLOBASE_PLATFORM_DB_PASSWORD` | Password of FloBase's own database. Do not change it after install. |
| `FLOBASE_MODE` | `self-hosted`, the default and the supported mode. (`cloud`, with public sign-up and prices, is kept for later and not offered.) |
| `FLOBASE_OWNER_EMAIL` | Your email: the owner account can be created with it, without the API token. |
| `FLOBASE_PUBLIC_HOST` | Host your apps use for direct database connections. |
| `FLOBASE_GATEWAY_URL` | Public URL of the project APIs, shown to developers and used by auth emails. |
| `FLOBASE_PORT`, `FLOBASE_GATEWAY_PORT` | Host ports for the dashboard and the project APIs. |
| `FLOBASE_AUTO_UPDATE` | `true` to install new releases automatically. |
| `FLOBASE_VERSION` | The running release. Set by the installer and updater; change it only through `upgrade.sh`. |
| `FLOBASE_BACKUP_S3_*`, `FLOBASE_BACKUP_ENCRYPTION_KEY` | Off-site backup copies; see [Backups](#backups). |

## Domain and HTTPS

Put a reverse proxy with TLS in front before you expose FloBase to the internet. With [Caddy](https://caddyserver.com), which gets certificates automatically, point two DNS names at the server and use:

```caddyfile
flobase.example.com {
	reverse_proxy localhost:28470
}

api.flobase.example.com {
	reverse_proxy localhost:28471
}
```

Then set `FLOBASE_GATEWAY_URL=https://api.flobase.example.com` and `FLOBASE_PUBLIC_HOST=flobase.example.com` in `.env`, run `docker compose up -d`, and close ports 28470 and 28471 to the outside.

## Updates

### Automatic

With `FLOBASE_AUTO_UPDATE=true` (the installer's default), FloBase checks [GitHub Releases](https://github.com/flomo-abel-231/flobase/releases) every six hours. When a newer release appears, it starts a short-lived updater container that runs `upgrade.sh`:

1. dumps the platform database (projects, keys, settings) to `/data/flobase/backups/`, keeping the last ten dumps;
2. downloads the release's `docker-compose.yml` and sets `FLOBASE_VERSION`;
3. pulls the new images and restarts the control plane and gateway;
4. waits up to three minutes for the control plane to report healthy, and **puts the previous version back** if it does not.

Project databases are separate containers: they keep running and serving your apps during an update. Each step is logged to `/data/flobase/upgrade.log`.

Settings → Updates in the dashboard shows the running and latest versions, the release notes, and an **Update to …** button for updating right away.

### Manual

Turn automatic updates off with `FLOBASE_AUTO_UPDATE=false` and update when you choose:

```bash
sudo sh /data/flobase/upgrade.sh          # the latest release
sudo sh /data/flobase/upgrade.sh 0.2.0    # a specific release
```

### Going back

`upgrade.sh` also installs older releases: `sudo sh /data/flobase/upgrade.sh 0.1.0`. If a newer release changed the platform database, restore the dump taken before the upgrade:

```bash
cd /data/flobase
docker compose stop flobase gateway
gunzip -c backups/platform-<time>-from-0.1.0.sql.gz | \
  docker exec -i flobase-platform-db-1 sh -c 'dropdb -U flobase --force flobase && createdb -U flobase flobase && psql -q -U flobase flobase'
docker compose up -d
```

## Backups

- **Project databases (Postgres):** archived continuously and backed up daily on the server, with point-in-time restore from each project's **Backups** tab. See the [README](../README.md#backups).
- **Off-site copies:** add a storage location on the dashboard's Off-site backups page, or set `FLOBASE_BACKUP_S3_BUCKET`, `FLOBASE_BACKUP_S3_ENDPOINT`, `FLOBASE_BACKUP_S3_ACCESS_KEY`, `FLOBASE_BACKUP_S3_SECRET_KEY` and, ideally, `FLOBASE_BACKUP_ENCRYPTION_KEY` in `.env` to copy every project's backups to S3-compatible storage. Keep the encryption key somewhere other than the server.
- **FloBase itself:** every update dumps the platform database to `backups/`. Copy `/data/flobase/.env` and `/data/flobase/backups/` off the server too.

## Uninstall

This deletes FloBase, every project database and every local backup. Off-site copies stay in your bucket.

```bash
cd /data/flobase
docker compose down
docker ps -aq --filter label=dev.flobase.project | xargs -r docker rm -f
docker volume ls -q --filter label=dev.flobase.project | xargs -r docker volume rm
docker network ls -q --filter label=dev.flobase.project | xargs -r docker network rm
docker volume rm flobase_platform-db
sudo rm -rf /data/flobase
```

## Troubleshooting

| Problem | What to check |
| --- | --- |
| Installer says Docker is unreachable | Run it with `sudo`, or add your user to the `docker` group and log in again. |
| Dashboard does not load | `docker compose -f /data/flobase/docker-compose.yml ps` and `… logs flobase`. |
| "Invalid API key" from your app | Send the project's anon or service_role key in the `apikey` header; supabase-js does this for you. |
| A project is stuck provisioning | Its first start pulls large images; watch `docker compose logs -f flobase`. |
| An update rolled back | Read `/data/flobase/upgrade.log`, then open an issue with it. |
| Images fail to pull | Check the server can reach `ghcr.io`, and that the release exists on the [Releases page](https://github.com/flomo-abel-231/flobase/releases). |

Ask questions and report problems in [GitHub Issues](https://github.com/flomo-abel-231/flobase/issues).
