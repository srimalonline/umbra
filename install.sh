#!/bin/sh
# umbra installer — AI-native PaaS, your agent is the dashboard.
# Idempotent. Safe to re-run. Read this script before piping it to a shell.
#
#   curl -fsSL https://raw.githubusercontent.com/srimalonline/umbra/main/install.sh | sh
#
# umbra is a single static Go binary — no Node, no runtime to install on the box.
# The installer downloads a prebuilt binary from GitHub releases when one exists;
# otherwise (e.g. run from a git checkout) it builds from source with `go build`.
#
# Flags (pass after `| sh -s --`):  --yes  skip prompts   --no-docker  don't install docker
set -eu

YES=0
INSTALL_DOCKER=1
for arg in "$@"; do
  case "$arg" in
    --yes) YES=1 ;;
    --no-docker) INSTALL_DOCKER=0 ;;
  esac
done

REPO="srimalonline/umbra"
PREFIX="${PREFIX:-/usr/local}"
BIN_DIR="$PREFIX/bin"

say() { printf '%s\n' "$*"; }
ask() {
  # ask "question" -> 0 if yes. Auto-yes under --yes or a non-interactive pipe.
  [ "$YES" = "1" ] && return 0
  [ -t 0 ] || return 0
  printf '%s [Y/n] ' "$1"; read -r a || true
  case "$a" in n*|N*) return 1 ;; *) return 0 ;; esac
}

banner() {
  cat <<'EOF'

 _   _ _ __ ___ | |__  _ __ __ _
| | | | '_ \` _ \| '_ \| '__/ _\` |
| |_| | | | | | | |_) | | | (_| |
 \__,_|_| |_| |_|_.__/|_|  \__,_|
        AI-native PaaS · your agent is the dashboard
EOF
}

# sudo only when we can't already write the install dir.
maybe_sudo() {
  if [ -w "$BIN_DIR" ] 2>/dev/null; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    "$@"
  fi
}

banner

OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
  say "! umbra is a server tool and expects Linux; you're on $OS."
  ask "Continue anyway?" || { say "Aborted."; exit 1; }
fi

# --- Docker (the one real runtime dependency; umbra itself needs nothing else)
if ! command -v docker >/dev/null 2>&1; then
  if [ "$INSTALL_DOCKER" = "1" ] && ask "Docker not found. Install it via https://get.docker.com?"; then
    curl -fsSL https://get.docker.com | sh
  else
    say "Docker is required. Install it, then re-run. Aborting."
    exit 1
  fi
fi
say "✓ docker: $(docker --version 2>/dev/null || echo present)"

# --- Resolve OS/arch to a release asset name.
uname_arch="$(uname -m)"
case "$uname_arch" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) ARCH="$uname_arch" ;;
esac
case "$OS" in
  Linux) GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  *) GOOS="$(echo "$OS" | tr '[:upper:]' '[:lower:]')" ;;
esac
ASSET="umbra-${GOOS}-${ARCH}"

install_binary() {  # install_binary <path-to-built-or-downloaded-binary>
  mkdir -p "$BIN_DIR"
  maybe_sudo install -m 0755 "$1" "$BIN_DIR/umbra"
}

INSTALLED=0

# 1. Prefer a prebuilt release binary (none exist yet while the repo is private;
#    this path lights up automatically once a release is published).
if [ "$INSTALLED" = "0" ] && command -v curl >/dev/null 2>&1; then
  URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
  TMP="$(mktemp)"
  if curl -fsSL "$URL" -o "$TMP" 2>/dev/null && [ -s "$TMP" ]; then
    say "Downloaded prebuilt umbra ($ASSET)."
    install_binary "$TMP"
    INSTALLED=1
  fi
  rm -f "$TMP"
fi

# 2. Build from a local checkout (the supported path while the repo is private).
if [ "$INSTALLED" = "0" ] && [ -f "./go.mod" ] && grep -q 'module github.com/srimalonline/umbra' go.mod 2>/dev/null; then
  if ! command -v go >/dev/null 2>&1; then
    say "! No prebuilt binary was available and Go is not installed to build from source."
    say "  Install Go 1.25+ (https://go.dev/dl/) and re-run, or download a release binary."
    exit 1
  fi
  say "Building umbra from this checkout with 'go build'…"
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o umbra .
  install_binary ./umbra
  INSTALLED=1
fi

if [ "$INSTALLED" = "0" ]; then
  say "Could not install umbra: no release binary for ${ASSET}, and no local checkout to build from."
  say "Clone the repo and re-run this script from inside it, or install Go and build with 'go build -o umbra'."
  exit 1
fi

say "✓ umbra: $(umbra version 2>/dev/null | tail -1 || echo installed)"

# --- bring up the proxy
say "Bringing up the umbra proxy (Caddy)…"
umbra init || say "! 'umbra init' hit an issue — run it manually and check docker permissions."

say ""
say "Done. Deploy something:"
say "  umbra deploy hello --image nginxdemos/hello --domain hello.yourdomain.com"
say "  (point the domain's DNS at this server first; ports 80 and 443 must be open)"
