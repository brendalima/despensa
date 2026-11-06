package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/brendalima/despensa/internal/bot"
)

const (
	// bodyLimit is far above anything Meta sends and far below
	// anything that could fill memory.
	bodyLimit = 1 << 20

	// workTimeout bounds one message's processing. It has to fit a
	// cold database, a few model calls and a send.
	workTimeout = 2 * time.Minute
)

// WhatsApp is the webhook. Its shape is the one thing that matters
// here: the signature is checked before the payload is looked at, and
// the 200 goes out before the work starts, because Meta redelivers
// anything that takes too long and a redelivery is another model call.
type WhatsApp struct {
	webhook bot.Webhook
	handler bot.Handler

	// inFlight lets the server wait for the work it already answered
	// 200 for, instead of dropping it on shutdown.
	inFlight sync.WaitGroup
}

func NewWhatsApp(webhook bot.Webhook, handler bot.Handler) (*WhatsApp, error) {
	if webhook == nil {
		return nil, errors.New("missing webhook dependency")
	}
	if handler == nil {
		return nil, errors.New("missing bot dependency")
	}
	return &WhatsApp{webhook: webhook, handler: handler}, nil
}

func MustNewWhatsApp(webhook bot.Webhook, handler bot.Handler) *WhatsApp {
	w, err := NewWhatsApp(webhook, handler)
	if err != nil {
		panic(fmt.Errorf("couldn't create whatsapp handler: %w", err))
	}
	return w
}

// Verify answers Meta's subscription handshake with the challenge, as
// plain text: it is the one response here that is not JSON.
func (h *WhatsApp) Verify(w http.ResponseWriter, r *http.Request) {
	challenge, err := h.webhook.Challenge(r)
	if err != nil {
		slog.Warn("webhook verification refused", "error", err)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, challenge); err != nil {
		slog.Warn("couldn't write the challenge", "error", err)
	}
}

func (h *WhatsApp) Receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "couldn't read the body"})
		return
	}

	// Nothing above this line parsed the payload, and nothing below it
	// runs when the signature is wrong.
	if err := h.webhook.Verify(r, body); err != nil {
		slog.Warn("refused a delivery with a bad signature", "error", err)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	delivery, err := h.webhook.Parse(body)
	if err != nil {
		// A payload that cannot be read will not read any better on
		// the next attempt, so this is a 200 and a log, not a retry.
		slog.Error("couldn't parse a delivery", "error", err)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	// A delivery report costs nothing and answers the only question a
	// send cannot: a 2xx from Meta means accepted, and "failed" shows
	// up here or nowhere.
	for _, status := range delivery.Statuses {
		if len(status.Errors) == 0 {
			slog.Info("delivery status",
				"whatsapp_message_id", status.MessageID,
				"recipient", status.Recipient,
				"status", status.Status)
			continue
		}
		for _, e := range status.Errors {
			slog.Error("whatsapp could not deliver the message",
				"whatsapp_message_id", status.MessageID,
				"recipient", status.Recipient,
				"status", status.Status,
				"code", e.Code,
				"title", e.Title,
				"detail", e.Details)
		}
	}

	for _, msg := range delivery.Messages {
		h.inFlight.Add(1)
		go func() {
			defer h.inFlight.Done()

			// A context of its own: this outlives the request it
			// arrived on, which is the whole point of answering first.
			ctx, cancel := context.WithTimeout(context.Background(), workTimeout)
			defer cancel()

			h.handler.Handle(ctx, msg)
		}()
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Wait blocks until the messages already answered for are done. The
// server calls it during shutdown.
func (h *WhatsApp) Wait() { h.inFlight.Wait() }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("couldn't write the response", "error", err)
	}
}
