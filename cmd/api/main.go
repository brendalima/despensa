// Command api is the WhatsApp bot: a webhook, a health check, and
// nothing else. The conversation itself is internal/bot, the pantry is
// internal/core/services, and this file only puts them together.
//
// It talks to the database directly. There is no second service to call
// — one pantry, one household — so the tools run in process, on the
// same connection the CLI uses.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brendalima/despensa/adapters"
	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/bot/claude"
	"github.com/brendalima/despensa/internal/bot/whatsapp"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/repository"
	"github.com/brendalima/despensa/internal/rest"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("api failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadAPIConfig()
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
	mRepository := repository.MustNewMessage(db)
	cRepository := repository.MustNewConversation(db)
	dRepository := repository.MustNewDigest(db)
	hRepository := repository.MustNewHealth(db)

	pService := services.MustNewPantry(pRepository)
	dService := services.MustNewDigest(services.DigestInput{
		Pantry:  pService,
		Digests: dRepository,
	})
	cService := services.MustNewChat(services.ChatInput{
		Messages:      mRepository,
		Conversations: cRepository,
		Window:        services.DefaultConversationWindow,
		Limit:         services.DefaultConversationLimit,
	})

	tools := agent.MustNew(pService, domain.SourceWhatsApp)
	model := claude.MustNew(claude.Input{
		APIKey: cfg.Anthropic.APIKey,
		Model:  cfg.Anthropic.Model,
	})
	messenger := whatsapp.MustNewMessenger(whatsapp.MessengerInput{
		PhoneNumberID: cfg.WhatsApp.PhoneNumberID,
		AccessToken:   cfg.WhatsApp.AccessToken,
	})
	webhook := whatsapp.MustNewWebhook(cfg.WhatsApp.VerifyToken, cfg.WhatsApp.AppSecret)

	theBot := bot.MustNew(bot.Input{
		Tools:          tools,
		Model:          model,
		Messenger:      messenger,
		Chat:           cService,
		Digests:        dService,
		AllowedNumbers: cfg.WhatsApp.AllowedNumbers,
		MaxIterations:  cfg.Anthropic.MaxToolIterations,
	})

	rWhats := rest.MustNewWhatsApp(webhook, theBot)
	rHealth := rest.MustNewHealth(hRepository)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", rHealth.Live)
	mux.HandleFunc("GET /readyz", rHealth.Ready)
	mux.HandleFunc("GET /webhooks/whatsapp", rWhats.Verify)
	mux.HandleFunc("POST /webhooks/whatsapp", rWhats.Receive)

	slog.Info("serving the pantry over whatsapp",
		"addr", cfg.Addr(), "env", cfg.Env, "model", cfg.Anthropic.Model,
		"tools", len(tools.Tools()), "numbers", len(cfg.WhatsApp.AllowedNumbers))

	if err := serve(ctx, cfg.Addr(), mux, rWhats.Wait); err != nil {
		return err
	}
	slog.Info("stopped")
	return nil
}

// serve runs until the context is cancelled, then stops taking
// requests and waits for the messages already answered 200 for. A
// message dropped there would be a cup of rice claimed and never
// written: the provider will not redeliver what it was told arrived.
func serve(ctx context.Context, addr string, handler http.Handler, waitForWork func()) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	failed := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
		close(failed)
	}()

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	waitForWork()
	return err
}
