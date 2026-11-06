package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/repository"
)

var _ DigestService = (*Digest)(nil)

// Digest is the weekly list, plus the debt book behind it.
//
// It reads the pantry through PantryService rather than through the
// repository, so "what is low" has exactly one definition: the digest
// and the shopping list the bot answers with can never disagree about
// whether the rice is running out.
type Digest struct {
	pantry  PantryService
	digests repository.DigestRepository

	now func() time.Time
}

type DigestInput struct {
	Pantry  PantryService
	Digests repository.DigestRepository

	// Now is the clock, injected only so a test can say what week it
	// is. Nil means time.Now.
	Now func() time.Time
}

func NewDigest(in DigestInput) (*Digest, error) {
	if in.Pantry == nil {
		return nil, errors.New("missing pantry service dependency")
	}
	if in.Digests == nil {
		return nil, errors.New("missing digest repository dependency")
	}
	now := in.Now
	if now == nil {
		now = time.Now
	}
	return &Digest{pantry: in.Pantry, digests: in.Digests, now: now}, nil
}

func MustNewDigest(in DigestInput) *Digest {
	d, err := NewDigest(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create digest service: %w", err))
	}
	return d
}

// List is the week's list: the low items and the notes, derived now.
func (d *Digest) List(ctx context.Context) (domain.ShoppingList, error) {
	return d.pantry.ShoppingList(ctx)
}

// Owe records that a chat is owed a digest that could not be sent, and
// reports whether this call is the one that created the debt.
func (d *Digest) Owe(ctx context.Context, chatID string) (bool, error) {
	if chatID == "" {
		return false, domain.NewValidationError("chat_id", "is empty")
	}
	return d.digests.Owe(ctx, chatID, d.now())
}

// Owed reports whether a digest is waiting for a turn to ride out on.
func (d *Digest) Owed(ctx context.Context, chatID string) (domain.Digest, bool, error) {
	if chatID == "" {
		return domain.Digest{}, false, domain.NewValidationError("chat_id", "is empty")
	}
	return d.digests.Owed(ctx, chatID)
}

// Paid settles a debt, after the message is away and not before: a
// failed send has to leave the digest owed.
func (d *Digest) Paid(ctx context.Context, id int64) error {
	if id <= 0 {
		return domain.NewValidationError("digest_id", "is not an id")
	}
	return d.digests.Paid(ctx, id)
}
