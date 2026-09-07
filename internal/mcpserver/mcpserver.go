// Package mcpserver is umbra's MCP layer — the agent's door to the headless
// platform. Every tool is a thin skin over the core lifecycle functions; the
// CLI is the same skin for a human. stdio transport, registered with:
//
//	claude mcp add umbra -- umbra mcp
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/srimalonline/umbra/internal/core"
	"github.com/srimalonline/umbra/internal/safety"
)

// Version is the MCP server version reported to clients.
const Version = "0.1.0"

// Serve starts the umbra MCP server on stdio and blocks until the transport closes.
func Serve() error {
	deps := core.RealDeps()
	s := server.NewMCPServer("umbra", Version)

	s.AddTool(mcp.NewTool("umbra_deploy",
		mcp.WithDescription("Deploy (or redeploy) an app to this host: from a bare image, inline compose YAML, or a compose file path. Optionally attach a domain (routed through the umbra Caddy proxy with automatic SSL). Exactly one of image/composeYaml/composePath."),
		mcp.WithString("name", mcp.Required(), mcp.Description("App name ([a-z0-9-]); becomes its compose project and network alias")),
		mcp.WithString("image", mcp.Description("Deploy a bare image, e.g. nginx:alpine")),
		mcp.WithString("composeYaml", mcp.Description("Inline docker-compose YAML")),
		mcp.WithString("composePath", mcp.Description("Local path to a docker-compose file")),
		mcp.WithString("domain", mcp.Description("Attach a domain, e.g. app.example.com (needs DNS -> this server, ports 80/443 open)")),
		mcp.WithNumber("port", mcp.Description("Container port the domain forwards to (default 80)")),
		mcp.WithString("service", mcp.Description("Compose service that carries the domain; defaults to the app name")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(core.Deploy(deps, core.DeployArgs{
			Name:        req.GetString("name", ""),
			Image:       req.GetString("image", ""),
			ComposeYAML: req.GetString("composeYaml", ""),
			ComposePath: req.GetString("composePath", ""),
			Domain:      req.GetString("domain", ""),
			Port:        req.GetInt("port", 0),
			Service:     req.GetString("service", ""),
		}))
	})

	s.AddTool(mcp.NewTool("umbra_apps",
		mcp.WithDescription("List every umbra app on this host with status and domain."),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(core.List(deps))
	})

	s.AddTool(mcp.NewTool("umbra_status",
		mcp.WithDescription("Detailed status of one app (metadata + docker compose ps)."),
		mcp.WithString("name", mcp.Required()),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(core.Status(deps, req.GetString("name", "")))
	})

	s.AddTool(mcp.NewTool("umbra_logs",
		mcp.WithDescription("Tail an app's logs (default 200 lines, max 1000)."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithNumber("lines"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(core.Logs(deps, req.GetString("name", ""), req.GetInt("lines", 0)))
	})

	s.AddTool(mcp.NewTool("umbra_control",
		mcp.WithDescription("restart / stop / start an app."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("action", mcp.Required(), mcp.Enum("restart", "stop", "start")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(core.Control(deps, req.GetString("name", ""), req.GetString("action", "")))
	})

	s.AddTool(mcp.NewTool("umbra_domain",
		mcp.WithDescription("Attach or detach a domain on an app. attach routes domain -> app:port with SSL; detach removes the route (app keeps running)."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("action", mcp.Required(), mcp.Enum("attach", "detach")),
		mcp.WithString("domain", mcp.Description("Required for attach; optional for detach (defaults to the app's domain)")),
		mcp.WithNumber("port", mcp.Description("attach: container port (default 80)")),
		mcp.WithString("service"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		action := req.GetString("action", "")
		if action == "attach" {
			domain := req.GetString("domain", "")
			if domain == "" {
				return fail(fmt.Errorf("attach requires domain")), nil
			}
			port := req.GetInt("port", 0)
			if port == 0 {
				port = 80
			}
			return result(core.AttachDomain(deps, name, domain, port, req.GetString("service", "")))
		}
		return result(core.DetachDomain(deps, name, req.GetString("domain", "")))
	})

	s.AddTool(mcp.NewTool("umbra_remove",
		mcp.WithDescription("Remove an app: compose down, drop its routes, delete its dir. CONFIRM-GATED. Named volumes are KEPT unless purge:true. Returns a preview unless confirm:true."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithBoolean("confirm"),
		mcp.WithBoolean("purge", mcp.Description("Also destroy named volumes (data loss). Default false.")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		purge := req.GetBool("purge", false)
		if !req.GetBool("confirm", false) {
			suffix := ", volumes kept"
			if purge {
				suffix = " + PURGE volumes — DATA LOSS"
			}
			return result(safety.NewConfirmPreview("remove",
				fmt.Sprintf("Would remove app %q (compose down%s) and drop its proxy routes.", name, suffix)), nil)
		}
		return result(core.Remove(deps, name, purge))
	})

	return server.ServeStdio(s)
}

// result renders a core call's (value, error) as an MCP tool result: the value
// as pretty JSON text, or an error result. It also accepts a plain value with a
// nil error for the confirm-preview path.
func result(data any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return fail(err), nil
	}
	return ok(data), nil
}

func ok(data any) *mcp.CallToolResult {
	if s, isStr := data.(string); isStr {
		return mcp.NewToolResultText(s)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fail(err)
	}
	return mcp.NewToolResultText(string(b))
}

func fail(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError("Error: " + err.Error())
}
