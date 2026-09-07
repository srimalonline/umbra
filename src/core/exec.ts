/** Real implementations of the injectable boundary: execa + node fs. */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execa } from "execa";
import type { Exec, FileSystem } from "./types.js";

export const DEFAULT_TIMEOUT_MS = 120_000;

/** execa-backed exec. Never throws on non-zero exit — returns the code so the
 * core decides what a failure means (docker returns 1 for "no such container"
 * as well as for real errors). */
export const execReal: Exec = async (file, args, opts = {}) => {
  const result = await execa(file, args, {
    cwd: opts.cwd,
    input: opts.input,
    timeout: opts.timeoutMs ?? DEFAULT_TIMEOUT_MS,
    reject: false,
    all: false,
  });
  return {
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? "",
    exitCode: typeof result.exitCode === "number" ? result.exitCode : 1,
  };
};

export const fsReal: FileSystem = {
  readFile: (p) => fs.readFileSync(p, "utf8"),
  writeFile: (p, content) => {
    fs.mkdirSync(path.dirname(p), { recursive: true });
    fs.writeFileSync(p, content, "utf8");
  },
  exists: (p) => fs.existsSync(p),
  mkdirp: (p) => {
    fs.mkdirSync(p, { recursive: true });
  },
  rmrf: (p) => {
    fs.rmSync(p, { recursive: true, force: true });
  },
  readdir: (p) => (fs.existsSync(p) ? fs.readdirSync(p) : []),
};

/** Resolve UMBRA_HOME: env override, else ~/.umbra. */
export function umbraHome(): string {
  return process.env.UMBRA_HOME || path.join(os.homedir(), ".umbra");
}
