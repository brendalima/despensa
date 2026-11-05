package services

import (
	"context"

	"github.com/brendalima/despensa/internal/core/domain"
)

// PantryService is what every driver depends on: the CLI, the tools the
// model calls, and the bot.
type PantryService interface {
	// Record applies a batch in one transaction, so one sentence
	// mentioning three things lands as three events or as none. It
	// creates the items and portions it needs on the way.
	//
	// Nothing is confirmed first: the receipts carry the event ids to
	// undo, because the failure mode that kills this product is
	// friction, not a wrong row.
	Record(ctx context.Context, in []domain.EventInput) ([]domain.Receipt, error)

	// Undo reverses one event, and is safe to call twice: a second tap
	// on the same button must not take the amount off twice.
	Undo(ctx context.Context, eventID int64) (domain.Receipt, error)

	// List returns the live pantry, by name.
	List(ctx context.Context) ([]domain.Item, error)
	GetByID(ctx context.Context, id string) (domain.Item, error)
	History(ctx context.Context, itemID string, limit int) ([]domain.Event, error)

	// Update touches only the fields the change sets.
	Update(ctx context.Context, change domain.ItemChange) (domain.Item, error)

	// ShoppingList is the low items plus the notes.
	ShoppingList(ctx context.Context) (domain.ShoppingList, error)
	AddNote(ctx context.Context, text string) (domain.Note, error)
	RemoveNote(ctx context.Context, id int64) error
}

// ChatService is what the bot needs around one incoming message: the
// guard against a redelivery, and the short history that lets "e mais
// uma taça" still mean rice a minute later.
type ChatService interface {
	// Claim reports whether this delivery is the one to process. It is
	// called before the model and before any write.
	Claim(ctx context.Context, provider, messageID string) (bool, error)

	History(ctx context.Context, chatID string) ([]domain.Turn, error)
	Remember(ctx context.Context, chatID string, role domain.TurnRole, content []byte) error

	// Forget drops the turns that fell out of the window.
	Forget(ctx context.Context) (int64, error)
}
