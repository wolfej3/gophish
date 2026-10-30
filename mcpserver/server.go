// Package mcpserver implements the `gophish mcp` subcommand: a Model Context
// Protocol (MCP) server that lets AI agents and assistants drive Gophish over
// a standard tool interface.
//
// It speaks MCP over stdio (how desktop AI clients connect) and adapts each
// tool call to a request against a running Gophish instance's REST API, using
// a configured base URL and API key. It is deliberately decoupled from the
// server: it does not load config.json, open the database, or start the
// admin/phishing servers — it is purely a REST client. Because every tool is
// a REST call carrying the caller's API key, the key's permissions, scope,
// team, tenant, and the view-only rule are the permission boundary; a
// read-only or scoped key yields a read-only or limited MCP with no extra
// enforcement code here.
package mcpserver

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// apiKeyEnv is the environment variable holding the Gophish API key. The key
// is read from the environment rather than a flag so it stays out of the
// process list.
const apiKeyEnv = "GOPHISH_MCP_API_KEY"

// Run is the entry point for the `gophish mcp` subcommand. It parses its own
// flags, reads the API key from the environment, builds the REST client, and
// serves MCP over stdio until the transport closes.
func Run(version string, args []string) error {
	fs := flag.NewFlagSet("gophish mcp", flag.ContinueOnError)
	baseURL := fs.String("url", "https://localhost:3333", "base URL of the running Gophish admin server")
	team := fs.String("team", "", "default team id to act within (optional)")
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification (for the self-signed admin certificate)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s is not set; export a Gophish API key to use the MCP server (the key is the permission boundary)", apiKeyEnv)
	}

	client := newClient(*baseURL, apiKey, *team, *insecure)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "gophish",
		Title:   "Gophish",
		Version: version,
	}, nil)
	registerTools(server, client)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}
