package services

import (
	"context"

	"github.com/brendalima/despensa/internal/core/domain"
)

// PantryService is what every driver depends on: the CLI, the tools the
// model calls, and the bot. None of them knows the repository exists.
type PantryService interface {
	// Record applies a batch of entries in one transaction, so one
	// sentence mentioning three things lands as three events or as
	// none. It creates the items and the portions it needs on the way,
	// which is what makes "comprei um pacote de lentilha" work with no
	// lentilha in the pantry.
	//
	// Nothing is confirmed first: the receipts carry what changed and
	// the event ids to undo, because the failure mode that kills this
	// product is friction, not a wrong row.
	Record(ctx context.Context, in []domain.EventInput) ([]domain.Receipt, error)

	// Undo reverses one event. Undoing an already undone event is not
	// an error — a second tap on the same button must not take the
	// amount off twice.
	Undo(ctx context.Context, eventID int64) (domain.Receipt, error)

	// List returns the live pantry, by name.
	List(ctx context.Context) ([]domain.Item, error)
	GetByID(ctx context.Context, id string) (domain.Item, error)
	History(ctx context.Context, itemID string, limit int) ([]domain.Event, error)

	// Update changes an item's threshold, portions, aliases, name or
	// category, and archives it. Only the fields set in the change are
	// touched.
	Update(ctx context.Context, change domain.ItemChange) (domain.Item, error)

	// ShoppingList is the items at or below their threshold plus the
	// notes, derived on every read.
	ShoppingList(ctx context.Context) (domain.ShoppingList, error)
	AddNote(ctx context.Context, text string) (domain.Note, error)
	RemoveNote(ctx context.Context, id int64) error
}
