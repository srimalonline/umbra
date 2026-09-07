/** Shared types and the injectable side-effect boundary for umbra's core. */

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface ExecOptions {
  cwd?: string;
  /** Piped to the process stdin (used to feed compose YAML to `docker compose -f -`). */
  input?: string;
  timeoutMs?: number;
}

/**
 * The one impure boundary the core shells through. Real implementation wraps
 * execa; tests inject a recording mock. Everything the core does to the host
 * — docker, docker compose, docker network — goes through here, so a test can
 * assert the exact argv without a Docker daemon in sight.
 */
export type Exec = (file: string, args: string[], opts?: ExecOptions) => Promise<ExecResult>;

/** Minimal synchronous filesystem surface, also injectable for tests. */
export interface FileSystem {
  readFile(path: string): string;
  writeFile(path: string, content: string): void;
  exists(path: string): boolean;
  mkdirp(path: string): void;
  /** Recursive remove; must be a no-op when the path is absent. */
  rmrf(path: string): void;
  readdir(path: string): string[];
}

export interface Deps {
  exec: Exec;
  fs: FileSystem;
  /** UMBRA_HOME, normally ~/.umbra. Injected so tests use a temp dir. */
  home: string;
}

/** Metadata umbra keeps per app under ~/.umbra/apps/<name>/umbra.json. */
export interface AppMeta {
  name: string;
  /** How the app was defined. */
  source: "image" | "compose";
  image?: string;
  /** Domain attached for reverse-proxy routing, if any. */
  domain?: string;
  /** Container port the proxy forwards to. */
  port?: number;
  /** Compose service that carries the domain (defaults to the app name). */
  service?: string;
  createdAt: string;
  updatedAt: string;
}

/** A single reverse-proxy route: domain -> app-alias:port. */
export interface Route {
  domain: string;
  app: string;
  port: number;
}

/** Persisted proxy configuration under ~/.umbra/proxy/routes.json. */
export interface ProxyConfig {
  /** ACME contact email for Let's Encrypt; optional but recommended. */
  email?: string;
  routes: Route[];
}
