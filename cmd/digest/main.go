// Command digest sends the weekly shopping list and exits.
//
// It is the only message this service starts: everything the bot sends
// otherwise is a reply to something a person typed. That is why it
// needs an approved WhatsApp template and the rest of the project does
// not — outside the 24-hour window Meta accepts nothing else — and why
// a digest that cannot be sent is recorded as owed rather than dropped.
//
// It runs as a scheduled Fly machine on the same image as the API:
//
//	fly machine run --schedule weekly --command /app/digest <image>
//
// Being a separate binary rather than a goroutine in cmd/api is the
// same decision as the migration being separate: a job that runs once a
// week has no business keeping a web server alive, and a web server
// that scales to zero cannot be trusted to be awake on Sunday.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brendalima/despensa/adapters"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/bot/whatsapp"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/repository"
)

// runTimeout bounds the whole job. A scheduled machine that hangs is a
// machine nobody notices until the bill arrives, and there is nothing
// here worth more than a minute: one query and one send per recipient.
const runTimeout = time.Minute

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("digest failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := adapters.LoadDigestConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	pRepository := repository.MustNewPantry(db)
	mRepository := repository.MustNewMessage(db)
	cRepository := repository.MustNewConversation(db)
	dRepository := repository.MustNewDigest(db)

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

	messenger := whatsapp.MustNewMessenger(whatsapp.MessengerInput{
		PhoneNumberID: cfg.PhoneNumberID,
		AccessToken:   cfg.AccessToken,
	})
	weekly := bot.MustNewDigest(bot.DigestInput{
		Digests:    dService,
		Messenger:  messenger,
		Recipients: cfg.Recipients,
		Template:   cfg.Template,
	})

	slog.Info("sending the weekly digest",
		"recipients", len(cfg.Recipients), "template", cfg.Template)

	sendErr := weekly.Send(ctx)

	// The pruning runs whether or not the send worked, because this is
	// the only thing that ever runs it. conversation_turns is read
	// through a window, so the old rows are harmless — but harmless
	// rows that accumulate forever are still a log of everything said
	// in a kitchen, kept for no reason.
	forgotten, forgetErr := cService.Forget(ctx)
	if forgetErr != nil {
		slog.Error("couldn't prune the conversation history", "error", forgetErr)
	} else {
		slog.Info("pruned the conversation history", "turns", forgotten)
	}

	if sendErr != nil {
		return sendErr
	}
	return forgetErr
}
