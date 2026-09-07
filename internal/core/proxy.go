package core

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/srimalonline/umbra/internal/safety"
)

// The reverse proxy: a single umbra-managed Caddy container. Caddy is chosen
// because it issues and renews Let's Encrypt certificates automatically with
// almost no configuration — a domain block with a `reverse_proxy` line is the
// whole story.
//
// Routing model: Caddy and every app container share one external bridge
// network (`umbra`). An app joins it with its name as a network alias, so a
// route is simply `domain -> reverse_proxy <app>:<port>` — no host ports, no
// per-app port juggling.
//
// DNS + ports: for SSL to issue, the domain's A/AAAA record must point at this
// server and TCP 80 + 443 must be open to the internet. That is a fact about
// the host, documented in the README; umbra cannot verify it from here.

// RenderCaddyfile renders a full Caddyfile from the route table. Pure — unit-tested directly.
func RenderCaddyfile(config ProxyConfig) string {
	var out []string
	out = append(out, "# Managed by umbra. Do not edit by hand — regenerated on every route change.")
	if config.Email != "" {
		out = append(out, "", "{", "\temail "+config.Email, "}")
	}
	if len(config.Routes) == 0 {
		out = append(out, "", "# No routes yet. Attach a domain: umbra domain <app> <domain> --port <n>")
	}
	routes := make([]Route, len(config.Routes))
	copy(routes, config.Routes)
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].Domain < routes[j].Domain })
	for _, r := range routes {
		out = append(out, "", r.Domain+" {", "\treverse_proxy "+r.App+":"+itoa(r.Port), "}")
	}
	return strings.Join(out, "\n") + "\n"
}

// loadProxyConfig reads the persisted route table, tolerating a missing or
// corrupt file by returning an empty config.
func loadProxyConfig(deps Deps) ProxyConfig {
	p := proxyConfigPath(deps.Home)
	if !deps.FS.Exists(p) {
		return ProxyConfig{Routes: []Route{}}
	}
	raw, err := deps.FS.ReadFile(p)
	if err != nil {
		return ProxyConfig{Routes: []Route{}}
	}
	var parsed ProxyConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ProxyConfig{Routes: []Route{}}
	}
	if parsed.Routes == nil {
		parsed.Routes = []Route{}
	}
	return parsed
}

func saveProxyConfig(deps Deps, config ProxyConfig) error {
	if config.Routes == nil {
		config.Routes = []Route{}
	}
	if err := deps.FS.MkdirAll(proxyDir(deps.Home)); err != nil {
		return err
	}
	if err := deps.FS.WriteFile(proxyConfigPath(deps.Home), mustJSON(config)); err != nil {
		return err
	}
	return deps.FS.WriteFile(caddyfilePath(deps.Home), RenderCaddyfile(config))
}

// containerExists reports whether a container with the given name exists (any state).
func containerExists(deps Deps, name string) (bool, error) {
	stdout, _, _, err := run(deps, defaultTimeout, "docker", "ps", "-a", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return lineSetContains(stdout, name), nil
}

// containerRunning reports whether the container is currently running.
func containerRunning(deps Deps, name string) (bool, error) {
	stdout, _, _, err := run(deps, defaultTimeout, "docker", "ps", "--filter", "name=^/"+name+"$", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	return lineSetContains(stdout, name), nil
}

// ensureNetwork creates the shared umbra network if it is missing. Idempotent.
func ensureNetwork(deps Deps) error {
	stdout, _, _, err := run(deps, defaultTimeout, "docker", "network", "ls", "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	if !lineSetContains(stdout, UmbraNetwork) {
		if _, _, _, err := run(deps, defaultTimeout, "docker", "network", "create", UmbraNetwork); err != nil {
			return err
		}
	}
	return nil
}

// EnsureProxy ensures the Caddy proxy container is up. It writes an initial
// Caddyfile if none exists, then starts (or restarts) the container.
// Idempotent: a running proxy is left alone.
func EnsureProxy(deps Deps) error {
	if err := ensureNetwork(deps); err != nil {
		return err
	}
	config := loadProxyConfig(deps)
	// Always keep the on-disk Caddyfile in sync with the route table.
	if err := saveProxyConfig(deps, config); err != nil {
		return err
	}

	running, err := containerRunning(deps, ProxyContainer)
	if err != nil {
		return err
	}
	if running {
		return nil
	}
	exists, err := containerExists(deps, ProxyContainer)
	if err != nil {
		return err
	}
	if exists {
		_, _, _, err := run(deps, defaultTimeout, "docker", "start", ProxyContainer)
		return err
	}
	_, _, _, err = run(deps, defaultTimeout, "docker",
		"run", "-d",
		"--name", ProxyContainer,
		"--restart", "unless-stopped",
		"--network", UmbraNetwork,
		"-p", "80:80",
		"-p", "443:443",
		"-v", caddyfilePath(deps.Home)+":/etc/caddy/Caddyfile",
		"-v", CaddyDataVolume+":/data",
		"-v", CaddyConfigVolume+":/config",
		ProxyImage,
	)
	return err
}

// reloadProxy asks the running Caddy to reload its config in place — no downtime.
func reloadProxy(deps Deps) error {
	running, err := containerRunning(deps, ProxyContainer)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	_, _, _, err = run(deps, defaultTimeout, "docker",
		"exec", ProxyContainer,
		"caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile",
	)
	return err
}

// addRoute adds or replaces the route for a domain, then persists + reloads.
func addRoute(deps Deps, route Route) error {
	if _, err := safety.ValidateDomain(route.Domain); err != nil {
		return err
	}
	if _, err := safety.ValidatePort(route.Port); err != nil {
		return err
	}
	config := loadProxyConfig(deps)
	routes := make([]Route, 0, len(config.Routes)+1)
	for _, r := range config.Routes {
		if r.Domain != route.Domain {
			routes = append(routes, r)
		}
	}
	routes = append(routes, route)
	config.Routes = routes
	if err := saveProxyConfig(deps, config); err != nil {
		return err
	}
	if err := EnsureProxy(deps); err != nil {
		return err
	}
	return reloadProxy(deps)
}

// removeRoutesForApp removes every route pointing at an app (used on detach / remove).
func removeRoutesForApp(deps Deps, app string) ([]Route, error) {
	config := loadProxyConfig(deps)
	var removed, kept []Route
	for _, r := range config.Routes {
		if r.App == app {
			removed = append(removed, r)
		} else {
			kept = append(kept, r)
		}
	}
	if len(removed) == 0 {
		return []Route{}, nil
	}
	config.Routes = kept
	if err := saveProxyConfig(deps, config); err != nil {
		return nil, err
	}
	if err := reloadProxy(deps); err != nil {
		return nil, err
	}
	return removed, nil
}

// removeRoute removes a single domain route. It reports whether a route was removed.
func removeRoute(deps Deps, domain string) (bool, error) {
	config := loadProxyConfig(deps)
	var kept []Route
	for _, r := range config.Routes {
		if r.Domain != domain {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(config.Routes) {
		return false, nil
	}
	config.Routes = kept
	if err := saveProxyConfig(deps, config); err != nil {
		return false, err
	}
	if err := reloadProxy(deps); err != nil {
		return false, err
	}
	return true, nil
}

// lineSetContains splits stdout into trimmed lines and reports set membership.
func lineSetContains(stdout, want string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

const defaultTimeout = 120 * time.Second

// run executes one command through the injected Executor with a timeout context.
func run(deps Deps, timeout time.Duration, name string, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return deps.Exec.Run(ctx, name, args...)
}
