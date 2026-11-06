package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
)

// fakeDigestRepo is the debt book, with the partial unique index's rule
// — one standing debt per chat — reproduced in memory. The real one is
// proved against Postgres in internal/repository.
type fakeDigestRepo struct {
	owed map[string]domain.Digest
	next int64

	paid []int64
	err  error
}

func newFakeDigestRepo() *fakeDigestRepo {
	return &fakeDigestRepo{owed: map[string]domain.Digest{}}
}

func (r *fakeDigestRepo) Owe(_ context.Context, chatID string, dueAt time.Time) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	if _, already := r.owed[chatID]; already {
		return false, nil
	}
	r.next++
	r.owed[chatID] = domain.Digest{ID: r.next, ChatID: chatID, DueAt: dueAt}
	return true, nil
}

func (r *fakeDigestRepo) Owed(_ context.Context, chatID string) (domain.Digest, bool, error) {
	if r.err != nil {
		return domain.Digest{}, false, r.err
	}
	digest, ok := r.owed[chatID]
	return digest, ok, nil
}

func (r *fakeDigestRepo) Paid(_ context.Context, id int64) error {
	r.paid = append(r.paid, id)
	for chatID, digest := range r.owed {
		if digest.ID == id {
			delete(r.owed, chatID)
		}
	}
	return nil
}

// stubPantry answers with one list and records nothing else: the only
// thing the digest asks the pantry is what is low.
type stubPantry struct {
	PantryService

	list domain.ShoppingList
	err  error

	calls int
}

func (p *stubPantry) ShoppingList(context.Context) (domain.ShoppingList, error) {
	p.calls++
	return p.list, p.err
}

const chat = "5511999990000"

func newDigestService(t *testing.T, pantry PantryService, repo *fakeDigestRepo, at time.Time) *Digest {
	t.Helper()
	d, err := NewDigest(DigestInput{
		Pantry:  pantry,
		Digests: repo,
		Now:     func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("couldn't create the digest service: %v", err)
	}
	return d
}

// The digest reads the pantry through the service, not the repository,
// so the weekly list and the list the bot answers with can never
// disagree about whether the rice is running out.
func TestTheWeekIsTheSameListTheBotAnswersWith(t *testing.T) {
	want := domain.ShoppingList{Notes: []domain.Note{{ID: 1, Text: "papel toalha"}}}
	pantry := &stubPantry{list: want}
	d := newDigestService(t, pantry, newFakeDigestRepo(), time.Now())

	got, err := d.List(context.Background())
	if err != nil {
		t.Fatalf("couldn't build the list: %v", err)
	}
	if len(got.Notes) != 1 || got.Notes[0].Text != "papel toalha" {
		t.Errorf("got %+v, want the pantry service's own list", got)
	}
	if pantry.calls != 1 {
		t.Errorf("asked the pantry %d times, want 1", pantry.calls)
	}
}

// Owe is dated by the service, because when a digest was due is the
// only thing worth saying about a late one.
func TestOweDatesTheDebt(t *testing.T) {
	sunday := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	repo := newFakeDigestRepo()
	d := newDigestService(t, &stubPantry{}, repo, sunday)

	fresh, err := d.Owe(context.Background(), chat)
	if err != nil {
		t.Fatalf("couldn't owe: %v", err)
	}
	if !fresh {
		t.Error("the first debt was not reported as fresh")
	}
	if got := repo.owed[chat].DueAt; !got.Equal(sunday) {
		t.Errorf("due at %v, want %v", got, sunday)
	}
}

// A second week without a template adds no second debt: the list is the
// same list.
func TestASecondDebtCollapsesIntoTheFirst(t *testing.T) {
	repo := newFakeDigestRepo()
	d := newDigestService(t, &stubPantry{}, repo, time.Now())
	ctx := context.Background()

	if _, err := d.Owe(ctx, chat); err != nil {
		t.Fatal(err)
	}
	fresh, err := d.Owe(ctx, chat)
	if err != nil {
		t.Fatalf("couldn't owe twice: %v", err)
	}
	if fresh {
		t.Error("a second debt was reported as fresh")
	}
	if len(repo.owed) != 1 {
		t.Errorf("got %d standing debts, want 1", len(repo.owed))
	}
}

func TestOwedAndPaid(t *testing.T) {
	repo := newFakeDigestRepo()
	d := newDigestService(t, &stubPantry{}, repo, time.Now())
	ctx := context.Background()

	if _, _, err := mustOwed(t, d, chat); err == nil {
		t.Error("nothing was owed yet, but something came back")
	}

	if _, err := d.Owe(ctx, chat); err != nil {
		t.Fatal(err)
	}
	digest, ok, err := d.Owed(ctx, chat)
	if err != nil || !ok {
		t.Fatalf("the debt was not found: ok=%v err=%v", ok, err)
	}
	if !digest.Owed() {
		t.Error("a standing debt does not report itself as owed")
	}

	if err := d.Paid(ctx, digest.ID); err != nil {
		t.Fatalf("couldn't settle: %v", err)
	}
	if _, ok, _ := d.Owed(ctx, chat); ok {
		t.Error("the debt is still standing after being paid")
	}
}

// The validations are what keep a missing chat id from becoming a row
// nobody can ever deliver.
func TestTheDigestRefusesWhatItCannotUse(t *testing.T) {
	d := newDigestService(t, &stubPantry{}, newFakeDigestRepo(), time.Now())
	ctx := context.Background()

	var invalid *domain.ValidationError
	if _, err := d.Owe(ctx, ""); !errors.As(err, &invalid) {
		t.Errorf("owing for no chat gave %v, want a validation error", err)
	}
	if _, _, err := d.Owed(ctx, ""); !errors.As(err, &invalid) {
		t.Errorf("reading no chat gave %v, want a validation error", err)
	}
	if err := d.Paid(ctx, 0); !errors.As(err, &invalid) {
		t.Errorf("settling id 0 gave %v, want a validation error", err)
	}
}

func TestTheDigestNeedsItsDependencies(t *testing.T) {
	if _, err := NewDigest(DigestInput{Digests: newFakeDigestRepo()}); err == nil {
		t.Error("a digest service was built without a pantry")
	}
	if _, err := NewDigest(DigestInput{Pantry: &stubPantry{}}); err == nil {
		t.Error("a digest service was built without a debt book")
	}
}

// mustOwed reports an error when nothing is owed, so the "nothing yet"
// case reads as one line above.
func mustOwed(t *testing.T, d *Digest, chatID string) (domain.Digest, bool, error) {
	t.Helper()
	digest, ok, err := d.Owed(context.Background(), chatID)
	if err != nil {
		t.Fatalf("couldn't read the debt: %v", err)
	}
	if !ok {
		return digest, false, errors.New("nothing owed")
	}
	return digest, true, nil
}
