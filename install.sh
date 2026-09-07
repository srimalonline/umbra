#!/bin/sh
# umbra installer — headless platform, no ui, ever.
# Idempotent. Safe to re-run. Read this script before piping it to a shell.
#
#   curl -fsSL https://raw.githubusercontent.com/srimalonline/umbra/main/install.sh | sh
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
| | | | '_ ` _ \| '_ \| '__/ _` |
| |_| | | | | | | |_) | | | (_| |
 \__,_|_| |_| |_|_.__/|_|  \__,_|
        headless platform · no ui, ever
EOF
}

banner

OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
  say "! umbra is a server tool and expects Linux; you're on $OS."
  ask "Continue anyway?" || { say "Aborted."; exit 1; }
fi

# --- Docker
if ! command -v docker >/dev/null 2>&1; then
  if [ "$INSTALL_DOCKER" = "1" ] && ask "Docker not found. Install it via https://get.docker.com?"; then
    curl -fsSL https://get.docker.com | sh
  else
    say "Docker is required. Install it, then re-run. Aborting."
    exit 1
  fi
fi
say "✓ docker: $(docker --version 2>/dev/null || echo present)"

# --- Node 20+
NODE_OK=0
if command -v node >/dev/null 2>&1; then
  MAJ="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
  [ "$MAJ" -ge 20 ] 2>/dev/null && NODE_OK=1
fi
if [ "$NODE_OK" = "0" ]; then
  say "! Node 20+ is required and was not found."
  say "  Install it, e.g.:  curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash - && sudo apt-get install -y nodejs"
  say "  Then re-run this installer."
  exit 1
fi
say "✓ node: $(node --version)"

# --- umbra itself
mkdir -p "${UMBRA_HOME:-$HOME/.umbra}"

# From a clone (the supported path while the repo is private): build + link.
if [ -f "./package.json" ] && grep -q '"name": "umbra"' package.json 2>/dev/null; then
  say "Installing umbra from this checkout…"
  npm install
  npm run build
  npm link
else
  # Public path (once umbra is published): npm i -g umbra
  say "Installing umbra from npm…"
  npm install -g umbra
fi
say "✓ umbra: $(umbra --version 2>/dev/null | tail -1 || echo installed)"

# --- bring up the proxy
say "Bringing up the umbra proxy (Caddy)…"
umbra init || say "! 'umbra init' hit an issue — run it manually and check docker permissions."

say ""
say "Done. Deploy something:"
say "  umbra deploy hello --image nginxdemos/hello --domain hello.yourdomain.com"
say "  (point the domain's DNS at this server first; ports 80 and 443 must be open)"
