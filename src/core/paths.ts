/** The ~/.umbra layout. Every path is derived here so the tree is documented in one place. */
import path from "node:path";
import { sanitizeName } from "../safety.js";

export const UMBRA_NETWORK = "umbra";
export const PROXY_CONTAINER = "umbra-proxy";
export const PROXY_IMAGE = "caddy:2";
/** Named docker volumes so Caddy keeps its ACME certificates across restarts. */
export const CADDY_DATA_VOLUME = "umbra_caddy_data";
export const CADDY_CONFIG_VOLUME = "umbra_caddy_config";

export function appsDir(home: string): string {
  return path.join(home, "apps");
}

export function appDir(home: string, name: string): string {
  return path.join(appsDir(home), sanitizeName(name));
}

export function appComposePath(home: string, name: string): string {
  return path.join(appDir(home, name), "docker-compose.yml");
}

/** The generated override that joins an app's service to the umbra network with an alias. */
export function appNetworkOverridePath(home: string, name: string): string {
  return path.join(appDir(home, name), "umbra-network.yml");
}

export function appMetaPath(home: string, name: string): string {
  return path.join(appDir(home, name), "umbra.json");
}

export function proxyDir(home: string): string {
  return path.join(home, "proxy");
}

export function caddyfilePath(home: string): string {
  return path.join(proxyDir(home), "Caddyfile");
}

export function proxyConfigPath(home: string): string {
  return path.join(proxyDir(home), "routes.json");
}

/** The compose project name docker sees: keeps umbra's projects namespaced. */
export function projectName(name: string): string {
  return `umbra-${sanitizeName(name)}`;
}
