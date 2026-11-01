#!/bin/sh
# Upgrades a FloBase installed by install.sh to VERSION (default: the latest
# release). FloBase runs this itself for automatic updates, in a short-lived
# docker:cli container; you can also run it by hand:
#
#   sh /data/flobase/upgrade.sh          # latest
#   sh /data/flobase/upgrade.sh 0.2.0    # a given release, also to go back
#
# It backs up the platform database, swaps in the release's compose file,
# pulls the new images and restarts. If the new version is not healthy
# within three minutes it puts the previous version back. Project databases
# are separate containers and keep running throughout.
#
# FloBase Desktop and the flobase CLI install the same way in ~/.flobase,
# where .env sets COMPOSE_PROJECT_NAME and FLOBASE_NAME.
set -eu

DIR="${FLOBASE_INSTALL_DIR:-$(cd "$(dirname "$0")" && pwd)}"
REPO="${FLOBASE_UPDATE_REPO:-flomo-abel-231/flobase}"
VERSION="${1:-latest}"
cd "$DIR"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*" | tee -a "$DIR/upgrade.log"; }
fetch() {
  if command -v curl > /dev/null; then curl -fsSL "$1" -o "$2"; else wget -qO "$2" "$1"; fi
}
setenv() {
  if grep -q "^$1=" .env; then
    sed -i.bak "s|^$1=.*|$1=$2|" .env && rm -f .env.bak
  else
    printf '%s=%s\n' "$1" "$2" >> .env
  fi
}

if [ ! -f .env ] || [ ! -f docker-compose.yml ]; then
  echo "No FloBase install in $DIR." >&2
  exit 1
fi

if [ "$VERSION" = latest ]; then
  fetch "https://api.github.com/repos/$REPO/releases/latest" /tmp/flobase-release.json
  VERSION="$(sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' /tmp/flobase-release.json | head -n 1)"
  [ -n "$VERSION" ] || { log "could not find the latest release"; exit 1; }
fi
VERSION="${VERSION#v}"
OLD="$(sed -n 's/^FLOBASE_VERSION=//p' .env)"
NAME="$(sed -n 's/^FLOBASE_NAME=//p' .env)"
NAME="${NAME:-flobase}"
BASE="https://github.com/$REPO/releases/download/v$VERSION"
log "upgrading FloBase ${OLD:-unknown} to $VERSION"

# 1. Back up the platform database (projects, keys, regions); keep 10.
mkdir -p backups
dump="backups/platform-$(date -u +%Y%m%dT%H%M%SZ)-from-${OLD:-unknown}.sql.gz"
docker compose exec -T platform-db pg_dump -U flobase -d flobase | gzip > "$dump"
log "platform database saved to $dump"
# shellcheck disable=SC2012 # names are generated above, no odd characters
ls -1t backups/platform-*.sql.gz | tail -n +11 | while read -r f; do rm -f "$f"; done

# 2. Fetch the release's files, keeping the current ones to roll back to.
cp docker-compose.yml docker-compose.yml.prev
cp .env .env.prev
fetch "$BASE/docker-compose.yml" docker-compose.yml.new || { log "release v$VERSION not found"; exit 1; }
fetch "$BASE/upgrade.sh" upgrade.sh.new || cp upgrade.sh upgrade.sh.new
fetch "$BASE/env.example" env.example.new || cp env.example env.example.new 2> /dev/null || true

rollback() {
  log "upgrade failed; going back to ${OLD:-the previous version}"
  mv docker-compose.yml.prev docker-compose.yml
  mv .env.prev .env
  rm -f upgrade.sh.new env.example.new
  docker compose up -d --remove-orphans || true
  log "rolled back. The platform database from before is in $dump"
  exit 1
}

mv docker-compose.yml.new docker-compose.yml
setenv FLOBASE_VERSION "$VERSION"

# 3. Pull and restart.
docker compose pull --quiet || rollback
docker compose up -d --remove-orphans || rollback

# 4. Wait for the control plane to report healthy.
i=0
until [ "$(docker inspect -f '{{.State.Health.Status}}' "$NAME-control-plane" 2> /dev/null)" = healthy ]; do
  i=$((i + 1))
  [ "$i" -lt 60 ] || rollback
  sleep 3
done

# Replacing this script last is safe: the running shell keeps the old file.
mv upgrade.sh.new upgrade.sh
chmod +x upgrade.sh
[ -f env.example.new ] && mv env.example.new env.example
rm -f docker-compose.yml.prev .env.prev
log "FloBase is now $VERSION"
