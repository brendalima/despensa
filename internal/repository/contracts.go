package repository

import (
	"context"

	"github.com/brendalima/despensa/internal/core/domain"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

// PantryRepository persists the pantry. Reads return each item with its
// portions and aliases.
//
// InTx is the shape that matters. One sentence can mention three items,
// and all three have to land or none: if an event is written and the
// running total is not, the inventory lies in silence, which is the
// disease this product exists to cure. The business rules still belong to
// the service — what a correction's delta is, when a threshold was
// crossed — so instead of moving them in here, the service is handed a
// repository bound to one transaction and never learns what a
// transaction is.
type PantryRepository interface {
	// InTx runs fn and commits only if it returns nil.
	InTx(ctx context.Context, fn func(tx PantryTx) error) error

	// List returns the live items, by name. Archived ones are left out:
	// they are gone from the pantry, not hidden.
	List(ctx context.Context) ([]domain.Item, error)
	ListArchived(ctx context.Context) ([]domain.Item, error)

	GetByID(ctx context.Context, id string) (domain.Item, error)

	// Events returns an item's ledger, most recent first, undone rows
	// included: they are part of what happened.
	Events(ctx context.Context, itemID string, limit int) ([]domain.Event, error)

	// Notes are the shopping list's free-text entries.
	Notes(ctx context.Context) ([]domain.Note, error)
	AddNote(ctx context.Context, text string) (domain.Note, error)
	RemoveNote(ctx context.Context, id int64) error
}

// PantryTx is the repository inside a transaction. The rows it reads are
// locked until the transaction ends, so two messages arriving together
// queue up instead of both reading the same on-hand and one overwriting
// the other.
type PantryTx interface {
	// LockByID and LockByName read one item FOR UPDATE. LockByName
	// matches the name and the aliases, ignoring case, and reports
	// *domain.AmbiguousItemError when more than one item matches, so
	// nothing is guessed.
	LockByID(ctx context.Context, id string) (domain.Item, error)
	LockByName(ctx context.Context, name string) (domain.Item, error)

	CreateItem(ctx context.Context, item domain.Item) error
	UpdateItem(ctx context.Context, item domain.Item) error
	SetOnHand(ctx context.Context, itemID string, onHand domain.Amount) error

	// UpsertPortion writes a portion. A portion whose source is
	// 'corrected' is never replaced by a 'seeded' estimate: the person's
	// own number outranks the model's guess.
	UpsertPortion(ctx context.Context, itemID string, p domain.Portion) error
	ReplaceAliases(ctx context.Context, itemID string, aliases []string) error

	AddEvent(ctx context.Context, e domain.Event) (int64, error)
	LockEvent(ctx context.Context, id int64) (domain.Event, error)
	MarkEventUndone(ctx context.Context, id int64) error
}

// MessageRepository is the idempotency guard for an incoming message.
type MessageRepository interface {
	// Claim records that a message is being handled and reports whether
	// this caller is the one that got it. A redelivery gets false, and
	// costs neither a model call nor a write.
	Claim(ctx context.Context, provider, messageID string) (bool, error)
}
