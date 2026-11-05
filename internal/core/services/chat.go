package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/repository"
)

var _ ChatService = (*Chat)(nil)

// Chat is the plumbing around one incoming message: the idempotency
// guard and the short history. It is a service rather than two
// repositories handed to the bot, because the window and the cap are
// rules — how much of a conversation is worth remembering — and rules
// do not belong in a driver.
type Chat struct {
	messages      repository.MessageRepository
	conversations repository.ConversationRepository

	window time.Duration
	limit  int
}

// ChatInput is the service's dependencies and its two numbers.
type ChatInput struct {
	Messages      repository.MessageRepository
	Conversations repository.ConversationRepository

	// Window is how far back a turn still counts. Thirty minutes is
	// long enough to finish a sentence and short enough that yesterday
	// does not answer today.
	Window time.Duration

	// Limit caps how many turns are read, so a long argument with the
	// bot cannot grow one request without bound.
	Limit int
}

func NewChat(in ChatInput) (*Chat, error) {
	if in.Messages == nil {
		return nil, errors.New("missing message repository dependency")
	}
	if in.Conversations == nil {
		return nil, errors.New("missing conversation repository dependency")
	}
	if in.Window <= 0 {
		return nil, errors.New("missing conversation window")
	}
	if in.Limit <= 0 {
		return nil, errors.New("missing conversation limit")
	}
	return &Chat{
		messages:      in.Messages,
		conversations: in.Conversations,
		window:        in.Window,
		limit:         in.Limit,
	}, nil
}

func MustNewChat(in ChatInput) *Chat {
	c, err := NewChat(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create chat service: %w", err))
	}
	return c
}

// Claim reports whether this delivery is the one that gets processed.
// WhatsApp redelivers, and a second run would log the same cup of rice
// twice, so this is called before the model and before any write.
func (s *Chat) Claim(ctx context.Context, provider, messageID string) (bool, error) {
	if provider == "" || messageID == "" {
		return false, domain.NewValidationError("message_id", "is empty")
	}
	return s.messages.Claim(ctx, provider, messageID)
}

func (s *Chat) History(ctx context.Context, chatID string) ([]domain.Turn, error) {
	return s.conversations.Recent(ctx, chatID, time.Now().Add(-s.window), s.limit)
}

func (s *Chat) Remember(ctx context.Context, chatID string, role domain.TurnRole, content []byte) error {
	if !role.Valid() {
		return domain.NewValidationError("role", "is not a known role")
	}
	if len(content) == 0 {
		return domain.NewValidationError("content", "is empty")
	}
	_, err := s.conversations.Append(ctx, domain.Turn{ChatID: chatID, Role: role, Content: content})
	return err
}

// Forget drops what fell out of the window, old enough that a reply
// would quote a conversation nobody remembers having.
func (s *Chat) Forget(ctx context.Context) (int64, error) {
	return s.conversations.Forget(ctx, time.Now().Add(-s.window))
}
