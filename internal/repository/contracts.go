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
// InTx is the shape that matters: if an event is written and the running
// total is not, the inventory lies in silence. The rules still belong to
// the service, so rather than move them in here, the service is handed a
// repository bound to one transaction and never learns what a
// transaction is.
type PantryRepository interface {
	// InTx runs fn and commits only if it returns nil.
	InTx(ctx context.Context, fn func(tx PantryTx) error) error

	// List returns the live items, by name. Archived ones are gone from
	// the pantry, not hidden, so they are left out.
	List(ctx context.Context) ([]domain.Item, error)
	ListArchived(ctx context.Context) ([]domain.Item, error)

	GetByID(ctx context.Context, id string) (domain.Item, error)

	// Events returns an item's ledger, most recent first, undone rows
	// included.
	Events(ctx context.Context, itemID string, limit int) ([]domain.Event, error)

	// Notes are the shopping list's free-text entries.
	Notes(ctx context.Context) ([]domain.Note, error)
	AddNote(ctx context.Context, text string) (domain.Note, error)
	RemoveNote(ctx context.Context, id int64) error
}

// PantryTx is the repository inside a transaction. The rows it reads are
// locked until it ends, so two messages arriving together queue up
// instead of both reading the same on-hand.
type PantryTx interface {
	// LockByName matches the name and the aliases, ignoring case, and
	// reports *domain.AmbiguousItemError rather than guessing.
	LockByID(ctx context.Context, id string) (domain.Item, error)
	LockByName(ctx context.Context, name string) (domain.Item, error)

	CreateItem(ctx context.Context, item domain.Item) error
	UpdateItem(ctx context.Context, item domain.Item) error
	SetOnHand(ctx context.Context, itemID string, onHand domain.Amount) error

	// UpsertPortion never lets a 'seeded' estimate replace a
	// 'corrected' size: the person's number outranks the model's.
	UpsertPortion(ctx context.Context, itemID string, p domain.Portion) error
	ReplaceAliases(ctx context.Context, itemID string, aliases []string) error

	AddEvent(ctx context.Context, e domain.Event) (int64, error)
	LockEvent(ctx context.Context, id int64) (domain.Event, error)
	MarkEventUndone(ctx context.Context, id int64) error
}

// MessageRepository is the idempotency guard for an incoming message.
type MessageRepository interface {
	// Claim reports whether this caller got the message. A redelivery
	// gets false, and costs neither a model call nor a write.
	Claim(ctx context.Context, provider, messageID string) (bool, error)
}
