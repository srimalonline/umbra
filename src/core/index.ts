/**
 * The umbra core: the app lifecycle, assembled from the compose, proxy and
 * path primitives. Everything here is pure orchestration over the injectable
 * `Deps` (exec + fs), so the whole platform is testable without a Docker
 * daemon. The MCP layer and the CLI are thin skins over these functions.
 *
 * An "app" is a docker-compose project living under ~/.umbra/apps/<name>/:
 *   docker-compose.yml    the stack (synthesized from an image, or the user's)
 *   umbra-network.yml     generated override joining a service to the proxy net
 *   umbra.json            umbra's metadata for the app
 */
import {
  appComposePath,
  appDir,
  appMetaPath,
  appNetworkOverridePath,
  appsDir,
  projectName,
} from "./paths.js";
import { synthesizeImageCompose, networkOverride } from "./compose.js";
import {
  addRoute,
  ensureProxy,
  loadProxyConfig,
  removeRoutesForApp,
  removeRoute,
} from "./proxy.js";
import type { AppMeta, Deps } from "./types.js";
import { sanitizeName, validateDomain, validatePort, capOutput, clampLines } from "../safety.js";

export * from "./types.js";
export { ensureProxy, loadProxyConfig, renderCaddyfile } from "./proxy.js";

export interface DeployArgs {
  name: string;
  /** One of these three defines the app. */
  image?: string;
  composeYaml?: string;
  composePath?: string;
  /** Attach a domain in the same call (optional). */
  domain?: string;
  port?: number;
  /** Compose service that carries the domain; defaults to the app name. */
  service?: string;
}

function readMeta(deps: Deps, name: string): AppMeta | undefined {
  const p = appMetaPath(deps.home, name);
  if (!deps.fs.exists(p)) return undefined;
  try {
    return JSON.parse(deps.fs.readFile(p)) as AppMeta;
  } catch {
    return undefined;
  }
}

function writeMeta(deps: Deps, meta: AppMeta): void {
  deps.fs.mkdirp(appDir(deps.home, meta.name));
  deps.fs.writeFile(appMetaPath(deps.home, meta.name), JSON.stringify(meta, null, 2) + "\n");
}

/** Build the `-f` argument list for an app: base compose, plus the network override when it exists. */
function composeFiles(deps: Deps, name: string): string[] {
  const files = ["-f", appComposePath(deps.home, name)];
  if (deps.fs.exists(appNetworkOverridePath(deps.home, name))) {
    files.push("-f", appNetworkOverridePath(deps.home, name));
  }
  return files;
}

async function composeCmd(deps: Deps, name: string, args: string[], timeoutMs?: number) {
  return deps.exec(
    "docker",
    ["compose", "-p", projectName(name), ...composeFiles(deps, name), ...args],
    { cwd: appDir(deps.home, name), timeoutMs }
  );
}

/**
 * Deploy (or redeploy) an app: writes its compose, brings it up, and attaches a
 * domain route when one is given. Idempotent — a second deploy updates in place.
 */
export async function deploy(deps: Deps, args: DeployArgs): Promise<Record<string, unknown>> {
  const name = sanitizeName(args.name);
  const service = args.service ? sanitizeName(args.service) : name;

  const sources = [args.image, args.composeYaml, args.composePath].filter(Boolean);
  if (sources.length !== 1) {
    throw new Error("deploy needs exactly one of: image, composeYaml, composePath.");
  }

  await ensureProxy(deps);
  deps.fs.mkdirp(appDir(deps.home, name));

  // 1. Write the app's compose.
  let source: AppMeta["source"];
  if (args.image) {
    deps.fs.writeFile(appComposePath(deps.home, name), synthesizeImageCompose(name, args.image, args.port));
    source = "image";
  } else {
    const yaml = args.composeYaml ?? deps.fs.readFile(args.composePath!);
    deps.fs.writeFile(appComposePath(deps.home, name), yaml);
    source = "compose";
  }

  // 2. Domain routing: write the network override so the service is reachable
  //    from Caddy by the app's alias, then (after up) register the route.
  const domain = args.domain ? validateDomain(args.domain) : undefined;
  const port = domain ? validatePort(args.port ?? 80) : args.port;
  if (domain) {
    deps.fs.writeFile(appNetworkOverridePath(deps.home, name), networkOverride(name, service));
  }

  // 3. Bring it up.
  const up = await composeCmd(deps, name, ["up", "-d", "--remove-orphans"], 300_000);
  if (up.exitCode !== 0) {
    throw new Error(`docker compose up failed (exit ${up.exitCode}): ${capOutput(up.stderr || up.stdout)}`);
  }

  // 4. Persist metadata.
  const now = new Date().toISOString();
  const prev = readMeta(deps, name);
  const meta: AppMeta = {
    name,
    source,
    image: args.image ?? prev?.image,
    domain: domain ?? prev?.domain,
    port: port ?? prev?.port,
    service,
    createdAt: prev?.createdAt ?? now,
    updatedAt: now,
  };
  writeMeta(deps, meta);

  // 5. Register the route (after the container is up so Caddy can resolve it).
  if (domain) await addRoute(deps, { domain, app: name, port: port! });

  return {
    deployed: name,
    source,
    domain: meta.domain,
    port: meta.port,
    url: meta.domain ? `https://${meta.domain}` : undefined,
    output: capOutput(up.stdout + up.stderr),
    note: meta.domain
      ? "Domain attached. SSL issues automatically once the domain's DNS points at this server and ports 80/443 are open."
      : "No domain attached; reach it on the umbra network or attach one with umbra_domain.",
  };
}

/** List every umbra app with its recorded metadata and live compose status. */
export async function list(deps: Deps): Promise<Record<string, unknown>> {
  const dir = appsDir(deps.home);
  const names = deps.fs.readdir(dir).filter((n) => deps.fs.exists(appMetaPath(deps.home, n)));
  const apps = await Promise.all(
    names.map(async (name) => {
      const meta = readMeta(deps, name);
      let running = 0;
      let total = 0;
      try {
        const ps = await composeCmd(deps, name, ["ps", "--format", "{{.Name}}\t{{.State}}"]);
        const rows = ps.stdout.split("\n").map((s) => s.trim()).filter(Boolean);
        total = rows.length;
        running = rows.filter((r) => /running|up/i.test(r)).length;
      } catch {
        /* status best-effort */
      }
      return {
        name,
        domain: meta?.domain,
        source: meta?.source,
        status: total === 0 ? "stopped" : running === total ? "running" : `${running}/${total} up`,
        url: meta?.domain ? `https://${meta.domain}` : undefined,
      };
    })
  );
  return { apps, count: apps.length };
}

export async function status(deps: Deps, name: string): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  const meta = readMeta(deps, n);
  if (!meta) throw new Error(`No umbra app named "${n}".`);
  const ps = await composeCmd(deps, n, ["ps", "--format", "table {{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}"]);
  return { name: n, meta, ps: capOutput(ps.stdout || ps.stderr) };
}

export async function logs(deps: Deps, name: string, lines?: number): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  if (!readMeta(deps, n)) throw new Error(`No umbra app named "${n}".`);
  const tail = clampLines(lines);
  const res = await composeCmd(deps, n, ["logs", "--no-color", "--tail", String(tail)]);
  return { name: n, lines: tail, logs: capOutput(res.stdout + res.stderr) };
}

export async function control(
  deps: Deps,
  name: string,
  action: "restart" | "stop" | "start"
): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  if (!readMeta(deps, n)) throw new Error(`No umbra app named "${n}".`);
  const res = await composeCmd(deps, n, [action], 120_000);
  if (res.exitCode !== 0) throw new Error(`${action} failed (exit ${res.exitCode}): ${capOutput(res.stderr)}`);
  return { name: n, action, output: capOutput(res.stdout + res.stderr) };
}

/**
 * Remove an app: `docker compose down` (keeps named volumes unless purge),
 * drops its proxy routes, and deletes its ~/.umbra/apps/<name> dir.
 */
export async function remove(
  deps: Deps,
  name: string,
  opts: { purge?: boolean } = {}
): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  if (!readMeta(deps, n)) throw new Error(`No umbra app named "${n}".`);
  const downArgs = ["down", "--remove-orphans"];
  if (opts.purge) downArgs.push("-v"); // volumes destroyed ONLY on explicit purge
  const res = await composeCmd(deps, n, downArgs, 120_000);
  const routes = await removeRoutesForApp(deps, n);
  deps.fs.rmrf(appDir(deps.home, n));
  return {
    removed: n,
    volumes: opts.purge ? "purged" : "kept",
    routesDropped: routes.map((r) => r.domain),
    output: capOutput(res.stdout + res.stderr),
  };
}

/** Attach a domain to an existing app (or move it) and route it through Caddy. */
export async function attachDomain(
  deps: Deps,
  name: string,
  domain: string,
  port = 80,
  service?: string
): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  const meta = readMeta(deps, n);
  if (!meta) throw new Error(`No umbra app named "${n}".`);
  const d = validateDomain(domain);
  const p = validatePort(port);
  const svc = service ? sanitizeName(service) : meta.service ?? n;

  // Ensure the app is joined to the umbra network, then bring it up so the
  // alias exists before Caddy is told to route to it.
  deps.fs.writeFile(appNetworkOverridePath(deps.home, n), networkOverride(n, svc));
  await composeCmd(deps, n, ["up", "-d", "--remove-orphans"], 300_000);
  await addRoute(deps, { domain: d, app: n, port: p });
  writeMeta(deps, { ...meta, domain: d, port: p, service: svc, updatedAt: new Date().toISOString() });
  return { app: n, domain: d, port: p, url: `https://${d}` };
}

/** Detach a domain from an app (removes the Caddy route; app keeps running). */
export async function detachDomain(deps: Deps, name: string, domain?: string): Promise<Record<string, unknown>> {
  const n = sanitizeName(name);
  const meta = readMeta(deps, n);
  if (!meta) throw new Error(`No umbra app named "${n}".`);
  const target = domain ? validateDomain(domain) : meta.domain;
  if (!target) throw new Error(`App "${n}" has no domain attached.`);
  const dropped = await removeRoute(deps, target);
  if (meta.domain === target) writeMeta(deps, { ...meta, domain: undefined, updatedAt: new Date().toISOString() });
  return { app: n, detached: target, removed: dropped };
}
