#!/usr/bin/env bash
# FloBase installer: sets up a self-hosted FloBase on this machine from the
# released images on GitHub.
#
#   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install.sh | sudo bash
#
# Running it again upgrades in place and keeps .env. Options, as variables:
#   FLOBASE_VERSION       release to install, e.g. 0.2.0 (default: latest)
#   FLOBASE_INSTALL_DIR   where FloBase lives (default: /data/flobase)
#   FLOBASE_PUBLIC_HOST   host or IP clients use (default: this machine's IP)
#   FLOBASE_AUTO_UPDATE   install new releases automatically (default: true)
#   FLOBASE_PORT          dashboard port (default: 8080)
#   FLOBASE_GATEWAY_PORT  project API port (default: 8000)
set -euo pipefail

REPO="${FLOBASE_UPDATE_REPO:-flomo-abel-231/flobase}"
DIR="${FLOBASE_INSTALL_DIR:-/data/flobase}"
VERSION="${FLOBASE_VERSION:-latest}"
AUTO_UPDATE="${FLOBASE_AUTO_UPDATE:-true}"
PORT="${FLOBASE_PORT:-8080}"
GATEWAY_PORT="${FLOBASE_GATEWAY_PORT:-8000}"

say() { printf '\033[1;33m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }
rand() { head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n'; }

# setenv KEY VALUE writes KEY=VALUE into .env, replacing an existing line.
setenv() {
  if grep -q "^$1=" .env; then
    sed -i.bak "s|^$1=.*|$1=$2|" .env && rm -f .env.bak
  else
    printf '%s=%s\n' "$1" "$2" >> .env
  fi
}

# --- Checks ---------------------------------------------------------------

OS="$(uname -s)"
case "$(uname -m)" in
  x86_64 | amd64 | aarch64 | arm64) ;;
  *) die "FloBase images are built for x86_64 and arm64, not $(uname -m)." ;;
esac
command -v curl > /dev/null || die "curl is required."

if ! command -v docker > /dev/null; then
  [ "$OS" = Linux ] || die "Install Docker Desktop first: https://docs.docker.com/desktop/"
  [ "$(id -u)" -eq 0 ] || die "Run as root (sudo) so the installer can install Docker."
  say "Installing Docker with Docker's official script"
  curl -fsSL https://get.docker.com | sh
fi
docker compose version > /dev/null 2>&1 || die "The Docker Compose v2 plugin is required: https://docs.docker.com/compose/install/"
docker info > /dev/null 2>&1 || die "Cannot reach Docker. Is it running? On Linux, run the installer with sudo."

# --- Release --------------------------------------------------------------

if [ "$VERSION" = latest ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$VERSION" ] || die "Could not find a FloBase release in $REPO."
fi
VERSION="${VERSION#v}"
BASE="https://github.com/$REPO/releases/download/v$VERSION"
say "Installing FloBase $VERSION into $DIR"

mkdir -p "$DIR/backups"
cd "$DIR"
for f in docker-compose.yml upgrade.sh env.example; do
  curl -fsSL "$BASE/$f" -o "$f.new" || die "Could not download $f from release v$VERSION."
  mv "$f.new" "$f"
done
chmod +x upgrade.sh

# --- Configuration --------------------------------------------------------

if [ -f .env ]; then
  say "Keeping the existing .env"
else
  host="${FLOBASE_PUBLIC_HOST:-}"
  if [ -z "$host" ] && [ "$OS" = Linux ]; then
    host="$(hostname -I 2> /dev/null | awk '{print $1}')"
  fi
  host="${host:-localhost}"
  cp env.example .env
  chmod 600 .env
  setenv FLOBASE_API_TOKEN "$(rand)"
  setenv FLOBASE_PLATFORM_DB_PASSWORD "$(rand)"
  setenv FLOBASE_PUBLIC_HOST "$host"
  setenv FLOBASE_PORT "$PORT"
  setenv FLOBASE_GATEWAY_PORT "$GATEWAY_PORT"
  setenv FLOBASE_GATEWAY_URL "http://$host:$GATEWAY_PORT"
  setenv FLOBASE_AUTO_UPDATE "$AUTO_UPDATE"
  say "Wrote .env with new secrets"
fi
setenv FLOBASE_VERSION "$VERSION"
setenv FLOBASE_INSTALL_DIR "$DIR"
setenv FLOBASE_UPDATE_REPO "$REPO"

# --- Start ----------------------------------------------------------------

say "Pulling images"
docker compose pull --quiet
say "Starting FloBase"
docker compose up -d --remove-orphans

i=0
until [ "$(docker inspect -f '{{.State.Health.Status}}' flobase-control-plane 2> /dev/null)" = healthy ]; do
  i=$((i + 1))
  [ "$i" -lt 60 ] || die "FloBase did not become healthy. See: docker compose -f $DIR/docker-compose.yml logs flobase"
  sleep 3
done

host="$(sed -n 's/^FLOBASE_PUBLIC_HOST=//p' .env)"
token="$(sed -n 's/^FLOBASE_API_TOKEN=//p' .env)"
auto="$(sed -n 's/^FLOBASE_AUTO_UPDATE=//p' .env)"
cat << EOF

  FloBase $VERSION is running.

  Dashboard     http://$host:$(sed -n 's/^FLOBASE_PORT=//p' .env)
  Project APIs  $(sed -n 's/^FLOBASE_GATEWAY_URL=//p' .env)
  API token     $token   (sign in with this; it is also in $DIR/.env)

  Automatic updates are $([ "$auto" = true ] && echo on || echo off). Settings live in $DIR/.env.
  Put HTTPS in front before exposing FloBase to the internet:
  https://github.com/$REPO/blob/main/docs/self-hosting.md

EOF
