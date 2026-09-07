package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// --- in-memory fs + recording exec, so the whole core runs with no Docker ---

type call struct {
	name string
	args []string
}

type mockExec struct{ calls *[]call }

func (m mockExec) Run(ctx context.Context, name string, args ...string) (string, string, int, error) {
	*m.calls = append(*m.calls, call{name: name, args: args})
	joined := strings.Join(args, " ")
	// docker compose ps -> pretend one running container
	if contains(args, "ps") {
		return "web\trunning", "", 0, nil
	}
	if contains(args, "network") && contains(args, "ls") {
		return "bridge\nhost", "", 0, nil
	}
	_ = joined
	return "ok", "", 0, nil
}

type mockFS struct {
	store map[string]string
	dirs  map[string]bool
}

func newMockFS() *mockFS {
	return &mockFS{store: map[string]string{}, dirs: map[string]bool{}}
}

func (f *mockFS) ReadFile(p string) (string, error) {
	v, ok := f.store[p]
	if !ok {
		return "", fmt.Errorf("ENOENT %s", p)
	}
	return v, nil
}
func (f *mockFS) WriteFile(p, c string) error { f.store[p] = c; return nil }
func (f *mockFS) Exists(p string) bool        { _, ok := f.store[p]; return ok || f.dirs[p] }
func (f *mockFS) MkdirAll(p string) error     { f.dirs[p] = true; return nil }
func (f *mockFS) RemoveAll(p string) error {
	for k := range f.store {
		if strings.HasPrefix(k, p) {
			delete(f.store, k)
		}
	}
	for k := range f.dirs {
		if strings.HasPrefix(k, p) {
			delete(f.dirs, k)
		}
	}
	return nil
}
func (f *mockFS) ReadDir(p string) ([]string, error) {
	prefix := p
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	seen := map[string]bool{}
	var kids []string
	add := func(k string) {
		if strings.HasPrefix(k, prefix) {
			child := strings.SplitN(k[len(prefix):], "/", 2)[0]
			if child != "" && !seen[child] {
				seen[child] = true
				kids = append(kids, child)
			}
		}
	}
	for k := range f.store {
		add(k)
	}
	for k := range f.dirs {
		add(k)
	}
	return kids, nil
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func makeDeps() (Deps, *[]call, *mockFS) {
	calls := &[]call{}
	fs := newMockFS()
	return Deps{Exec: mockExec{calls: calls}, FS: fs, Home: "/home/test/.umbra"}, calls, fs
}

func callWith(calls []call, arg string) *call {
	for i := range calls {
		if calls[i].name == "docker" && contains(calls[i].args, arg) {
			return &calls[i]
		}
	}
	return nil
}

// --- caddyfile rendering ---------------------------------------------------

func TestRenderCaddyfile(t *testing.T) {
	cf := RenderCaddyfile(ProxyConfig{
		Email: "me@example.com",
		Routes: []Route{
			{Domain: "b.example.com", App: "b", Port: 3000},
			{Domain: "a.example.com", App: "a", Port: 80},
		},
	})
	if !strings.Contains(cf, "email me@example.com") {
		t.Fatalf("missing email directive:\n%s", cf)
	}
	if strings.Index(cf, "a.example.com") >= strings.Index(cf, "b.example.com") {
		t.Fatalf("routes not sorted by domain:\n%s", cf)
	}
	if !strings.Contains(cf, "reverse_proxy a:80") || !strings.Contains(cf, "reverse_proxy b:3000") {
		t.Fatalf("missing reverse_proxy lines:\n%s", cf)
	}
}

func TestRenderCaddyfileNoRoutes(t *testing.T) {
	if !strings.Contains(RenderCaddyfile(ProxyConfig{Routes: []Route{}}), "No routes yet") {
		t.Fatalf("expected a 'No routes yet' note")
	}
}

// --- app lifecycle ---------------------------------------------------------

func TestDeployImageWithDomain(t *testing.T) {
	deps, calls, fs := makeDeps()
	res, err := Deploy(deps, DeployArgs{
		Name:   "blog",
		Image:  "nginx:alpine",
		Domain: "blog.example.com",
		Port:   80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["deployed"] != "blog" {
		t.Fatalf("deployed = %v", res["deployed"])
	}
	if res["url"] != "https://blog.example.com" {
		t.Fatalf("url = %v", res["url"])
	}
	for _, p := range []string{
		"/home/test/.umbra/apps/blog/docker-compose.yml",
		"/home/test/.umbra/apps/blog/umbra-network.yml",
		"/home/test/.umbra/apps/blog/umbra.json",
	} {
		if _, ok := fs.store[p]; !ok {
			t.Fatalf("expected %s to be written", p)
		}
	}
	if !strings.Contains(fs.store["/home/test/.umbra/proxy/Caddyfile"], "reverse_proxy blog:80") {
		t.Fatalf("Caddyfile missing route:\n%s", fs.store["/home/test/.umbra/proxy/Caddyfile"])
	}
	if callWith(*calls, "up") == nil {
		t.Fatalf("no `docker compose up` ran")
	}
}

func TestDeployRejectsTwoSources(t *testing.T) {
	deps, _, _ := makeDeps()
	_, err := Deploy(deps, DeployArgs{Name: "x", Image: "nginx", ComposeYAML: "services: {}"})
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected 'exactly one' error, got %v", err)
	}
}

func TestListDeployedApps(t *testing.T) {
	deps, _, _ := makeDeps()
	if _, err := Deploy(deps, DeployArgs{Name: "api", Image: "node:20"}); err != nil {
		t.Fatal(err)
	}
	res, err := List(deps)
	if err != nil {
		t.Fatal(err)
	}
	if res["count"] != 1 {
		t.Fatalf("count = %v", res["count"])
	}
	apps := res["apps"].([]map[string]any)
	if apps[0]["name"] != "api" {
		t.Fatalf("app name = %v", apps[0]["name"])
	}
}

func TestRemoveKeepsVolumesByDefault(t *testing.T) {
	deps, calls, fs := makeDeps()
	if _, err := Deploy(deps, DeployArgs{Name: "gone", Image: "nginx", Domain: "gone.example.com"}); err != nil {
		t.Fatal(err)
	}
	res, err := Remove(deps, "gone", false)
	if err != nil {
		t.Fatal(err)
	}
	if res["volumes"] != "kept" {
		t.Fatalf("volumes = %v; want kept", res["volumes"])
	}
	dropped := res["routesDropped"].([]string)
	if len(dropped) != 1 || dropped[0] != "gone.example.com" {
		t.Fatalf("routesDropped = %v", dropped)
	}
	if _, ok := fs.store["/home/test/.umbra/apps/gone/umbra.json"]; ok {
		t.Fatalf("app dir should be gone")
	}
	down := callWith(*calls, "down")
	if down == nil {
		t.Fatalf("no down call")
	}
	if contains(down.args, "-v") {
		t.Fatalf("down should NOT carry -v without purge: %v", down.args)
	}
}

func TestRemovePurgePassesV(t *testing.T) {
	deps, calls, _ := makeDeps()
	if _, err := Deploy(deps, DeployArgs{Name: "wipe", Image: "nginx"}); err != nil {
		t.Fatal(err)
	}
	res, err := Remove(deps, "wipe", true)
	if err != nil {
		t.Fatal(err)
	}
	if res["volumes"] != "purged" {
		t.Fatalf("volumes = %v; want purged", res["volumes"])
	}
	down := callWith(*calls, "down")
	if down == nil || !contains(down.args, "-v") {
		t.Fatalf("purge must pass -v (the only path to volume loss): %v", down)
	}
}
