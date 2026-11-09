// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/mcpserver"
)

// runMCP is the stdio bridge: an AI assistant that starts local commands (and speaks no HTTP)
// runs goarmin mcp, which forwards everything to a goarmin server's /mcp endpoint with a token
// from Settings › MCP. The token comes from GOARMIN_MCP_TOKEN, never a flag (shell history,
// the process list).
func runMCP(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", config.String("GOARMIN_MCP_SERVER", ""), "the goarmin server, e.g. http://goarmin.example:8642 (or GOARMIN_MCP_SERVER)")
	list := fs.Bool("list", false, "print what the server offers this token, and exit")
	verbose := fs.Bool("v", false, "log the bridge's work on stderr")
	fs.Usage = func() {
		fmt.Fprint(stderr, `usage: GOARMIN_MCP_TOKEN=gma_… goarmin mcp -server URL [-list]

Serves a goarmin server's MCP tools, resources and prompts on standard input and output, for AI
assistants that start a local command. Make the token under Settings › MCP in the dashboard.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *server == "" {
		fmt.Fprintln(stderr, "goarmin mcp: -server is needed (the goarmin server's address)")
		return 2
	}
	endpoint, err := mcpserver.Endpoint(*server)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin mcp:", err)
		return 2
	}
	token := strings.TrimSpace(os.Getenv("GOARMIN_MCP_TOKEN"))
	if token == "" {
		fmt.Fprintln(stderr, "goarmin mcp: set GOARMIN_MCP_TOKEN to a token from Settings › MCP")
		return 2
	}
	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signalContext()
	defer stop()
	b := mcpserver.NewBridge(endpoint, token, version, log)
	defer b.Close()
	if *list {
		return printCatalogue(ctx, b, stdout, stderr)
	}
	srv, err := b.Server(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin mcp:", err)
		return 1
	}
	log.Info("mcp bridge: serving on stdio", "endpoint", endpoint)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(stderr, "goarmin mcp:", err)
		return 1
	}
	return 0
}

// printCatalogue lists the tools, resources and prompts the server offers this token.
func printCatalogue(ctx context.Context, b *mcpserver.Bridge, stdout, stderr io.Writer) int {
	tools, templates, prompts, _, err := b.Catalogue(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin mcp:", err)
		return 1
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%d tools\n", len(tools))
	for _, t := range tools {
		kind := "read"
		if a := t.Annotations; a != nil && !a.ReadOnlyHint {
			kind = "write"
			if a.DestructiveHint != nil && *a.DestructiveHint {
				kind = "removes"
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", t.Name, kind, t.Title)
	}
	fmt.Fprintf(tw, "\n%d resources\n", len(templates))
	for _, r := range templates {
		fmt.Fprintf(tw, "  %s\t%s\n", r.URITemplate, r.Title)
	}
	fmt.Fprintf(tw, "\n%d prompts\n", len(prompts))
	for _, p := range prompts {
		fmt.Fprintf(tw, "  %s\t%s\n", p.Name, p.Title)
	}
	_ = tw.Flush()
	return 0
}
