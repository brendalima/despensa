package agent

import (
	"context"
	"encoding/json"

	"github.com/brendalima/despensa/internal/core/domain"
)

func updateItemSchema() map[string]any {
	return object(map[string]any{
		"item_id":       str(descItemID),
		"name":          str(descItemName),
		"category":      str(descCategory),
		"minimum":       number(descMinimum),
		"shopping_mode": enum(descShoppingMode, shoppingModeNames()),
		"portions":      portionList(descPortions),
		"aliases":       array(descAliases, str(descAliases)),
		"archive":       boolean(descArchive),
	}, "item_id")
}

// The enum comes from the domain, so a fourth mode stays a one-line
// change and the schema cannot fall behind the code.
func shoppingModeNames() []string {
	modes := domain.AllShoppingModes()
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		names = append(names, string(m))
	}
	return names
}

// Pointers, so that a field the model left out and a field it set to
// empty stay different: correcting a portion must not wipe the category.
type updateItemArgs struct {
	ItemID string `json:"item_id"`

	Name     *string  `json:"name"`
	Category *string  `json:"category"`
	Minimum  *float64 `json:"minimum"`
	Shopping *string  `json:"shopping_mode"`
	Aliases  []string `json:"aliases"`
	Archive  *bool    `json:"archive"`

	Portions []portionArgs `json:"portions"`
}

func (t *tools) updateItem(ctx context.Context, args json.RawMessage) (any, error) {
	var in updateItemArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}

	change := domain.ItemChange{
		ItemID:   in.ItemID,
		Name:     in.Name,
		Category: in.Category,
		Aliases:  in.Aliases,
		Archive:  in.Archive,
	}
	if in.Minimum != nil {
		threshold := domain.AmountFromFloat(*in.Minimum)
		change.Threshold = &threshold
	}
	if in.Shopping != nil {
		// The service refuses a mode it does not know: the schema's enum
		// already did, and both layers say no on their own.
		mode := domain.ShoppingMode(*in.Shopping)
		change.Shopping = &mode
	}
	// The service writes these as corrected, which is what stops a later
	// estimate from moving a number the person gave.
	for _, p := range in.Portions {
		change.Portions = append(change.Portions, domain.Portion{
			Name: p.Name,
			One:  domain.AmountFromFloat(p.Size),
		})
	}

	item, err := t.pantry.Update(ctx, change)
	if err != nil {
		return nil, err
	}
	return toItemView(item), nil
}
