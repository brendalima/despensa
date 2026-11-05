package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/repository"
)

// fakeMessages and fakeConversations record what the service asked for,
// so the window and the cap are asserted on the call and not only on
// what came back.
type fakeMessages struct {
	claimed map[string]bool
	err     error
}

func (m *fakeMessages) Claim(_ context.Context, provider, messageID string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	key := provider + ":" + messageID
	if m.claimed[key] {
		return false, nil
	}
	m.claimed[key] = true
	return true, nil
}

type fakeConversations struct {
	turns []domain.Turn

	askedSince time.Time
	askedLimit int
	appended   []domain.Turn
	forgotten  time.Time

	err error
}

func (c *fakeConversations) Recent(_ context.Context, _ string, since time.Time, limit int) ([]domain.Turn, error) {
	c.askedSince, c.askedLimit = since, limit
	return c.turns, c.err
}

func (c *fakeConversations) Append(_ context.Context, t domain.Turn) (domain.Turn, error) {
	if c.err != nil {
		return domain.Turn{}, c.err
	}
	t.ID = int64(len(c.appended) + 1)
	c.appended = append(c.appended, t)
	return t, nil
}

func (c *fakeConversations) Forget(_ context.Context, before time.Time) (int64, error) {
	c.forgotten = before
	return int64(len(c.turns)), c.err
}

var (
	_ repository.MessageRepository      = (*fakeMessages)(nil)
	_ repository.ConversationRepository = (*fakeConversations)(nil)
)

func newChat(t *testing.T) (*services.Chat, *fakeMessages, *fakeConversations) {
	t.Helper()

	messages := &fakeMessages{claimed: map[string]bool{}}
	conversations := &fakeConversations{}

	chat, err := services.NewChat(services.ChatInput{
		Messages:      messages,
		Conversations: conversations,
		Window:        30 * time.Minute,
		Limit:         20,
	})
	if err != nil {
		t.Fatalf("couldn't create the chat service: %v", err)
	}
	return chat, messages, conversations
}

// WhatsApp redelivers, and a second run would log the same cup of rice
// twice. Only the first delivery gets the message.
func TestClaimIsOnlyWonOnce(t *testing.T) {
	chat, _, _ := newChat(t)
	ctx := context.Background()

	mine, err := chat.Claim(ctx, domain.SourceWhatsApp, "wamid.1")
	if err != nil {
		t.Fatalf("couldn't claim: %v", err)
	}
	if !mine {
		t.Fatal("the first delivery did not get the message")
	}

	again, err := chat.Claim(ctx, domain.SourceWhatsApp, "wamid.1")
	if err != nil {
		t.Fatalf("couldn't claim: %v", err)
	}
	if again {
		t.Error("a redelivery got the message too")
	}
}

func TestClaimRefusesAnEmptyKey(t *testing.T) {
	chat, _, _ := newChat(t)

	for _, c := range []struct{ provider, id string }{{"", "wamid.1"}, {domain.SourceWhatsApp, ""}} {
		_, err := chat.Claim(context.Background(), c.provider, c.id)

		var invalid *domain.ValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("Claim(%q, %q) = %v, want a validation error", c.provider, c.id, err)
		}
	}
}

// The window is the rule: long enough to finish a sentence, short
// enough that yesterday does not answer today.
func TestHistoryAsksForTheWindow(t *testing.T) {
	chat, _, conversations := newChat(t)

	before := time.Now().Add(-30 * time.Minute)
	if _, err := chat.History(context.Background(), "5511999990000"); err != nil {
		t.Fatalf("couldn't read the history: %v", err)
	}
	after := time.Now().Add(-30 * time.Minute)

	if conversations.askedSince.Before(before.Add(-time.Second)) ||
		conversations.askedSince.After(after.Add(time.Second)) {
		t.Errorf("asked for turns since %v, want half an hour ago", conversations.askedSince)
	}
	if conversations.askedLimit != 20 {
		t.Errorf("asked for %d turns, want the cap of 20", conversations.askedLimit)
	}
}

func TestRememberRefusesWhatCannotBeReadBack(t *testing.T) {
	chat, _, conversations := newChat(t)
	ctx := context.Background()

	if err := chat.Remember(ctx, "5511999990000", domain.TurnUser, []byte(`{"role":"user"}`)); err != nil {
		t.Fatalf("couldn't remember a turn: %v", err)
	}
	if len(conversations.appended) != 1 {
		t.Fatalf("stored %d turns", len(conversations.appended))
	}
	if conversations.appended[0].Role != domain.TurnUser {
		t.Errorf("stored the role as %q", conversations.appended[0].Role)
	}

	var invalid *domain.ValidationError
	if err := chat.Remember(ctx, "5511999990000", "model", []byte(`{}`)); !errors.As(err, &invalid) {
		t.Errorf("Remember accepted an unknown role: %v", err)
	}
	if err := chat.Remember(ctx, "5511999990000", domain.TurnUser, nil); !errors.As(err, &invalid) {
		t.Errorf("Remember accepted an empty turn: %v", err)
	}
}

func TestForgetDropsWhatFellOutOfTheWindow(t *testing.T) {
	chat, _, conversations := newChat(t)

	if _, err := chat.Forget(context.Background()); err != nil {
		t.Fatalf("couldn't forget: %v", err)
	}
	if time.Since(conversations.forgotten) < 29*time.Minute {
		t.Errorf("forgot turns from %v, want half an hour ago", conversations.forgotten)
	}
}

func TestChatNeedsItsDependenciesAndItsNumbers(t *testing.T) {
	messages := &fakeMessages{claimed: map[string]bool{}}
	conversations := &fakeConversations{}

	cases := map[string]services.ChatInput{
		"no messages":      {Conversations: conversations, Window: time.Minute, Limit: 1},
		"no conversations": {Messages: messages, Window: time.Minute, Limit: 1},
		"no window":        {Messages: messages, Conversations: conversations, Limit: 1},
		"no limit":         {Messages: messages, Conversations: conversations, Window: time.Minute},
	}

	for name, in := range cases {
		if _, err := services.NewChat(in); err == nil {
			t.Errorf("NewChat accepted %s", name)
		}
	}
}
