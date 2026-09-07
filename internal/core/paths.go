package core

import (
	"path/filepath"

	"github.com/srimalonline/umbra/internal/safety"
)

// The ~/.umbra layout and the shared-infrastructure constants. Every path is
// derived here so the tree is documented in one place.
const (
	UmbraNetwork   = "umbra"
	ProxyContainer = "umbra-proxy"
	ProxyImage     = "caddy:2"
	// Named docker volumes so Caddy keeps its ACME certificates across restarts.
	CaddyDataVolume   = "umbra_caddy_data"
	CaddyConfigVolume = "umbra_caddy_config"
)

func appsDir(home string) string {
	return filepath.Join(home, "apps")
}

func appDir(home, name string) string {
	return filepath.Join(appsDir(home), safety.MustSanitizeName(name))
}

func appComposePath(home, name string) string {
	return filepath.Join(appDir(home, name), "docker-compose.yml")
}

// appNetworkOverridePath is the generated override that joins an app's service
// to the umbra network with an alias.
func appNetworkOverridePath(home, name string) string {
	return filepath.Join(appDir(home, name), "umbra-network.yml")
}

func appMetaPath(home, name string) string {
	return filepath.Join(appDir(home, name), "umbra.json")
}

func proxyDir(home string) string {
	return filepath.Join(home, "proxy")
}

func caddyfilePath(home string) string {
	return filepath.Join(proxyDir(home), "Caddyfile")
}

func proxyConfigPath(home string) string {
	return filepath.Join(proxyDir(home), "routes.json")
}

// projectName is the compose project name docker sees: keeps umbra's projects namespaced.
func projectName(name string) string {
	return "umbra-" + safety.MustSanitizeName(name)
}
