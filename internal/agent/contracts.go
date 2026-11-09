package agent

import (
	"context"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Pantry is the narrow slice of the pantry service the tools use. It is
// declared here, not imported from services, so this package depends on
// a shape it owns: services.Pantry satisfies it without knowing it
// exists, and a test satisfies it with a mock.
//
// Narrower than services.PantryService on purpose — GetByID and History
// have no tool, so they are not a dependency.
type Pantry interface {
	List(ctx context.Context) ([]domain.Item, error)
	Record(ctx context.Context, in []domain.EventInput) ([]domain.Receipt, error)
	Undo(ctx context.Context, eventID int64) (domain.Receipt, error)
	Update(ctx context.Context, change domain.ItemChange) (domain.Item, error)
	ShoppingList(ctx context.Context) (domain.ShoppingList, error)
	AddNote(ctx context.Context, text string) (domain.Note, error)
	RemoveNote(ctx context.Context, id int64) (domain.Note, error)
}
