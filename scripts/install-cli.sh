#!/bin/sh
# Installs the flobase CLI on macOS (Intel or Apple silicon) or Linux, then
# starts FloBase, which installs a Docker engine first if there is none.
#
#   curl -fsSL https://raw.githubusercontent.com/flomo-abel-231/flobase/main/scripts/install-cli.sh | sh
#
# Options, as variables:
#   FLOBASE_VERSION    release to install, e.g. 0.2.0 (default: latest)
#   FLOBASE_BIN_DIR    where to put flobase (default: ~/.local/bin)
#   FLOBASE_NO_START   set to 1 to install the CLI only
set -eu

REPO="${FLOBASE_UPDATE_REPO:-flomo-abel-231/flobase}"
VERSION="${FLOBASE_VERSION:-latest}"
BIN_DIR="${FLOBASE_BIN_DIR:-$HOME/.local/bin}"

say() { printf '\033[1;33m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "This script is for macOS and Linux. On Windows, use install-cli.ps1." ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "No flobase build for $(uname -m)." ;;
esac
command -v curl > /dev/null || die "curl is required."

if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/v${VERSION#v}"
fi
file="flobase_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading flobase for $os/$arch"
curl -fsSL "$base/$file" -o "$tmp/$file" || die "Could not download $file."
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || die "Could not download checksums.txt."
expected="$(grep " $file\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
if command -v sha256sum > /dev/null; then
  actual="$(sha256sum "$tmp/$file" | cut -d' ' -f1)"
else
  actual="$(shasum -a 256 "$tmp/$file" | cut -d' ' -f1)"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  die "Checksum mismatch for $file."
fi

tar -xzf "$tmp/$file" -C "$tmp"
mkdir -p "$BIN_DIR"
mv "$tmp/flobase" "$BIN_DIR/flobase"
chmod +x "$BIN_DIR/flobase"
say "Installed $("$BIN_DIR/flobase" version) to $BIN_DIR/flobase"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    profile="$HOME/.profile"
    case "${SHELL:-}" in
      */zsh) profile="$HOME/.zshrc" ;;
      */bash) profile="$HOME/.bashrc" ;;
    esac
    # $PATH stays literal: it expands when the shell starts.
    # shellcheck disable=SC2016
    printf '\nexport PATH="%s:$PATH"\n' "$BIN_DIR" >> "$profile"
    say "Added $BIN_DIR to PATH in $profile; open a new terminal to use flobase by name"
    ;;
esac

if [ "${FLOBASE_NO_START:-}" = 1 ]; then
  echo "Run 'flobase start' to set up and start FloBase."
  exit 0
fi
# flobase asks before installing anything that needs your agreement; the
# terminal is reattached so it can ask even when piped from curl.
if [ -r /dev/tty ]; then
  "$BIN_DIR/flobase" start < /dev/tty
else
  FLOBASE_YES=1 "$BIN_DIR/flobase" start
fi
