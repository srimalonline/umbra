package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/srimalonline/umbra/internal/safety"
)

const (
	upTimeout      = 300 * time.Second
	controlTimeout = 120 * time.Second
)

func readMeta(deps Deps, name string) *AppMeta {
	p := appMetaPath(deps.Home, name)
	if !deps.FS.Exists(p) {
		return nil
	}
	raw, err := deps.FS.ReadFile(p)
	if err != nil {
		return nil
	}
	var meta AppMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil
	}
	return &meta
}

func writeMeta(deps Deps, meta AppMeta) error {
	if err := deps.FS.MkdirAll(appDir(deps.Home, meta.Name)); err != nil {
		return err
	}
	return deps.FS.WriteFile(appMetaPath(deps.Home, meta.Name), mustJSON(meta))
}

// composeFiles builds the `-f` argument list for an app: base compose, plus the
// network override when it exists.
func composeFiles(deps Deps, name string) []string {
	files := []string{"-f", appComposePath(deps.Home, name)}
	if deps.FS.Exists(appNetworkOverridePath(deps.Home, name)) {
		files = append(files, "-f", appNetworkOverridePath(deps.Home, name))
	}
	return files
}

func composeCmd(deps Deps, name string, timeout time.Duration, args ...string) (string, string, int, error) {
	full := append([]string{"compose", "-p", projectName(name)}, composeFiles(deps, name)...)
	full = append(full, args...)
	return run(deps, timeout, "docker", full...)
}

// Deploy deploys (or redeploys) an app: writes its compose, brings it up, and
// attaches a domain route when one is given. Idempotent — a second deploy
// updates in place.
func Deploy(deps Deps, args DeployArgs) (map[string]any, error) {
	name, err := safety.SanitizeName(args.Name)
	if err != nil {
		return nil, err
	}
	service := name
	if args.Service != "" {
		if service, err = safety.SanitizeName(args.Service); err != nil {
			return nil, err
		}
	}

	sources := 0
	for _, s := range []string{args.Image, args.ComposeYAML, args.ComposePath} {
		if s != "" {
			sources++
		}
	}
	if sources != 1 {
		return nil, fmt.Errorf("deploy needs exactly one of: image, composeYaml, composePath")
	}

	if err := EnsureProxy(deps); err != nil {
		return nil, err
	}
	if err := deps.FS.MkdirAll(appDir(deps.Home, name)); err != nil {
		return nil, err
	}

	// 1. Write the app's compose.
	var source string
	if args.Image != "" {
		if err := deps.FS.WriteFile(appComposePath(deps.Home, name), synthesizeImageCompose(name, args.Image, args.Port)); err != nil {
			return nil, err
		}
		source = "image"
	} else {
		yaml := args.ComposeYAML
		if yaml == "" {
			if yaml, err = deps.FS.ReadFile(args.ComposePath); err != nil {
				return nil, err
			}
		}
		if err := deps.FS.WriteFile(appComposePath(deps.Home, name), yaml); err != nil {
			return nil, err
		}
		source = "compose"
	}

	// 2. Domain routing: write the network override so the service is reachable
	//    from Caddy by the app's alias, then (after up) register the route.
	domain := ""
	if args.Domain != "" {
		if domain, err = safety.ValidateDomain(args.Domain); err != nil {
			return nil, err
		}
	}
	port := args.Port
	if domain != "" {
		p := args.Port
		if p == 0 {
			p = 80
		}
		if port, err = safety.ValidatePort(p); err != nil {
			return nil, err
		}
		if err := deps.FS.WriteFile(appNetworkOverridePath(deps.Home, name), networkOverride(name, service)); err != nil {
			return nil, err
		}
	}

	// 3. Bring it up.
	stdout, stderr, code, err := composeCmd(deps, name, upTimeout, "up", "-d", "--remove-orphans")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("docker compose up failed (exit %d): %s", code, safety.CapOutput(nonEmpty(stderr, stdout)))
	}

	// 4. Persist metadata.
	now := nowISO()
	prev := readMeta(deps, name)
	meta := AppMeta{
		Name:      name,
		Source:    source,
		Image:     coalesce(args.Image, metaImage(prev)),
		Domain:    coalesce(domain, metaDomain(prev)),
		Port:      coalesceInt(port, metaPort(prev)),
		Service:   service,
		CreatedAt: metaCreatedAt(prev, now),
		UpdatedAt: now,
	}
	if err := writeMeta(deps, meta); err != nil {
		return nil, err
	}

	// 5. Register the route (after the container is up so Caddy can resolve it).
	if domain != "" {
		if err := addRoute(deps, Route{Domain: domain, App: name, Port: port}); err != nil {
			return nil, err
		}
	}

	res := map[string]any{
		"deployed": name,
		"source":   source,
		"output":   safety.CapOutput(stdout + stderr),
	}
	if meta.Domain != "" {
		res["domain"] = meta.Domain
		res["url"] = "https://" + meta.Domain
		res["note"] = "Domain attached. SSL issues automatically once the domain's DNS points at this server and ports 80/443 are open."
	} else {
		res["note"] = "No domain attached; reach it on the umbra network or attach one with umbra_domain."
	}
	if meta.Port != 0 {
		res["port"] = meta.Port
	}
	return res, nil
}

var runningRe = regexp.MustCompile(`(?i)running|up`)

// List lists every umbra app with its recorded metadata and live compose status.
func List(deps Deps) (map[string]any, error) {
	entries, err := deps.FS.ReadDir(appsDir(deps.Home))
	if err != nil {
		return nil, err
	}
	apps := []map[string]any{}
	for _, name := range entries {
		if !deps.FS.Exists(appMetaPath(deps.Home, name)) {
			continue
		}
		meta := readMeta(deps, name)
		running, total := 0, 0
		if stdout, _, _, err := composeCmd(deps, name, defaultTimeout, "ps", "--format", "{{.Name}}\t{{.State}}"); err == nil {
			for _, row := range strings.Split(stdout, "\n") {
				if strings.TrimSpace(row) == "" {
					continue
				}
				total++
				if runningRe.MatchString(row) {
					running++
				}
			}
		}
		status := ""
		switch {
		case total == 0:
			status = "stopped"
		case running == total:
			status = "running"
		default:
			status = fmt.Sprintf("%d/%d up", running, total)
		}
		app := map[string]any{"name": name, "status": status}
		if meta != nil {
			if meta.Domain != "" {
				app["domain"] = meta.Domain
				app["url"] = "https://" + meta.Domain
			}
			if meta.Source != "" {
				app["source"] = meta.Source
			}
		}
		apps = append(apps, app)
	}
	return map[string]any{"apps": apps, "count": len(apps)}, nil
}

// Status returns the detailed status of one app (metadata + docker compose ps).
func Status(deps Deps, name string) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	meta := readMeta(deps, n)
	if meta == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	stdout, stderr, _, err := composeCmd(deps, n, defaultTimeout, "ps", "--format", "table {{.Name}}\t{{.State}}\t{{.Status}}\t{{.Ports}}")
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": n, "meta": meta, "ps": safety.CapOutput(nonEmpty(stdout, stderr))}, nil
}

// Logs tails an app's logs (default 200 lines, max 1000).
func Logs(deps Deps, name string, lines int) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	if readMeta(deps, n) == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	tail := safety.ClampLines(lines)
	stdout, stderr, _, err := composeCmd(deps, n, defaultTimeout, "logs", "--no-color", "--tail", strconv.Itoa(tail))
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": n, "lines": tail, "logs": safety.CapOutput(stdout + stderr)}, nil
}

// Control restarts / stops / starts an app.
func Control(deps Deps, name, action string) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	if readMeta(deps, n) == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	stdout, stderr, code, err := composeCmd(deps, n, controlTimeout, action)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s failed (exit %d): %s", action, code, safety.CapOutput(stderr))
	}
	return map[string]any{"name": n, "action": action, "output": safety.CapOutput(stdout + stderr)}, nil
}

// Remove removes an app: `docker compose down` (keeps named volumes unless
// purge), drops its proxy routes, and deletes its ~/.umbra/apps/<name> dir.
func Remove(deps Deps, name string, purge bool) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	if readMeta(deps, n) == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	downArgs := []string{"down", "--remove-orphans"}
	if purge {
		downArgs = append(downArgs, "-v") // volumes destroyed ONLY on explicit purge
	}
	stdout, stderr, _, err := composeCmd(deps, n, controlTimeout, downArgs...)
	if err != nil {
		return nil, err
	}
	routes, err := removeRoutesForApp(deps, n)
	if err != nil {
		return nil, err
	}
	if err := deps.FS.RemoveAll(appDir(deps.Home, n)); err != nil {
		return nil, err
	}
	dropped := []string{}
	for _, r := range routes {
		dropped = append(dropped, r.Domain)
	}
	volumes := "kept"
	if purge {
		volumes = "purged"
	}
	return map[string]any{
		"removed":       n,
		"volumes":       volumes,
		"routesDropped": dropped,
		"output":        safety.CapOutput(stdout + stderr),
	}, nil
}

// AttachDomain attaches a domain to an existing app (or moves it) and routes it through Caddy.
func AttachDomain(deps Deps, name, domain string, port int, service string) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	meta := readMeta(deps, n)
	if meta == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	d, err := safety.ValidateDomain(domain)
	if err != nil {
		return nil, err
	}
	p, err := safety.ValidatePort(port)
	if err != nil {
		return nil, err
	}
	svc := meta.Service
	if svc == "" {
		svc = n
	}
	if service != "" {
		if svc, err = safety.SanitizeName(service); err != nil {
			return nil, err
		}
	}

	// Ensure the app is joined to the umbra network, then bring it up so the
	// alias exists before Caddy is told to route to it.
	if err := deps.FS.WriteFile(appNetworkOverridePath(deps.Home, n), networkOverride(n, svc)); err != nil {
		return nil, err
	}
	if _, _, _, err := composeCmd(deps, n, upTimeout, "up", "-d", "--remove-orphans"); err != nil {
		return nil, err
	}
	if err := addRoute(deps, Route{Domain: d, App: n, Port: p}); err != nil {
		return nil, err
	}
	updated := *meta
	updated.Domain = d
	updated.Port = p
	updated.Service = svc
	updated.UpdatedAt = nowISO()
	if err := writeMeta(deps, updated); err != nil {
		return nil, err
	}
	return map[string]any{"app": n, "domain": d, "port": p, "url": "https://" + d}, nil
}

// DetachDomain detaches a domain from an app (removes the Caddy route; app keeps running).
func DetachDomain(deps Deps, name, domain string) (map[string]any, error) {
	n, err := safety.SanitizeName(name)
	if err != nil {
		return nil, err
	}
	meta := readMeta(deps, n)
	if meta == nil {
		return nil, fmt.Errorf("no umbra app named %q", n)
	}
	target := meta.Domain
	if domain != "" {
		if target, err = safety.ValidateDomain(domain); err != nil {
			return nil, err
		}
	}
	if target == "" {
		return nil, fmt.Errorf("app %q has no domain attached", n)
	}
	dropped, err := removeRoute(deps, target)
	if err != nil {
		return nil, err
	}
	if meta.Domain == target {
		updated := *meta
		updated.Domain = ""
		updated.UpdatedAt = nowISO()
		if err := writeMeta(deps, updated); err != nil {
			return nil, err
		}
	}
	return map[string]any{"app": n, "detached": target, "removed": dropped}, nil
}

// --- small helpers ---------------------------------------------------------

func nowISO() string {
	// ISO-8601 with millisecond precision and a Z suffix, matching JS toISOString().
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func itoa(n int) string { return strconv.Itoa(n) }

func nonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func coalesceInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func metaImage(m *AppMeta) string {
	if m == nil {
		return ""
	}
	return m.Image
}
func metaDomain(m *AppMeta) string {
	if m == nil {
		return ""
	}
	return m.Domain
}
func metaPort(m *AppMeta) int {
	if m == nil {
		return 0
	}
	return m.Port
}
func metaCreatedAt(m *AppMeta, now string) string {
	if m != nil && m.CreatedAt != "" {
		return m.CreatedAt
	}
	return now
}

// mustJSON marshals v with two-space indentation and a trailing newline,
// without HTML-escaping, matching JSON.stringify(v, null, 2) + "\n".
func mustJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	// Encoder.Encode already appends a newline; that is the trailing "\n".
	return buf.String()
}
