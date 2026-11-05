package bot

import (
	"context"
	"net/http"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
)

// Messenger sends a message out. Three methods, because there are three
// shapes: a line of text, a receipt with buttons, and — only for the
// weekly digest, outside the 24-hour window — an approved template.
type Messenger interface {
	SendText(ctx context.Context, to, text string) error
	SendWithButtons(ctx context.Context, to, text string, buttons []Button) error
	SendTemplate(ctx context.Context, to, name string, params []string) error
}

// Webhook is the inbound half of a provider. Verify takes the raw body
// because a signature is over the bytes that arrived, not over anything
// re-encoded from a parsed payload.
type Webhook interface {
	// Challenge answers the provider's subscription handshake.
	Challenge(r *http.Request) (string, error)

	// Verify fails closed: an unsigned or wrongly signed delivery is
	// refused before the payload is looked at, which is the difference
	// between a webhook and a public write API.
	Verify(r *http.Request, body []byte) error

	Parse(body []byte) ([]Message, error)
}

// Model is the model, narrowed to what a conversation needs. It is a
// port so the loop's rules — the allowed list, the iteration cap, the
// button that skips the model — can be tested without a key and
// without spending anything.
type Model interface {
	// Reply answers with one assistant turn. Tool calls in it are the
	// model asking; running them is the loop's job.
	Reply(ctx context.Context, req Request) (Turn, error)
}

// Request is everything the model is given for one turn.
type Request struct {
	System string
	Tools  []agent.Tool
	Turns  []Turn
}

// Chat is the slice of the chat service the bot uses. Declared here so
// this package depends on a shape it owns; services.Chat satisfies it
// without knowing it exists.
type Chat interface {
	Claim(ctx context.Context, provider, messageID string) (bool, error)
	History(ctx context.Context, chatID string) ([]domain.Turn, error)
	Remember(ctx context.Context, chatID string, role domain.TurnRole, content []byte) error
}

// Handler is what the HTTP layer sees: a message in, nothing out. The
// webhook has already answered 200 by the time this runs, because the
// provider redelivers what takes too long.
type Handler interface {
	Handle(ctx context.Context, msg Message)
}
