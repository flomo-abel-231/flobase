#!/usr/bin/env bash
# Prepares a fresh Ubuntu or Debian server (Hetzner, Vultr, any VPS) to be a
# FloBase region server: installs Docker, adds a `flobase` user that the
# control plane signs in as over SSH, and lets only the control plane reach
# the relay port. Run it as root on the new server:
#
#   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/region-server.sh | \
#     bash -s -- --control-plane 203.0.113.5 --key "ssh-ed25519 AAAA… flobase"
#
# Options:
#   --control-plane IP   address of the FloBase control plane (required);
#                        repeat for more than one
#   --key PUBLIC_KEY     public half of the key FloBase signs in with (required)
#   --relay-port PORT    relay port to allow (default: 28474)
#   --user NAME          user FloBase signs in as (default: flobase)
#
# It prints what to paste into Regions → Add region server. Running it again
# is safe.
set -euo pipefail

RELAY_PORT=28474
USER_NAME=flobase
KEY=""
CONTROL_PLANES=()

say() { printf '\033[1;33m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --control-plane) CONTROL_PLANES+=("${2:?--control-plane needs an IP}"); shift 2 ;;
    --key) KEY="${2:?--key needs a public key}"; shift 2 ;;
    --relay-port) RELAY_PORT="${2:?--relay-port needs a port}"; shift 2 ;;
    --user) USER_NAME="${2:?--user needs a name}"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run this as root"
[ ${#CONTROL_PLANES[@]} -gt 0 ] || die "--control-plane <IP of your FloBase> is required"
[ -n "$KEY" ] || die "--key \"ssh-ed25519 AAAA…\" is required"
case "$KEY" in ssh-*|ecdsa-*) ;; *) die "--key must be a public key such as ssh-ed25519 AAAA…" ;; esac
case "$RELAY_PORT" in ''|*[!0-9]*) die "--relay-port must be a number" ;; esac
for ip in "${CONTROL_PLANES[@]}"; do
  case "$ip" in *[!0-9.]*|'') die "--control-plane takes an IPv4 address, not $ip" ;; esac
done
command -v apt-get >/dev/null || die "this script supports Ubuntu and Debian"

if ! command -v docker >/dev/null; then
  say "Installing Docker"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl iptables >/dev/null
  curl -fsSL https://get.docker.com | sh >/dev/null
else
  say "Docker is installed: $(docker --version)"
fi
if command -v systemctl >/dev/null; then systemctl enable --now docker >/dev/null 2>&1 || true; fi

say "Adding the $USER_NAME user"
id "$USER_NAME" >/dev/null 2>&1 || useradd -m -s /bin/bash "$USER_NAME"
# Docker group membership is root on this machine: this user is FloBase's.
usermod -aG docker "$USER_NAME"
home=$(getent passwd "$USER_NAME" | cut -d: -f6)
install -d -m 700 -o "$USER_NAME" -g "$USER_NAME" "$home/.ssh"
touch "$home/.ssh/authorized_keys"
grep -qxF "$KEY" "$home/.ssh/authorized_keys" || printf '%s\n' "$KEY" >> "$home/.ssh/authorized_keys"
chown "$USER_NAME:$USER_NAME" "$home/.ssh/authorized_keys"
chmod 600 "$home/.ssh/authorized_keys"

say "Turning off SSH password sign-in"
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/10-flobase.conf <<'EOF'
# Written by FloBase's region-server.sh: keys only.
PasswordAuthentication no
KbdInteractiveAuthentication no
EOF
if command -v systemctl >/dev/null; then
  systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null || true
fi

# Docker publishes container ports with its own firewall rules, which skip
# ufw, so the relay port is limited in Docker's DOCKER-USER chain instead.
# Customers' database ports stay open; they are protected by passwords.
say "Limiting relay port $RELAY_PORT to ${CONTROL_PLANES[*]}"
cat > /usr/local/sbin/flobase-relay-firewall <<EOF
#!/bin/sh
# Written by FloBase's region-server.sh. Only the control plane may reach
# the relay; run after Docker starts (flobase-relay-firewall.service).
set -e
# Docker creates DOCKER-USER when it starts.
i=0
until iptables -L DOCKER-USER -n >/dev/null 2>&1; do
  i=\$((i + 1))
  [ "\$i" -le 30 ] || { echo "Docker's DOCKER-USER chain is missing: is Docker running?" >&2; exit 1; }
  sleep 1
done
iptables -N FLOBASE-RELAY 2>/dev/null || iptables -F FLOBASE-RELAY
$(for ip in "${CONTROL_PLANES[@]}"; do printf 'iptables -A FLOBASE-RELAY -s %s -j RETURN\n' "$ip"; done)
iptables -A FLOBASE-RELAY -j DROP
# Original direction only: replies from the relay carry the same port.
iptables -C DOCKER-USER -p tcp -m conntrack --ctorigdstport $RELAY_PORT --ctdir ORIGINAL -j FLOBASE-RELAY 2>/dev/null ||
  iptables -I DOCKER-USER -p tcp -m conntrack --ctorigdstport $RELAY_PORT --ctdir ORIGINAL -j FLOBASE-RELAY
# IPv6 reaches published ports through Docker's proxy; the control plane
# uses IPv4, so the relay port is closed there.
if command -v ip6tables >/dev/null; then
  ip6tables -C INPUT -p tcp --dport $RELAY_PORT -j DROP 2>/dev/null ||
    ip6tables -I INPUT -p tcp --dport $RELAY_PORT -j DROP || true
fi
EOF
chmod 755 /usr/local/sbin/flobase-relay-firewall
if command -v systemctl >/dev/null && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/flobase-relay-firewall.service <<'EOF'
[Unit]
Description=Limit the FloBase relay port to the control plane
After=docker.service
Requires=docker.service
PartOf=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/flobase-relay-firewall

[Install]
WantedBy=docker.service
EOF
  systemctl daemon-reload
  systemctl enable flobase-relay-firewall.service >/dev/null 2>&1
  systemctl restart flobase-relay-firewall.service
else
  /usr/local/sbin/flobase-relay-firewall
fi

ip=$(curl -fsS4 --max-time 5 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
hostkey=$(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)

cat <<EOF

$(say "Done. In FloBase, open Regions → Add region server and enter:")
  Transport      SSH
  SSH endpoint   ssh://$USER_NAME@$ip
  Private key    the private half of the key you passed with --key
  Host key       $hostkey
  Public host    a DNS name pointing at $ip (or $ip itself)
  Relay port     $RELAY_PORT

In your provider's firewall (Hetzner Cloud Firewall, Vultr Firewall), allow:
  TCP 22                  SSH, from ${CONTROL_PLANES[*]} and your own IP
  TCP $RELAY_PORT               relay, from ${CONTROL_PLANES[*]}
  TCP 32768-60999         customers' database ports, from anywhere
EOF
