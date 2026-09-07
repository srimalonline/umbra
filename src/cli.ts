#!/usr/bin/env node
/**
 * umbra CLI — the human's door. Same core lifecycle as the MCP layer, a hand-
 * rolled arg parser (no dependency for something this small). The banner shows
 * on `init` and `--version`.
 */
import { realDeps } from "./deps.js";
import * as core from "./core/index.js";
import { banner } from "./banner.js";
import { confirmPreview } from "./safety.js";

const VERSION = "0.1.0";
const deps = realDeps();

function usage(): string {
  return `umbra — AI-native PaaS (your agent is the dashboard)

Usage:
  umbra init                                 ensure the proxy + network, print status
  umbra deploy <name> --image <img> [--domain d] [--port n]
  umbra deploy <name> --compose <file> [--domain d] [--port n] [--service s]
  umbra ls                                   list apps
  umbra status <name>                        app detail
  umbra logs <name> [--lines n]              tail logs
  umbra restart|stop|start <name>            lifecycle
  umbra domain <name> <domain> [--port n]    attach a domain
  umbra domain <name> --detach [<domain>]    detach a domain
  umbra rm <name> [--purge] [--yes]          remove (─-purge also destroys volumes)
  umbra version | --version
`;
}

/** Split argv into positionals and --flags (--k v, or --k for booleans). */
function parse(argv: string[]): { pos: string[]; flags: Record<string, string | boolean> } {
  const pos: string[] = [];
  const flags: Record<string, string | boolean> = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith("--")) {
      const key = a.slice(2);
      const next = argv[i + 1];
      if (next && !next.startsWith("--")) {
        flags[key] = next;
        i++;
      } else {
        flags[key] = true;
      }
    } else {
      pos.push(a);
    }
  }
  return { pos, flags };
}

function print(data: unknown): void {
  process.stdout.write((typeof data === "string" ? data : JSON.stringify(data, null, 2)) + "\n");
}

async function main() {
  const [cmd, ...rest] = process.argv.slice(2);
  const { pos, flags } = parse(rest);
  const num = (v: string | boolean | undefined) => (typeof v === "string" ? Number(v) : undefined);

  switch (cmd) {
    case undefined:
    case "help":
    case "--help":
      print(usage());
      return;
    case "version":
    case "--version":
      print(banner(VERSION));
      return;
    case "init": {
      await core.ensureProxy(deps);
      process.stdout.write(banner(VERSION));
      print(await core.list(deps));
      print("Proxy up. Next: umbra deploy <name> --image nginx:alpine --domain app.example.com");
      return;
    }
    case "deploy": {
      const name = pos[0];
      if (!name) throw new Error("deploy needs an app name.");
      print(
        await core.deploy(deps, {
          name,
          image: typeof flags.image === "string" ? flags.image : undefined,
          composePath: typeof flags.compose === "string" ? flags.compose : undefined,
          domain: typeof flags.domain === "string" ? flags.domain : undefined,
          port: num(flags.port),
          service: typeof flags.service === "string" ? flags.service : undefined,
        })
      );
      return;
    }
    case "ls":
    case "apps":
      print(await core.list(deps));
      return;
    case "status":
      print(await core.status(deps, pos[0]));
      return;
    case "logs":
      print(await core.logs(deps, pos[0], num(flags.lines)));
      return;
    case "restart":
    case "stop":
    case "start":
      print(await core.control(deps, pos[0], cmd));
      return;
    case "domain": {
      const name = pos[0];
      if (flags.detach) {
        print(await core.detachDomain(deps, name, pos[1] ?? (typeof flags.detach === "string" ? flags.detach : undefined)));
      } else {
        const domain = pos[1];
        if (!domain) throw new Error("domain needs <name> <domain> (or --detach).");
        print(await core.attachDomain(deps, name, domain, num(flags.port) ?? 80, typeof flags.service === "string" ? flags.service : undefined));
      }
      return;
    }
    case "rm":
    case "remove": {
      const name = pos[0];
      const purge = Boolean(flags.purge);
      if (!flags.yes) {
        print(confirmPreview("remove", `Would remove "${name}"${purge ? " and PURGE its volumes (data loss)" : " (volumes kept)"}. Re-run with --yes.`));
        return;
      }
      print(await core.remove(deps, name, { purge }));
      return;
    }
    default:
      print(`Unknown command "${cmd}".\n\n` + usage());
      process.exitCode = 1;
  }
}

main().catch((e) => {
  process.stderr.write(`umbra: ${e instanceof Error ? e.message : String(e)}\n`);
  process.exitCode = 1;
});
