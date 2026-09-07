#!/usr/bin/env node
/**
 * umbra's MCP layer — the agent's door to the headless platform. Every tool is
 * a thin skin over the core lifecycle functions; the CLI is the same skin for a
 * human. stdio transport, registered with:
 *   claude mcp add umbra -- node <path>/dist/mcp/index.js
 */
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { realDeps } from "../deps.js";
import * as core from "../core/index.js";
import { confirmPreview } from "../safety.js";

const deps = realDeps();
const server = new McpServer({ name: "umbra", version: "0.1.0" });

const ok = (data: unknown) => ({
  content: [{ type: "text" as const, text: typeof data === "string" ? data : JSON.stringify(data, null, 2) }],
});
const fail = (e: unknown) => ({
  content: [{ type: "text" as const, text: `Error: ${e instanceof Error ? e.message : String(e)}` }],
  isError: true,
});
const run =
  <A>(fn: (a: A) => Promise<unknown>) =>
  async (a: A) => {
    try {
      return ok(await fn(a));
    } catch (e) {
      return fail(e);
    }
  };

server.tool(
  "umbra_deploy",
  "Deploy (or redeploy) an app to this host: from a bare image, inline compose YAML, or a compose file path. Optionally attach a domain (routed through the umbra Caddy proxy with automatic SSL). Exactly one of image/composeYaml/composePath.",
  {
    name: z.string().describe("App name ([a-z0-9-]); becomes its compose project and network alias"),
    image: z.string().optional().describe("Deploy a bare image, e.g. nginx:alpine"),
    composeYaml: z.string().optional().describe("Inline docker-compose YAML"),
    composePath: z.string().optional().describe("Local path to a docker-compose file"),
    domain: z.string().optional().describe("Attach a domain, e.g. app.example.com (needs DNS -> this server, ports 80/443 open)"),
    port: z.number().optional().describe("Container port the domain forwards to (default 80)"),
    service: z.string().optional().describe("Compose service that carries the domain; defaults to the app name"),
  },
  run((a: core.DeployArgs) => core.deploy(deps, a))
);

server.tool("umbra_apps", "List every umbra app on this host with status and domain.", {}, run(() => core.list(deps)));

server.tool(
  "umbra_status",
  "Detailed status of one app (metadata + docker compose ps).",
  { name: z.string() },
  run((a: { name: string }) => core.status(deps, a.name))
);

server.tool(
  "umbra_logs",
  "Tail an app's logs (default 200 lines, max 1000).",
  { name: z.string(), lines: z.number().optional() },
  run((a: { name: string; lines?: number }) => core.logs(deps, a.name, a.lines))
);

server.tool(
  "umbra_control",
  "restart / stop / start an app.",
  { name: z.string(), action: z.enum(["restart", "stop", "start"]) },
  run((a: { name: string; action: "restart" | "stop" | "start" }) => core.control(deps, a.name, a.action))
);

server.tool(
  "umbra_domain",
  "Attach or detach a domain on an app. attach routes domain -> app:port with SSL; detach removes the route (app keeps running).",
  {
    name: z.string(),
    action: z.enum(["attach", "detach"]),
    domain: z.string().optional().describe("Required for attach; optional for detach (defaults to the app's domain)"),
    port: z.number().optional().describe("attach: container port (default 80)"),
    service: z.string().optional(),
  },
  run((a: { name: string; action: "attach" | "detach"; domain?: string; port?: number; service?: string }) => {
    if (a.action === "attach") {
      if (!a.domain) throw new Error("attach requires domain.");
      return core.attachDomain(deps, a.name, a.domain, a.port ?? 80, a.service);
    }
    return core.detachDomain(deps, a.name, a.domain);
  })
);

server.tool(
  "umbra_remove",
  "Remove an app: compose down, drop its routes, delete its dir. CONFIRM-GATED. Named volumes are KEPT unless purge:true. Returns a preview unless confirm:true.",
  {
    name: z.string(),
    confirm: z.boolean().optional(),
    purge: z.boolean().optional().describe("Also destroy named volumes (data loss). Default false."),
  },
  run(async (a: { name: string; confirm?: boolean; purge?: boolean }) => {
    if (!a.confirm) {
      return confirmPreview(
        "remove",
        `Would remove app "${a.name}" (compose down${a.purge ? " + PURGE volumes — DATA LOSS" : ", volumes kept"}) and drop its proxy routes.`
      );
    }
    return core.remove(deps, a.name, { purge: a.purge });
  })
);

const transport = new StdioServerTransport();
await server.connect(transport);
console.error("umbra mcp ready on stdio");
