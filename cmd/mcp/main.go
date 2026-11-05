// Command mcp serves the pantry as MCP tools over stdio, so the host —
// Claude Desktop, Claude Code — is what calls the model. That is the
// point of this binary existing before the WhatsApp bot: the whole
// conversation can be tried out, with a real model, before a phone
// number or an API key is involved.
//
// It talks to the database directly. There is no HTTP service in
// between, because there is one service and one user.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/brendalima/despensa/adapters"
	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/repository"
)

func main() {
	// Stderr, always: stdout carries the JSON-RPC frames, and one stray
	// line on it corrupts the session rather than showing up as a log.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if err := run(); err != nil {
		slog.Error("mcp server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadMCPConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	pRepository := repository.MustNewPantry(db)
	pService := services.MustNewPantry(pRepository)
	tools := agent.MustNew(pService, domain.SourceMCP)

	slog.Info("serving the pantry over stdio", "tools", len(tools.Tools()))
	return newServer(tools).Run(ctx, &mcpStdio)
}
