#!/usr/bin/env bash
# Turns on a firewall for a FloBase server (Ubuntu or Debian): only SSH,
# HTTP/HTTPS and the ports Docker gives project databases stay reachable
# from outside; everything else is refused. Run it as root:
#
#   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/firewall.sh | bash
#
# Options:
#   --db-from CIDR      who may reach project databases (default: anyone);
#                       repeat for more, e.g. your app servers' addresses
#   --allow-port PORT   another TCP port to keep open (repeatable), such as
#                       28470 for a dashboard served without HTTPS
#   --no-rollback       skip the safety timer (see below)
#
# Docker publishes container ports with its own firewall rules, which skip
# ufw, so the same allowlist is also applied in Docker's DOCKER-USER chain
# for traffic arriving on the public interface.
#
# Safety: unless --no-rollback, the firewall turns itself off again after
# 5 minutes. Check you can still open a new SSH session and the dashboard,
# then keep it with:  systemctl stop flobase-firewall-rollback.timer
# Running the script again is safe and applies the new options.
set -euo pipefail

DB_FROM=()
EXTRA_PORTS=()
ROLLBACK=1
DB_RANGE="32768:60999" # Docker's range for ports it picks

say() { printf '\033[1;33m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --db-from) DB_FROM+=("${2:?--db-from needs a CIDR}"); shift 2 ;;
    --allow-port) EXTRA_PORTS+=("${2:?--allow-port needs a port}"); shift 2 ;;
    --no-rollback) ROLLBACK=0; shift ;;
    *) die "unknown option $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run this as root"
command -v apt-get > /dev/null || die "this script supports Ubuntu and Debian"
for p in "${EXTRA_PORTS[@]}"; do
  case "$p" in '' | *[!0-9]*) die "--allow-port takes a port number, not $p" ;; esac
done
for c in "${DB_FROM[@]}"; do
  case "$c" in *[!0-9a-fA-F.:/]* | '') die "--db-from takes an address or CIDR, not $c" ;; esac
done
[ ${#DB_FROM[@]} -gt 0 ] || DB_FROM=("any")

# Never lock out the session running this: keep the port sshd listens on.
SSH_PORTS=$(sshd -T 2> /dev/null | awk '$1 == "port" {print $2}' | sort -u | tr '\n' ' ')
SSH_PORTS=${SSH_PORTS:-22}
EXT_IF=$(ip route get 1.1.1.1 2> /dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1)}' | head -n 1)
[ -n "$EXT_IF" ] || die "cannot find the public network interface"

if ! command -v ufw > /dev/null; then
  say "Installing ufw"
  apt-get update -qq && apt-get install -y -qq ufw > /dev/null
fi

say "Plan: SSH (${SSH_PORTS% }), HTTP 80, HTTPS 443, project databases ($DB_RANGE) from ${DB_FROM[*]}${EXTRA_PORTS[*]:+, and ${EXTRA_PORTS[*]}}"

ufw --force reset > /dev/null
ufw default deny incoming > /dev/null
ufw default allow outgoing > /dev/null
for p in $SSH_PORTS; do ufw allow "$p/tcp" comment "SSH" > /dev/null; done
ufw allow 80/tcp comment "HTTP" > /dev/null
ufw allow 443/tcp comment "HTTPS" > /dev/null
for p in "${EXTRA_PORTS[@]}"; do ufw allow "$p/tcp" comment "FloBase: allowed port" > /dev/null; done
# Database ports reached over IPv6 arrive through Docker's proxy, which ufw
# sees as ordinary incoming traffic.
for c in "${DB_FROM[@]}"; do
  if [ "$c" = any ]; then
    ufw allow "$DB_RANGE/tcp" comment "FloBase project databases" > /dev/null
  else
    ufw allow from "$c" to any port "$DB_RANGE" proto tcp comment "FloBase project databases" > /dev/null
  fi
done

# Docker-published ports: new connections from the public interface get the
# same allowlist; container traffic and replies are untouched.
cat > /usr/local/sbin/flobase-firewall-docker << EOF
#!/bin/sh
# Written by FloBase's firewall.sh; run after Docker starts
# (flobase-firewall-docker.service).
set -e
i=0
until iptables -L DOCKER-USER -n > /dev/null 2>&1; do
  i=\$((i + 1))
  [ "\$i" -le 30 ] || { echo "Docker's DOCKER-USER chain is missing: is Docker running?" >&2; exit 1; }
  sleep 1
done
iptables -N FLOBASE-FW 2> /dev/null || iptables -F FLOBASE-FW
for p in 80 443 $(printf '%s ' "${EXTRA_PORTS[@]}")$SSH_PORTS; do
  iptables -A FLOBASE-FW -p tcp -m conntrack --ctorigdstport "\$p" -j RETURN
done
$(for c in "${DB_FROM[@]}"; do
    if [ "$c" = any ]; then
      printf 'iptables -A FLOBASE-FW -p tcp -m conntrack --ctorigdstport %s -j RETURN\n' "$DB_RANGE"
    else
      printf 'iptables -A FLOBASE-FW -s %s -p tcp -m conntrack --ctorigdstport %s -j RETURN\n' "$c" "$DB_RANGE"
    fi
  done)
iptables -A FLOBASE-FW -j DROP
rule="-i $EXT_IF -m conntrack --ctstate NEW --ctdir ORIGINAL -j FLOBASE-FW"
# shellcheck disable=SC2086
iptables -C DOCKER-USER \$rule 2> /dev/null || iptables -I DOCKER-USER \$rule
EOF
chmod 755 /usr/local/sbin/flobase-firewall-docker
# The off switch the safety timer uses.
cat > /usr/local/sbin/flobase-firewall-off << 'EOF'
#!/bin/sh
# Written by FloBase's firewall.sh: turns the firewall off again.
ufw --force disable
iptables -D DOCKER-USER -i "$(ip route get 1.1.1.1 | awk '{for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1)}')" \
  -m conntrack --ctstate NEW --ctdir ORIGINAL -j FLOBASE-FW 2> /dev/null || true
EOF
chmod 755 /usr/local/sbin/flobase-firewall-off

if command -v systemctl > /dev/null && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/flobase-firewall-docker.service << 'EOF'
[Unit]
Description=FloBase firewall rules for Docker-published ports
After=docker.service
Requires=docker.service
PartOf=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/flobase-firewall-docker

[Install]
WantedBy=docker.service
EOF
  systemctl daemon-reload
  systemctl enable flobase-firewall-docker.service > /dev/null 2>&1
fi

if [ "$ROLLBACK" = 1 ] && command -v systemd-run > /dev/null; then
  systemctl stop flobase-firewall-rollback.timer 2> /dev/null || true
  systemd-run --quiet --unit flobase-firewall-rollback --on-active=300 /usr/local/sbin/flobase-firewall-off
fi

say "Turning the firewall on"
ufw --force enable > /dev/null
if command -v docker > /dev/null && docker info > /dev/null 2>&1; then
  if [ -d /run/systemd/system ]; then
    systemctl restart flobase-firewall-docker.service
  else
    /usr/local/sbin/flobase-firewall-docker
  fi
fi

ufw status numbered
if [ "$ROLLBACK" = 1 ]; then
  cat << 'EOF'

The firewall turns itself off again in 5 minutes. Check that you can open a
NEW SSH session and reach your sites, then keep it with:

  systemctl stop flobase-firewall-rollback.timer

To turn it off yourself at any time:  /usr/local/sbin/flobase-firewall-off
EOF
fi
