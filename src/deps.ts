/** Assemble the real Deps (execa + node fs + ~/.umbra) for the MCP and CLI entrypoints. */
import { execReal, fsReal, umbraHome } from "./core/exec.js";
import type { Deps } from "./core/types.js";

export function realDeps(): Deps {
  return { exec: execReal, fs: fsReal, home: umbraHome() };
}
