package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/brendalima/despensa/internal/core/domain"
)

// newTestChat gives each run its own chat id and removes its rows when
// the test ends: the table is shared with whatever else is in the
// branch.
func newTestChat(t *testing.T, repo *Conversation) string {
	t.Helper()
	chatID := "teste-" + uuid.New().String()

	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(),
			`DELETE FROM conversation_turns WHERE chat_id = $1`, chatID)
	})
	return chatID
}

// The history is read oldest first, which is the order a model reads a
// conversation in, and only this chat's turns come back.
func TestConversationRecentReadsOneChatInOrder(t *testing.T) {
	repo := MustNewConversation(testDB(t))
	ctx := context.Background()

	chatID := newTestChat(t, repo)
	other := newTestChat(t, repo)

	for _, text := range []string{"primeira", "segunda", "terceira"} {
		content, err := json.Marshal(map[string]string{"role": "user", "text": text})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Append(ctx, domain.Turn{
			ChatID:  chatID,
			Role:    domain.TurnUser,
			Content: content,
		}); err != nil {
			t.Fatalf("couldn't append a turn: %v", err)
		}
	}
	if _, err := repo.Append(ctx, domain.Turn{
		ChatID:  other,
		Role:    domain.TurnUser,
		Content: []byte(`{"role":"user","text":"outra conversa"}`),
	}); err != nil {
		t.Fatalf("couldn't append a turn: %v", err)
	}

	turns, err := repo.Recent(ctx, chatID, time.Now().Add(-30*time.Minute), 20)
	if err != nil {
		t.Fatalf("couldn't read the history: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("read %d turns, want 3", len(turns))
	}

	var first struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(turns[0].Content, &first); err != nil {
		t.Fatalf("couldn't decode the stored turn: %v", err)
	}
	if first.Text != "primeira" {
		t.Errorf("the history starts at %q, want the oldest turn", first.Text)
	}
	if turns[0].Role != domain.TurnUser {
		t.Errorf("role read as %q", turns[0].Role)
	}
	if turns[0].CreatedAt.IsZero() {
		t.Error("the turn came back without a time")
	}
}

// The limit takes the newest turns, so a long argument with the bot
// cannot grow one request without bound — and what comes back is still
// in reading order.
func TestConversationRecentKeepsTheNewestWithinTheLimit(t *testing.T) {
	repo := MustNewConversation(testDB(t))
	ctx := context.Background()
	chatID := newTestChat(t, repo)

	for _, text := range []string{"um", "dois", "três", "quatro"} {
		if _, err := repo.Append(ctx, domain.Turn{
			ChatID:  chatID,
			Role:    domain.TurnUser,
			Content: []byte(`{"role":"user","text":"` + text + `"}`),
		}); err != nil {
			t.Fatalf("couldn't append a turn: %v", err)
		}
	}

	turns, err := repo.Recent(ctx, chatID, time.Now().Add(-30*time.Minute), 2)
	if err != nil {
		t.Fatalf("couldn't read the history: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("read %d turns, want 2", len(turns))
	}

	var third, fourth struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(turns[0].Content, &third); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(turns[1].Content, &fourth); err != nil {
		t.Fatal(err)
	}
	if third.Text != "três" || fourth.Text != "quatro" {
		t.Errorf("read %q then %q, want the two newest in order", third.Text, fourth.Text)
	}
}

// The window is what makes the history short-lived: a turn older than
// it is not read, and Forget takes it away.
func TestConversationForgetsWhatFellOutOfTheWindow(t *testing.T) {
	repo := MustNewConversation(testDB(t))
	ctx := context.Background()
	chatID := newTestChat(t, repo)

	stored, err := repo.Append(ctx, domain.Turn{
		ChatID:  chatID,
		Role:    domain.TurnAssistant,
		Content: []byte(`{"role":"assistant","text":"pronto"}`),
	})
	if err != nil {
		t.Fatalf("couldn't append a turn: %v", err)
	}

	// Backdating is the only way to test a window without waiting for
	// one, and it is what a stale row looks like.
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE conversation_turns SET created_at = now() - interval '2 hours' WHERE id = $1`,
		stored.ID); err != nil {
		t.Fatalf("couldn't backdate the turn: %v", err)
	}

	turns, err := repo.Recent(ctx, chatID, time.Now().Add(-30*time.Minute), 20)
	if err != nil {
		t.Fatalf("couldn't read the history: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("read %d turns from outside the window", len(turns))
	}

	if _, err := repo.Forget(ctx, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("couldn't forget: %v", err)
	}

	var left int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM conversation_turns WHERE chat_id = $1`, chatID).Scan(&left); err != nil {
		t.Fatalf("couldn't count what is left: %v", err)
	}
	if left != 0 {
		t.Errorf("%d turns survived Forget", left)
	}
}

func TestConversationNeedsADatabase(t *testing.T) {
	if _, err := NewConversation(nil); err == nil {
		t.Error("NewConversation accepted a missing database")
	}
}
