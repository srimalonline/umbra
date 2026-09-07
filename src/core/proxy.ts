/**
 * The reverse proxy: a single umbra-managed Caddy container. Caddy is chosen
 * because it issues and renews Let's Encrypt certificates automatically with
 * almost no configuration — a domain block with a `reverse_proxy` line is the
 * whole story.
 *
 * Routing model: Caddy and every app container share one external bridge
 * network (`umbra`). An app joins it with its name as a network alias, so a
 * route is simply `domain -> reverse_proxy <app>:<port>` — no host ports, no
 * per-app port juggling.
 *
 * DNS + ports: for SSL to issue, the domain's A/AAAA record must point at this
 * server and TCP 80 + 443 must be open to the internet. That is a fact about
 * the host, documented in the README; umbra cannot verify it from here.
 */
import {
  UMBRA_NETWORK,
  PROXY_CONTAINER,
  PROXY_IMAGE,
  CADDY_DATA_VOLUME,
  CADDY_CONFIG_VOLUME,
  caddyfilePath,
  proxyConfigPath,
  proxyDir,
} from "./paths.js";
import type { Deps, ProxyConfig, Route } from "./types.js";
import { validateDomain, validatePort } from "../safety.js";

/** Render a full Caddyfile from the route table. Pure — unit-tested directly. */
export function renderCaddyfile(config: ProxyConfig): string {
  const out: string[] = [];
  out.push(`# Managed by umbra. Do not edit by hand — regenerated on every route change.`);
  if (config.email) {
    out.push(``, `{`, `\temail ${config.email}`, `}`);
  }
  if (config.routes.length === 0) {
    out.push(``, `# No routes yet. Attach a domain: umbra domain <app> <domain> --port <n>`);
  }
  for (const r of [...config.routes].sort((a, b) => a.domain.localeCompare(b.domain))) {
    out.push(``, `${r.domain} {`, `\treverse_proxy ${r.app}:${r.port}`, `}`);
  }
  return out.join("\n") + "\n";
}

export function loadProxyConfig(deps: Deps): ProxyConfig {
  const p = proxyConfigPath(deps.home);
  if (!deps.fs.exists(p)) return { routes: [] };
  try {
    const parsed = JSON.parse(deps.fs.readFile(p)) as ProxyConfig;
    return { email: parsed.email, routes: Array.isArray(parsed.routes) ? parsed.routes : [] };
  } catch {
    return { routes: [] };
  }
}

export function saveProxyConfig(deps: Deps, config: ProxyConfig): void {
  deps.fs.mkdirp(proxyDir(deps.home));
  deps.fs.writeFile(proxyConfigPath(deps.home), JSON.stringify(config, null, 2) + "\n");
  deps.fs.writeFile(caddyfilePath(deps.home), renderCaddyfile(config));
}

/** True if a container with the given name exists (any state). */
async function containerExists(deps: Deps, name: string): Promise<boolean> {
  const r = await deps.exec("docker", ["ps", "-a", "--filter", `name=^/${name}$`, "--format", "{{.Names}}"]);
  return r.stdout.split("\n").map((s) => s.trim()).includes(name);
}

/** True if the container is currently running. */
async function containerRunning(deps: Deps, name: string): Promise<boolean> {
  const r = await deps.exec("docker", ["ps", "--filter", `name=^/${name}$`, "--format", "{{.Names}}"]);
  return r.stdout.split("\n").map((s) => s.trim()).includes(name);
}

/** Create the shared umbra network if it is missing. Idempotent. */
export async function ensureNetwork(deps: Deps): Promise<void> {
  const r = await deps.exec("docker", ["network", "ls", "--format", "{{.Name}}"]);
  const names = r.stdout.split("\n").map((s) => s.trim());
  if (!names.includes(UMBRA_NETWORK)) {
    await deps.exec("docker", ["network", "create", UMBRA_NETWORK]);
  }
}

/**
 * Ensure the Caddy proxy container is up. Writes an initial Caddyfile if none
 * exists, then starts (or restarts) the container. Idempotent: a running proxy
 * is left alone.
 */
export async function ensureProxy(deps: Deps): Promise<{ started: boolean; note: string }> {
  await ensureNetwork(deps);
  const config = loadProxyConfig(deps);
  // Always keep the on-disk Caddyfile in sync with the route table.
  saveProxyConfig(deps, config);

  if (await containerRunning(deps, PROXY_CONTAINER)) {
    return { started: false, note: "proxy already running" };
  }
  if (await containerExists(deps, PROXY_CONTAINER)) {
    await deps.exec("docker", ["start", PROXY_CONTAINER]);
    return { started: true, note: "proxy container restarted" };
  }
  await deps.exec("docker", [
    "run",
    "-d",
    "--name",
    PROXY_CONTAINER,
    "--restart",
    "unless-stopped",
    "--network",
    UMBRA_NETWORK,
    "-p",
    "80:80",
    "-p",
    "443:443",
    "-v",
    `${caddyfilePath(deps.home)}:/etc/caddy/Caddyfile`,
    "-v",
    `${CADDY_DATA_VOLUME}:/data`,
    "-v",
    `${CADDY_CONFIG_VOLUME}:/config`,
    PROXY_IMAGE,
  ]);
  return { started: true, note: "proxy container created" };
}

/** Ask the running Caddy to reload its config in place — no downtime. */
export async function reloadProxy(deps: Deps): Promise<void> {
  if (!(await containerRunning(deps, PROXY_CONTAINER))) return;
  await deps.exec("docker", [
    "exec",
    PROXY_CONTAINER,
    "caddy",
    "reload",
    "--config",
    "/etc/caddy/Caddyfile",
    "--adapter",
    "caddyfile",
  ]);
}

/** Add or replace the route for a domain, then persist + reload. */
export async function addRoute(deps: Deps, route: Route): Promise<void> {
  validateDomain(route.domain);
  validatePort(route.port);
  const config = loadProxyConfig(deps);
  const routes = config.routes.filter((r) => r.domain !== route.domain);
  routes.push(route);
  const next: ProxyConfig = { ...config, routes };
  saveProxyConfig(deps, next);
  await ensureProxy(deps);
  await reloadProxy(deps);
}

/** Remove every route pointing at an app (used on detach / remove). */
export async function removeRoutesForApp(deps: Deps, app: string): Promise<Route[]> {
  const config = loadProxyConfig(deps);
  const removed = config.routes.filter((r) => r.app === app);
  if (removed.length === 0) return [];
  const next: ProxyConfig = { ...config, routes: config.routes.filter((r) => r.app !== app) };
  saveProxyConfig(deps, next);
  await reloadProxy(deps);
  return removed;
}

/** Remove a single domain route. */
export async function removeRoute(deps: Deps, domain: string): Promise<boolean> {
  const config = loadProxyConfig(deps);
  const before = config.routes.length;
  const next: ProxyConfig = { ...config, routes: config.routes.filter((r) => r.domain !== domain) };
  if (next.routes.length === before) return false;
  saveProxyConfig(deps, next);
  await reloadProxy(deps);
  return true;
}
