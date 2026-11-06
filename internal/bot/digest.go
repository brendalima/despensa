package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

// Digest is the weekly list, and the only message this service ever
// starts. Everything else the bot sends is a reply.
//
// That one difference is the whole shape of this file. Outside
// WhatsApp's 24-hour window Meta accepts nothing but a template it has
// approved, so a digest is either sent as that template or not sent at
// all — and "not sent at all" has to mean *owed*, never lost, because
// the alternative is a product that quietly stops reminding you to buy
// rice.
type Digest struct {
	digests   Digests
	messenger Messenger

	// recipients is the same closed list the bot answers: one
	// household, and the people in it are the people who get the list.
	recipients []string

	// template is the approved template's name. Empty is the expected
	// state until Meta approves one, and it is not an error: the
	// digest goes straight to the debt book and rides out on the next
	// message.
	template string
}

type DigestInput struct {
	Digests   Digests
	Messenger Messenger

	Recipients []string
	Template   string
}

func NewDigest(in DigestInput) (*Digest, error) {
	if in.Digests == nil {
		return nil, errors.New("missing digests dependency")
	}
	if in.Messenger == nil {
		return nil, errors.New("missing messenger dependency")
	}
	if len(in.Recipients) == 0 {
		return nil, errors.New("missing digest recipients")
	}
	return &Digest{
		digests:    in.Digests,
		messenger:  in.Messenger,
		recipients: in.Recipients,
		template:   in.Template,
	}, nil
}

func MustNewDigest(in DigestInput) *Digest {
	d, err := NewDigest(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create digest: %w", err))
	}
	return d
}

// Send is the whole of the weekly run: build the list once, then give
// every recipient their copy. One recipient failing does not stop the
// others — a household of two where one phone is out should still get
// its list.
func (d *Digest) Send(ctx context.Context) error {
	list, err := d.digests.List(ctx)
	if err != nil {
		return fmt.Errorf("couldn't build the weekly list: %w", err)
	}

	// Nothing to buy is not worth a message. The digest is the one
	// thing here that costs money per send and that nobody asked for,
	// so a weekly "não está faltando nada" is the exact habit that
	// teaches a person to stop reading it.
	if list.Empty() {
		slog.Info("nothing low, no digest sent", "recipients", len(d.recipients))
		return nil
	}

	var failures []error
	for _, to := range d.recipients {
		if err := d.sendOne(ctx, to, list); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", to, err))
		}
	}
	return errors.Join(failures...)
}

// sendOne sends one copy, or owes it. The fallback is not an error
// path: until Meta approves a template it is the only path, and it
// loses nothing.
func (d *Digest) sendOne(ctx context.Context, to string, list domain.ShoppingList) error {
	if d.template == "" {
		return d.owe(ctx, to, "no approved template")
	}

	err := d.messenger.SendTemplate(ctx, to, d.template, []string{humanize.DigestLine(list)})
	if err != nil {
		// A refused template is the case this fallback was built for:
		// a template pending review, a template renamed, a 24-hour
		// window that was never open. The list is not lost, it is
		// late.
		slog.Warn("the digest template was refused, owing it instead", "to", to, "error", err)
		return d.owe(ctx, to, "the template was refused")
	}

	slog.Info("weekly digest sent", "to", to, "template", d.template, "items", len(list.Low), "notes", len(list.Notes))
	return nil
}

func (d *Digest) owe(ctx context.Context, to, why string) error {
	fresh, err := d.digests.Owe(ctx, to)
	if err != nil {
		return fmt.Errorf("couldn't owe the digest: %w", err)
	}
	slog.Info("digest owed, it will ride out on the next message",
		"to", to, "why", why, "already_owed", !fresh)
	return nil
}

// deliverOwed pays a standing debt, after the turn's own answer is
// away. It runs on any message from the household — an inbound message
// is also what opens the 24-hour window, so this is the one moment a
// free-form digest is allowed at all.
//
// A debt is settled only once the send succeeded. A failure here leaves
// it standing, which is the right way round: a digest delivered twice
// is noise, a digest dropped is the product not working.
func (b *Bot) deliverOwed(ctx context.Context, chatID string) (bool, error) {
	owed, ok, err := b.digests.Owed(ctx, chatID)
	if err != nil {
		return false, fmt.Errorf("couldn't read the owed digest: %w", err)
	}
	if !ok {
		return false, nil
	}

	list, err := b.digests.List(ctx)
	if err != nil {
		return false, fmt.Errorf("couldn't build the owed list: %w", err)
	}
	if err := b.messenger.SendText(ctx, chatID, humanize.Digest(list)); err != nil {
		return false, fmt.Errorf("couldn't send the owed digest: %w", err)
	}
	if err := b.digests.Paid(ctx, owed.ID); err != nil {
		// The message is away, so the debt is paid whatever the row
		// says. Saying so is all that is left; the next turn sending
		// it a second time is the cost of not having written this.
		return true, fmt.Errorf("the owed digest was sent but not settled: %w", err)
	}
	slog.Info("owed digest delivered", "chat_id", chatID, "due_at", owed.DueAt)
	return true, nil
}
