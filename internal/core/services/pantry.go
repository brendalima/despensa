// Package services holds the business rules. It depends on the
// repository through interfaces and never on SQL.
package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/repository"
)

var _ PantryService = (*Pantry)(nil)

// defaultThresholdShare: a fifth of a package is late enough not to nag
// and early enough to buy before it runs out.
const defaultThresholdShare = 5

// historyLimit caps how much ledger a caller can ask for at once.
const historyLimit = 200

type Pantry struct {
	repo repository.PantryRepository
}

func NewPantry(repo repository.PantryRepository) (*Pantry, error) {
	if repo == nil {
		return nil, errors.New("missing pantry repository dependency")
	}
	return &Pantry{repo: repo}, nil
}

func MustNewPantry(repo repository.PantryRepository) *Pantry {
	p, err := NewPantry(repo)
	if err != nil {
		panic(fmt.Errorf("couldn't create pantry service: %w", err))
	}
	return p
}

func (s *Pantry) List(ctx context.Context) ([]domain.Item, error) {
	return s.repo.List(ctx)
}

func (s *Pantry) GetByID(ctx context.Context, id string) (domain.Item, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Pantry) History(ctx context.Context, itemID string, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > historyLimit {
		limit = historyLimit
	}
	return s.repo.Events(ctx, itemID, limit)
}

// Record applies the batch in one transaction.
func (s *Pantry) Record(ctx context.Context, in []domain.EventInput) ([]domain.Receipt, error) {
	if len(in) == 0 {
		return nil, domain.NewValidationError("entries", "is empty")
	}

	receipts := make([]domain.Receipt, 0, len(in))
	err := s.repo.InTx(ctx, func(tx repository.PantryTx) error {
		// A retried InTx must not double the receipts.
		receipts = receipts[:0]
		for i, entry := range in {
			receipt, err := s.record(ctx, tx, entry)
			if err != nil {
				// Which entry failed is what the border reports back.
				return fmt.Errorf("entry %d: %w", i+1, err)
			}
			receipts = append(receipts, receipt)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return receipts, nil
}

// record applies one entry. The order is the contract: nothing is
// written until the item, the portion and the quantity all resolve, so a
// refused entry leaves no half-created item behind.
func (s *Pantry) record(ctx context.Context, tx repository.PantryTx, in domain.EventInput) (domain.Receipt, error) {
	if !in.Kind.Valid() {
		return domain.Receipt{}, domain.NewValidationError("kind", "is not a known event kind")
	}
	if in.Source == "" {
		return domain.Receipt{}, domain.NewValidationError("source", "is empty")
	}
	// "usei nada de arroz" means nothing, while "acabou o arroz" is a
	// correction to zero, and so is an item entering the pantry empty.
	if in.PortionQty == 0 && !in.Kind.Absolute() {
		return domain.Receipt{}, domain.NewValidationError("portion_qty", "is zero")
	}
	if in.PortionQty > domain.MaxQty || in.PortionQty < -domain.MaxQty {
		return domain.Receipt{}, domain.NewValidationError("portion_qty", "is out of range")
	}

	item, created, err := s.resolveItem(ctx, tx, in)
	if err != nil {
		return domain.Receipt{}, err
	}

	portion, learned, err := s.resolvePortion(ctx, tx, item, in)
	if err != nil {
		return domain.Receipt{}, err
	}

	// Portions become an amount in the item's unit here and only here.
	magnitude := portion.One.Scale(in.PortionQty)
	if magnitude < 0 {
		// The kind already says which way it goes.
		return domain.Receipt{}, domain.NewValidationError("portion_qty", "is negative")
	}

	before := item.OnHand
	delta, err := deltaFor(in.Kind, before, magnitude)
	if err != nil {
		return domain.Receipt{}, err
	}

	after := before + delta
	if after > domain.MaxAmount {
		return domain.Receipt{}, domain.NewValidationError("quantity", "would put more in the pantry than fits in it")
	}

	if created {
		if err := tx.CreateItem(ctx, item); err != nil {
			return domain.Receipt{}, err
		}
	}
	if learned != nil {
		if err := tx.UpsertPortion(ctx, item.ID, *learned); err != nil {
			return domain.Receipt{}, err
		}
		item.Portions = append(item.Portions, *learned)
	}

	eventID, err := tx.AddEvent(ctx, domain.Event{
		ItemID:      item.ID,
		Kind:        in.Kind,
		Delta:       delta,
		PortionName: portion.Name,
		PortionQty:  in.PortionQty,
		RawMessage:  in.RawMessage,
		Source:      in.Source,
	})
	if err != nil {
		return domain.Receipt{}, err
	}
	if err := tx.SetOnHand(ctx, item.ID, after); err != nil {
		return domain.Receipt{}, err
	}
	item.OnHand = after

	return domain.Receipt{
		Item:           item,
		EventID:        eventID,
		Before:         before,
		After:          after,
		PortionLearned: learned,
		ItemCreated:    created,
		// The moment, not the state: an item already low must not
		// raise the alert on every use.
		CrossedThreshold: item.BelowThreshold() && before > item.Threshold,
		Drifted:          item.Drifted(),
	}, nil
}

// resolveItem finds the item, or builds the new one the entry describes.
// The new item comes back unsaved: nothing is written until the whole
// entry is known to be good.
func (s *Pantry) resolveItem(ctx context.Context, tx repository.PantryTx, in domain.EventInput) (domain.Item, bool, error) {
	switch {
	case in.ItemID != "" && in.NewItem != nil:
		return domain.Item{}, false, domain.NewValidationError("item", "names an existing item and a new one at once")

	case in.ItemID != "":
		item, err := tx.LockByID(ctx, in.ItemID)
		return item, false, err

	case in.NewItem != nil:
		item, err := newItem(*in.NewItem)
		if err != nil {
			return domain.Item{}, false, err
		}
		// Beats a duplicate "Arroz" next to "arroz" when the model
		// describes something it did not see in the list.
		if existing, err := tx.LockByName(ctx, item.Name); err == nil {
			return existing, false, nil
		} else if !isNotFound(err) {
			return domain.Item{}, false, err
		}
		return item, true, nil

	default:
		return domain.Item{}, false, domain.NewValidationError("item", "names neither an existing item nor a new one")
	}
}

// resolvePortion finds what one portion is worth. A known word always
// wins, so the same sentence always means the same amount; an unknown one
// is learned from the model's estimate, and refused only without one.
func (s *Pantry) resolvePortion(ctx context.Context, tx repository.PantryTx, item domain.Item, in domain.EventInput) (domain.Portion, *domain.Portion, error) {
	name := strings.TrimSpace(in.PortionName)
	if name == "" {
		return domain.Portion{}, nil, domain.NewValidationError("portion_name", "is empty")
	}

	if known, ok := item.Portion(name); ok {
		return known, nil, nil
	}

	if in.PortionOneHint <= 0 {
		known := make([]string, 0, len(item.Portions))
		for _, p := range item.Portions {
			known = append(known, p.Name)
		}
		return domain.Portion{}, nil, domain.NewUnknownPortionError(item.Name, name, known)
	}
	if in.PortionOneHint > domain.MaxAmount {
		return domain.Portion{}, nil, domain.NewValidationError("portion_one_hint", "is out of range")
	}

	learned := domain.Portion{Name: name, One: in.PortionOneHint, Source: domain.PortionSeeded}
	return learned, &learned, nil
}

// deltaFor turns a magnitude into a signed change. A correction carries
// a level, so its delta is whatever gets the total there.
func deltaFor(kind domain.EventKind, onHand, magnitude domain.Amount) (domain.Amount, error) {
	switch kind {
	case domain.EventRestock:
		return magnitude, nil
	case domain.EventConsume, domain.EventDiscard:
		return -magnitude, nil
	case domain.EventCorrection:
		return magnitude - onHand, nil
	default:
		return 0, domain.NewValidationError("kind", "is not a known event kind")
	}
}

// Undo reverses one event and is safe to call twice.
func (s *Pantry) Undo(ctx context.Context, eventID int64) (domain.Receipt, error) {
	var receipt domain.Receipt
	err := s.repo.InTx(ctx, func(tx repository.PantryTx) error {
		event, err := tx.LockEvent(ctx, eventID)
		if err != nil {
			return err
		}
		item, err := tx.LockByID(ctx, event.ItemID)
		if err != nil {
			return err
		}

		// A second tap on the same button changes nothing.
		if event.Undone() {
			receipt = domain.Receipt{
				Item:    item,
				EventID: event.ID,
				Before:  item.OnHand,
				After:   item.OnHand,
				Drifted: item.Drifted(),
			}
			return nil
		}

		before := item.OnHand
		after := before - event.Delta
		if err := tx.MarkEventUndone(ctx, event.ID); err != nil {
			return err
		}
		if err := tx.SetOnHand(ctx, item.ID, after); err != nil {
			return err
		}
		item.OnHand = after

		receipt = domain.Receipt{
			Item:    item,
			EventID: event.ID,
			Before:  before,
			After:   after,
			Drifted: item.Drifted(),
		}
		return nil
	})
	if err != nil {
		return domain.Receipt{}, err
	}
	return receipt, nil
}

func (s *Pantry) Update(ctx context.Context, change domain.ItemChange) (domain.Item, error) {
	if change.Empty() {
		return domain.Item{}, domain.NewValidationError("change", "asks for nothing")
	}

	var updated domain.Item
	err := s.repo.InTx(ctx, func(tx repository.PantryTx) error {
		item, err := tx.LockByID(ctx, change.ItemID)
		if err != nil {
			return err
		}

		if change.Name != nil {
			name := strings.TrimSpace(*change.Name)
			if name == "" {
				return domain.NewValidationError("name", "is empty")
			}
			item.Name = name
		}
		if change.Category != nil {
			item.Category = strings.TrimSpace(*change.Category)
		}
		if change.Threshold != nil {
			if *change.Threshold < 0 || *change.Threshold > domain.MaxAmount {
				return domain.NewValidationError("threshold", "is out of range")
			}
			item.Threshold = *change.Threshold
		}
		if change.Archive != nil {
			switch {
			case *change.Archive && !item.Archived():
				item.ArchivedAt = time.Now().UTC()
			case !*change.Archive:
				item.ArchivedAt = time.Time{}
			}
		}

		if err := tx.UpdateItem(ctx, item); err != nil {
			return err
		}

		// The person's own number: written as corrected, so no later
		// estimate can move it.
		for _, p := range change.Portions {
			if strings.TrimSpace(p.Name) == "" {
				return domain.NewValidationError("portion_name", "is empty")
			}
			if p.One <= 0 || p.One > domain.MaxAmount {
				return domain.NewValidationError("portion_one", "is out of range")
			}
			p.Source = domain.PortionCorrected
			if err := tx.UpsertPortion(ctx, item.ID, p); err != nil {
				return err
			}
		}
		if change.Aliases != nil {
			if err := tx.ReplaceAliases(ctx, item.ID, change.Aliases); err != nil {
				return err
			}
		}

		// Read it back so the caller gets what is stored, not the
		// patched copy.
		updated, err = tx.LockByID(ctx, item.ID)
		return err
	})
	if err != nil {
		return domain.Item{}, err
	}
	return updated, nil
}

func (s *Pantry) ShoppingList(ctx context.Context) (domain.ShoppingList, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return domain.ShoppingList{}, err
	}
	notes, err := s.repo.Notes(ctx)
	if err != nil {
		return domain.ShoppingList{}, err
	}

	low := []domain.Item{}
	for _, item := range items {
		if item.BelowThreshold() {
			low = append(low, item)
		}
	}
	return domain.ShoppingList{Low: low, Notes: notes}, nil
}

func (s *Pantry) AddNote(ctx context.Context, text string) (domain.Note, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return domain.Note{}, domain.NewValidationError("text", "is empty")
	}
	return s.repo.AddNote(ctx, trimmed)
}

func (s *Pantry) RemoveNote(ctx context.Context, id int64) error {
	return s.repo.RemoveNote(ctx, id)
}

// newItem validates what the model described and fills in the id and
// threshold.
func newItem(in domain.NewItem) (domain.Item, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return domain.Item{}, domain.NewValidationError("name", "is empty")
	}
	if !in.Unit.Valid() {
		return domain.Item{}, domain.NewValidationError("canonical_unit", "is not one of g, ml, un")
	}

	item := domain.Item{
		ID:        uuid.New().String(),
		Name:      name,
		Unit:      in.Unit,
		Threshold: in.Threshold,
		Category:  strings.TrimSpace(in.Category),
		Portions:  []domain.Portion{},
		Aliases:   []string{},
	}

	for _, p := range in.Portions {
		if strings.TrimSpace(p.Name) == "" {
			return domain.Item{}, domain.NewValidationError("portion_name", "is empty")
		}
		if p.One <= 0 || p.One > domain.MaxAmount {
			return domain.Item{}, domain.NewValidationError("portion_one", "is out of range")
		}
		if p.Source == "" {
			p.Source = domain.PortionSeeded
		}
		item.Portions = append(item.Portions, p)
	}
	for _, alias := range in.Aliases {
		if trimmed := strings.TrimSpace(alias); trimmed != "" {
			item.Aliases = append(item.Aliases, trimmed)
		}
	}

	if item.Threshold == 0 {
		item.Threshold = defaultThreshold(item.Portions)
	}
	if item.Threshold < 0 || item.Threshold > domain.MaxAmount {
		return domain.Item{}, domain.NewValidationError("threshold", "is out of range")
	}
	return item, nil
}

// defaultThreshold is a fifth of the largest portion, which is the
// package in practice. With no portions it stays zero: "tell me when it
// runs out".
func defaultThreshold(portions []domain.Portion) domain.Amount {
	var largest domain.Amount
	for _, p := range portions {
		if p.One > largest {
			largest = p.One
		}
	}
	return largest / defaultThresholdShare
}

func isNotFound(err error) bool {
	var notFound *domain.NotFoundError
	return errors.As(err, &notFound)
}
