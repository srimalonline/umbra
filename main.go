// Command umbra is a headless PaaS: one static binary that is BOTH the CLI and
// the MCP server. `umbra mcp` starts the stdio MCP server; every other
// subcommand is the CLI.
//
//	umbra init | deploy | ls | status | logs | restart | stop | start | domain | rm | version
package main

import (
	"fmt"
	"os"

	"github.com/srimalonline/umbra/internal/cli"
	"github.com/srimalonline/umbra/internal/mcpserver"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "mcp" {
		if err := mcpserver.Serve(); err != nil {
			fmt.Fprintf(os.Stderr, "umbra: %s\n", err.Error())
			os.Exit(1)
		}
		return
	}
	os.Exit(cli.Run(args))
}
