package repository

import (
	"context"
	"time"

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

	// RemoveNote returns the row it deleted: a note is removed by
	// saying the thing was bought, and the answer names it.
	RemoveNote(ctx context.Context, id int64) (domain.Note, error)
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

	// SetShopping writes the shopping mode alone, for the one case the
	// service changes it without the person asking: a pin ending on the
	// restock that clears the minimum.
	SetShopping(ctx context.Context, itemID string, mode domain.ShoppingMode) error

	// DeleteNotesNamed removes the reminders naming a thing that has
	// just become an item, inside the same transaction that created it.
	// It returns how many went, and no match is not an error.
	DeleteNotesNamed(ctx context.Context, name string) (int64, error)

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

// ConversationRepository keeps the short history that lets "e mais uma
// taça" still mean rice a minute later.
type ConversationRepository interface {
	// Recent returns one chat's turns since a moment, oldest first,
	// capped at limit newest turns.
	Recent(ctx context.Context, chatID string, since time.Time, limit int) ([]domain.Turn, error)
	Append(ctx context.Context, t domain.Turn) (domain.Turn, error)

	// Forget drops everything older than a moment.
	Forget(ctx context.Context, before time.Time) (int64, error)
}

// DigestRepository is the weekly digest's debt book: the row that says
// a digest was owed and never sent.
type DigestRepository interface {
	// Owe records that a chat is owed a digest, and reports whether
	// this call is the one that created the debt. A second call while
	// the first is unpaid gets false and writes nothing: three weeks
	// without an approved template owe one digest, not three.
	Owe(ctx context.Context, chatID string, dueAt time.Time) (bool, error)

	// Owed returns the digest a chat is still waiting for. The second
	// return is false when nothing is owed, which is the normal case.
	Owed(ctx context.Context, chatID string) (domain.Digest, bool, error)

	// Paid marks a debt settled. It is called after the message is
	// away, so a failed send leaves the debt standing.
	Paid(ctx context.Context, id int64) error
}
