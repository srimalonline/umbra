// Package cli is the human's door to umbra: the same core lifecycle as the MCP
// layer, behind a hand-rolled arg parser (no dependency for something this
// small). The banner shows on `init` and `version`.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/srimalonline/umbra/internal/banner"
	"github.com/srimalonline/umbra/internal/core"
	"github.com/srimalonline/umbra/internal/safety"
)

// Version is umbra's version string, shared by the CLI and the MCP server.
const Version = "0.1.0"

func usage() string {
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
  umbra rm <name> [--purge] [--yes]          remove (--purge also destroys volumes)
  umbra mcp                                  start the MCP server on stdio
  umbra version | --version
`
}

type flags struct {
	str  map[string]string
	bool map[string]bool
}

func (f flags) s(k string) string { return f.str[k] }
func (f flags) b(k string) bool   { return f.bool[k] || f.str[k] != "" }
func (f flags) has(k string) bool {
	_, okS := f.str[k]
	_, okB := f.bool[k]
	return okS || okB
}
func (f flags) num(k string) int {
	if v, ok := f.str[k]; ok {
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// parse splits argv into positionals and --flags (--k v, or --k for booleans).
func parse(argv []string) (pos []string, f flags) {
	f = flags{str: map[string]string{}, bool: map[string]bool{}}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if len(a) > 2 && a[:2] == "--" {
			key := a[2:]
			if i+1 < len(argv) && !(len(argv[i+1]) >= 2 && argv[i+1][:2] == "--") {
				f.str[key] = argv[i+1]
				i++
			} else {
				f.bool[key] = true
			}
		} else if a == "--" {
			f.bool[""] = true
		} else {
			pos = append(pos, a)
		}
	}
	return pos, f
}

// Run executes the CLI for the given argv (everything after the program name)
// and returns a process exit code.
func Run(argv []string) int {
	out := os.Stdout
	if len(argv) == 0 {
		printData(out, usage())
		return 0
	}
	cmd := argv[0]
	pos, f := parse(argv[1:])

	err := dispatch(out, cmd, pos, f)
	if err == errUnknownCommand {
		// Usage was already printed to stdout; just signal a non-zero exit.
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "umbra: %s\n", err.Error())
		return 1
	}
	return 0
}

// errUnknownCommand signals an unknown subcommand; usage is printed to stdout
// and the process exits 1 without an extra error line, matching the TS CLI.
var errUnknownCommand = fmt.Errorf("unknown command")

func dispatch(out io.Writer, cmd string, pos []string, f flags) error {
	deps := core.RealDeps()

	switch cmd {
	case "help", "--help":
		printData(out, usage())
		return nil
	case "version", "--version":
		printData(out, banner.Banner(Version))
		return nil
	case "init":
		if err := core.EnsureProxy(deps); err != nil {
			return err
		}
		io.WriteString(out, banner.Banner(Version))
		res, err := core.List(deps)
		if err != nil {
			return err
		}
		printData(out, res)
		printData(out, "Proxy up. Next: umbra deploy <name> --image nginx:alpine --domain app.example.com")
		return nil
	case "deploy":
		if len(pos) == 0 {
			return fmt.Errorf("deploy needs an app name")
		}
		res, err := core.Deploy(deps, core.DeployArgs{
			Name:        pos[0],
			Image:       f.s("image"),
			ComposePath: f.s("compose"),
			Domain:      f.s("domain"),
			Port:        f.num("port"),
			Service:     f.s("service"),
		})
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "ls", "apps":
		res, err := core.List(deps)
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "status":
		res, err := core.Status(deps, first(pos))
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "logs":
		res, err := core.Logs(deps, first(pos), f.num("lines"))
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "restart", "stop", "start":
		res, err := core.Control(deps, first(pos), cmd)
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "domain":
		name := first(pos)
		if f.has("detach") {
			domain := ""
			if len(pos) > 1 {
				domain = pos[1]
			} else {
				domain = f.s("detach")
			}
			res, err := core.DetachDomain(deps, name, domain)
			if err != nil {
				return err
			}
			printData(out, res)
			return nil
		}
		if len(pos) < 2 {
			return fmt.Errorf("domain needs <name> <domain> (or --detach)")
		}
		port := f.num("port")
		if port == 0 {
			port = 80
		}
		res, err := core.AttachDomain(deps, name, pos[1], port, f.s("service"))
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	case "rm", "remove":
		name := first(pos)
		purge := f.b("purge")
		if !f.b("yes") {
			suffix := " (volumes kept)"
			if purge {
				suffix = " and PURGE its volumes (data loss)"
			}
			printData(out, safety.NewConfirmPreview("remove", fmt.Sprintf("Would remove %q%s. Re-run with --yes.", name, suffix)))
			return nil
		}
		res, err := core.Remove(deps, name, purge)
		if err != nil {
			return err
		}
		printData(out, res)
		return nil
	default:
		printData(out, fmt.Sprintf("Unknown command %q.\n\n%s", cmd, usage()))
		return errUnknownCommand
	}
}

func first(pos []string) string {
	if len(pos) > 0 {
		return pos[0]
	}
	return ""
}

// printData writes a string as-is, or any other value as pretty JSON, each
// followed by a newline — matching the TypeScript CLI's print().
func printData(out io.Writer, data any) {
	if s, ok := data.(string); ok {
		io.WriteString(out, s+"\n")
		return
	}
	b, err := marshalIndent(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "umbra: %s\n", err.Error())
		return
	}
	io.WriteString(out, string(b)+"\n")
}

func marshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
