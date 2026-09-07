import { describe, it, expect, beforeEach } from "vitest";
import * as core from "../src/core/index.js";
import { renderCaddyfile } from "../src/core/proxy.js";
import { sanitizeName, validateDomain, capOutput } from "../src/safety.js";
import type { Deps, ExecResult, FileSystem } from "../src/core/types.js";

/** In-memory fs + recording exec, so the whole core runs with no Docker. */
function makeDeps() {
  const store = new Map<string, string>();
  const dirs = new Set<string>();
  const calls: { file: string; args: string[] }[] = [];

  const fs: FileSystem = {
    readFile: (p) => {
      if (!store.has(p)) throw new Error(`ENOENT ${p}`);
      return store.get(p)!;
    },
    writeFile: (p, c) => void store.set(p, c),
    exists: (p) => store.has(p) || dirs.has(p),
    mkdirp: (p) => void dirs.add(p),
    rmrf: (p) => {
      for (const k of [...store.keys()]) if (k.startsWith(p)) store.delete(k);
      for (const d of [...dirs]) if (d.startsWith(p)) dirs.delete(d);
    },
    readdir: (p) => {
      const kids = new Set<string>();
      const prefix = p.endsWith("/") ? p : p + "/";
      for (const k of [...store.keys(), ...dirs]) {
        if (k.startsWith(prefix)) kids.add(k.slice(prefix.length).split("/")[0]);
      }
      return [...kids];
    },
  };

  const exec = async (file: string, args: string[]): Promise<ExecResult> => {
    calls.push({ file, args });
    // docker compose ps -> pretend one running container
    if (args.includes("ps")) return { stdout: "web\trunning", stderr: "", exitCode: 0 };
    if (args.includes("network") && args.includes("ls")) return { stdout: "bridge\nhost", stderr: "", exitCode: 0 };
    if (args[0] === "ps" || (args.includes("ps") && args.includes("-a"))) return { stdout: "", stderr: "", exitCode: 0 };
    return { stdout: "ok", stderr: "", exitCode: 0 };
  };

  const deps: Deps = { exec, fs, home: "/home/test/.umbra" };
  return { deps, calls, store };
}

describe("safety", () => {
  it("sanitizes names to [a-z0-9-] and rejects empties", () => {
    expect(sanitizeName("My App!!")).toBe("my-app");
    expect(sanitizeName("  a__b  ")).toBe("a-b");
    expect(() => sanitizeName("!!!")).toThrow();
  });
  it("validates domains", () => {
    expect(validateDomain("app.example.com")).toBe("app.example.com");
    expect(() => validateDomain("not a domain")).toThrow();
    expect(() => validateDomain("evil.com { }")).toThrow();
  });
  it("caps output", () => {
    expect(capOutput("x".repeat(20000)).length).toBeLessThan(20000);
    expect(capOutput("short")).toBe("short");
  });
});

describe("caddyfile rendering", () => {
  it("renders a route block per domain, sorted", () => {
    const cf = renderCaddyfile({
      email: "me@example.com",
      routes: [
        { domain: "b.example.com", app: "b", port: 3000 },
        { domain: "a.example.com", app: "a", port: 80 },
      ],
    });
    expect(cf).toContain("email me@example.com");
    expect(cf.indexOf("a.example.com")).toBeLessThan(cf.indexOf("b.example.com")); // sorted
    expect(cf).toContain("reverse_proxy a:80");
    expect(cf).toContain("reverse_proxy b:3000");
  });
  it("notes when there are no routes", () => {
    expect(renderCaddyfile({ routes: [] })).toContain("No routes yet");
  });
});

describe("app lifecycle", () => {
  let d: ReturnType<typeof makeDeps>;
  beforeEach(() => (d = makeDeps()));

  it("deploys an image app with a domain: writes compose, override, meta, and a route", async () => {
    const res = (await core.deploy(d.deps, {
      name: "blog",
      image: "nginx:alpine",
      domain: "blog.example.com",
      port: 80,
    })) as Record<string, unknown>;

    expect(res.deployed).toBe("blog");
    expect(res.url).toBe("https://blog.example.com");
    // compose + override + meta written
    expect(d.store.has("/home/test/.umbra/apps/blog/docker-compose.yml")).toBe(true);
    expect(d.store.has("/home/test/.umbra/apps/blog/umbra-network.yml")).toBe(true);
    expect(d.store.has("/home/test/.umbra/apps/blog/umbra.json")).toBe(true);
    // the Caddyfile now carries the route
    expect(d.store.get("/home/test/.umbra/proxy/Caddyfile")).toContain("reverse_proxy blog:80");
    // a `docker compose up` actually ran
    expect(d.calls.some((c) => c.file === "docker" && c.args.includes("up"))).toBe(true);
  });

  it("rejects deploy with two sources", async () => {
    await expect(
      core.deploy(d.deps, { name: "x", image: "nginx", composeYaml: "services: {}" })
    ).rejects.toThrow(/exactly one/);
  });

  it("lists deployed apps", async () => {
    await core.deploy(d.deps, { name: "api", image: "node:20" });
    const res = (await core.list(d.deps)) as { count: number; apps: any[] };
    expect(res.count).toBe(1);
    expect(res.apps[0].name).toBe("api");
  });

  it("remove keeps volumes by default and drops routes", async () => {
    await core.deploy(d.deps, { name: "gone", image: "nginx", domain: "gone.example.com" });
    const res = (await core.remove(d.deps, "gone")) as Record<string, unknown>;
    expect(res.volumes).toBe("kept");
    expect(res.routesDropped).toContain("gone.example.com");
    expect(d.store.has("/home/test/.umbra/apps/gone/umbra.json")).toBe(false);
    // down ran WITHOUT -v
    const down = d.calls.find((c) => c.args.includes("down"))!;
    expect(down.args).not.toContain("-v");
  });

  it("remove --purge passes -v (the only path to volume loss)", async () => {
    await core.deploy(d.deps, { name: "wipe", image: "nginx" });
    const res = (await core.remove(d.deps, "wipe", { purge: true })) as Record<string, unknown>;
    expect(res.volumes).toBe("purged");
    const down = d.calls.find((c) => c.args.includes("down"))!;
    expect(down.args).toContain("-v");
  });
});
