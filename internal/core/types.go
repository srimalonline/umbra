// Package core is the umbra platform: the app lifecycle, assembled from the
// compose, proxy and path primitives. Everything here is pure orchestration
// over the injectable Deps (Executor + FileStore), so the whole platform is
// testable without a Docker daemon. The MCP layer and the CLI are thin skins
// over these functions.
//
// An "app" is a docker-compose project living under ~/.umbra/apps/<name>/:
//
//	docker-compose.yml    the stack (synthesized from an image, or the user's)
//	umbra-network.yml     generated override joining a service to the proxy net
//	umbra.json            umbra's metadata for the app
package core

import "context"

// Executor is the one impure boundary the core shells through. The real
// implementation wraps os/exec; tests inject a recording mock. Everything the
// core does to the host — docker, docker compose, docker network — goes through
// here, so a test can assert the exact argv without a Docker daemon in sight.
//
// It never returns an error for a non-zero exit: the exit code is returned so
// the core decides what a failure means (docker returns 1 for "no such
// container" as well as for real errors). err is reserved for the process
// failing to start or the context deadline firing.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, code int, err error)
}

// FileStore is the minimal filesystem surface umbra needs, also injectable for
// tests. Semantics mirror the TypeScript original: ReadDir on a missing path
// yields an empty slice (not an error); RemoveAll on a missing path is a no-op.
type FileStore interface {
	ReadFile(path string) (string, error)
	WriteFile(path, content string) error
	Exists(path string) bool
	MkdirAll(path string) error
	RemoveAll(path string) error
	ReadDir(path string) ([]string, error)
}

// Deps bundles the injectable boundary and UMBRA_HOME (normally ~/.umbra,
// injected so tests use a temp dir).
type Deps struct {
	Exec Executor
	FS   FileStore
	Home string
}

// AppMeta is the metadata umbra keeps per app under ~/.umbra/apps/<name>/umbra.json.
type AppMeta struct {
	Name string `json:"name"`
	// Source is how the app was defined: "image" or "compose".
	Source string `json:"source"`
	Image  string `json:"image,omitempty"`
	// Domain attached for reverse-proxy routing, if any.
	Domain string `json:"domain,omitempty"`
	// Port is the container port the proxy forwards to.
	Port int `json:"port,omitempty"`
	// Service is the compose service that carries the domain (defaults to the app name).
	Service   string `json:"service,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Route is a single reverse-proxy route: domain -> app-alias:port.
type Route struct {
	Domain string `json:"domain"`
	App    string `json:"app"`
	Port   int    `json:"port"`
}

// ProxyConfig is the persisted proxy configuration under ~/.umbra/proxy/routes.json.
type ProxyConfig struct {
	// Email is the ACME contact for Let's Encrypt; optional but recommended.
	Email  string  `json:"email,omitempty"`
	Routes []Route `json:"routes"`
}

// DeployArgs are the inputs to Deploy.
type DeployArgs struct {
	Name string
	// Exactly one of Image / ComposeYAML / ComposePath defines the app.
	Image       string
	ComposeYAML string
	ComposePath string
	// Domain attaches a domain in the same call (optional).
	Domain string
	Port   int
	// Service is the compose service that carries the domain; defaults to the app name.
	Service string
}
