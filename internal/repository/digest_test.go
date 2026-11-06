package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newTestDigestChat gives each run its own chat id and removes its rows
// when the test ends: the table is shared with whatever else is in the
// branch.
func newTestDigestChat(t *testing.T, repo *Digest) string {
	t.Helper()
	chatID := "teste-" + uuid.New().String()

	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(),
			`DELETE FROM pending_digests WHERE chat_id = $1`, chatID)
	})
	return chatID
}

// Nothing owed is the normal case, and it is a false rather than an
// error: the caller is asking a question, not looking something up.
func TestNothingIsOwedByDefault(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	chatID := newTestDigestChat(t, repo)

	_, ok, err := repo.Owed(context.Background(), chatID)
	if err != nil {
		t.Fatalf("couldn't read the debt book: %v", err)
	}
	if ok {
		t.Error("a digest was owed before anything was written")
	}
}

// The partial unique index is what makes owing a digest idempotent, so
// three weeks without an approved template owe one list and not three.
// This is the invariant, proved against Postgres rather than a map.
func TestOnlyOneDigestIsOwedPerChat(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	ctx := context.Background()
	chatID := newTestDigestChat(t, repo)

	first, err := repo.Owe(ctx, chatID, time.Now().Add(-14*24*time.Hour))
	if err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}
	if !first {
		t.Fatal("the first debt was not reported as fresh")
	}

	for week := range 2 {
		again, err := repo.Owe(ctx, chatID, time.Now())
		if err != nil {
			t.Fatalf("couldn't owe in week %d: %v", week, err)
		}
		if again {
			t.Errorf("week %d created a second standing debt", week)
		}
	}

	var standing int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT count(*) FROM pending_digests WHERE chat_id = $1 AND delivered_at IS NULL`,
		chatID).Scan(&standing); err != nil {
		t.Fatalf("couldn't count the debts: %v", err)
	}
	if standing != 1 {
		t.Errorf("got %d standing debts, want 1", standing)
	}
}

// The oldest debt is the one that comes back, and it carries the date
// it was due — which is the only thing worth saying about a late list.
func TestTheOwedDigestCarriesWhenItWasDue(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	ctx := context.Background()
	chatID := newTestDigestChat(t, repo)

	due := time.Now().Add(-9 * 24 * time.Hour).UTC().Truncate(time.Millisecond)
	if _, err := repo.Owe(ctx, chatID, due); err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}

	digest, ok, err := repo.Owed(ctx, chatID)
	if err != nil || !ok {
		t.Fatalf("the debt was not found: ok=%v err=%v", ok, err)
	}
	if digest.ChatID != chatID {
		t.Errorf("got chat %q, want %q", digest.ChatID, chatID)
	}
	if !digest.DueAt.UTC().Truncate(time.Millisecond).Equal(due) {
		t.Errorf("due at %v, want %v", digest.DueAt.UTC(), due)
	}
	if !digest.Owed() {
		t.Error("a standing debt does not report itself as owed")
	}
}

// Paying frees the chat to be owed again next week, which is the whole
// point of the index being partial rather than plain.
func TestPayingFreesTheChatForNextWeek(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	ctx := context.Background()
	chatID := newTestDigestChat(t, repo)

	if _, err := repo.Owe(ctx, chatID, time.Now()); err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}
	digest, _, err := repo.Owed(ctx, chatID)
	if err != nil {
		t.Fatalf("couldn't read the debt: %v", err)
	}
	if err := repo.Paid(ctx, digest.ID); err != nil {
		t.Fatalf("couldn't settle: %v", err)
	}

	if _, ok, err := repo.Owed(ctx, chatID); err != nil || ok {
		t.Fatalf("the debt is still standing: ok=%v err=%v", ok, err)
	}

	fresh, err := repo.Owe(ctx, chatID, time.Now())
	if err != nil {
		t.Fatalf("couldn't owe again: %v", err)
	}
	if !fresh {
		t.Error("the chat could not be owed a digest again after paying")
	}
}

// Settling twice is not an error: the caller wanted nothing owed, and
// nothing is. The button on a receipt is idempotent for the same
// reason.
func TestPayingTwiceIsIdempotent(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	ctx := context.Background()
	chatID := newTestDigestChat(t, repo)

	if _, err := repo.Owe(ctx, chatID, time.Now()); err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}
	digest, _, err := repo.Owed(ctx, chatID)
	if err != nil {
		t.Fatalf("couldn't read the debt: %v", err)
	}

	for range 2 {
		if err := repo.Paid(ctx, digest.ID); err != nil {
			t.Fatalf("couldn't settle: %v", err)
		}
	}
	if err := repo.Paid(ctx, 0); err != nil {
		t.Errorf("settling an id that never existed failed: %v", err)
	}
}

// One household's debt is not another's.
func TestDebtsAreReadPerChat(t *testing.T) {
	repo := MustNewDigest(testDB(t))
	ctx := context.Background()
	mine := newTestDigestChat(t, repo)
	theirs := newTestDigestChat(t, repo)

	if _, err := repo.Owe(ctx, theirs, time.Now()); err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}
	if _, ok, err := repo.Owed(ctx, mine); err != nil || ok {
		t.Fatalf("another chat's debt came back: ok=%v err=%v", ok, err)
	}
}
