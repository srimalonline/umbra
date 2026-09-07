```
 _   _ _ __ ___ | |__  _ __ __ _
| | | | '_ ` _ \| '_ \| '__/ _` |
| |_| | | | | | | |_) | | | (_| |
 \__,_|_| |_| |_|_.__/|_|  \__,_|
        headless platform · no ui, ever
```

# umbra

A **headless PaaS**. Coolify without the dashboard — the agent is the dashboard.

Umbra runs on your server and deploys docker-compose apps behind a reverse proxy with automatic SSL. It has **no web UI**. Its only two control surfaces are an **MCP layer** (so an AI agent drives it) and a **CLI** (so you do). Same core, two doors.

The name: the *umbra* is the innermost part of a shadow, where the light is fully blocked — nothing to see, on purpose.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/srimalonline/umbra/main/install.sh | sh
```

The installer ensures Docker and Node 20+, installs umbra, and brings up the proxy. Pass options after `-s --`, e.g. `| sh -s -- --yes`.

> **Read any script before you pipe it to a shell** — this one included.
>
> **While this repo is private**, that raw URL will not resolve. Install from a clone instead:
> ```sh
> git clone git@github.com:srimalonline/umbra.git && cd umbra && ./install.sh
> ```

## Use it — two doors, one platform

**CLI (you):**
```sh
umbra init
umbra deploy blog --image ghost:5 --domain blog.example.com --port 2368
umbra ls
umbra logs blog --lines 100
umbra domain blog blog2.example.com --port 2368     # attach another domain
umbra restart blog
umbra rm blog --yes                                  # volumes kept unless --purge
```

**MCP (an agent):** register once, then just ask it to deploy things.
```sh
claude mcp add umbra -- node "$(pwd)/dist/mcp/index.js"
```
Tools: `umbra_deploy`, `umbra_apps`, `umbra_status`, `umbra_logs`, `umbra_control`, `umbra_domain`, `umbra_remove`.

## Architecture

```
        you ──▶ umbra CLI ─┐
                           ├─▶  core  ──▶ docker / docker compose ──▶ your apps
   an agent ──▶ umbra MCP ─┘      │
                                  └─────▶ Caddy (one container) ──▶ auto-SSL routing
```

- An **app** is a docker-compose project under `~/.umbra/apps/<name>/`.
- One **Caddy** container is the reverse proxy; apps share an `umbra` bridge network and Caddy routes `domain → app:port`. Caddy issues and renews Let's Encrypt certificates automatically.
- **For SSL to work**: the domain's DNS must point at this server, and TCP **80 + 443** must be open to the internet. Umbra can't verify that from inside the box — it's a fact about your host and DNS.

## What's in v0 — and what isn't

| In v0 | Not yet (roadmap) |
|---|---|
| curl installer, Docker + Caddy setup | git-push deploys (push to deploy) |
| deploy from image or compose | managed databases as a service |
| domains + automatic SSL | scheduled backups / snapshots |
| logs, status, restart/stop/start | multiple servers / a fleet |
| remove (volumes kept unless purged) | metrics history, alerting |
| MCP layer + CLI, both complete | build-from-Dockerfile / buildpacks |

v0 is an honest working skeleton: it really deploys and runs apps with SSL, headless. It is **not** Coolify-parity — the roadmap items are the years Coolify spent hardening, and they come next, not first.

## Safety rails (in code, not just documented)

- App names are sanitized to `[a-z0-9-]` at one choke point — nothing reaches a shell or a path unescaped.
- Domains and ports are validated before they enter a Caddyfile (no directive injection).
- `umbra_remove` is **confirm-gated** and **never destroys volumes** unless you explicitly pass `purge` — data loss is always a deliberate act.
- Output is capped; logs are clamped (max 1000 lines).
- No secrets in the repo. Caddy's ACME account and certificate data live under `~/.umbra`, outside git.

## Develop

```sh
npm install && npm run build && npm test
```

MIT licensed.
