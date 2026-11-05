package agent

import (
	"context"
	"encoding/json"

	"github.com/brendalima/despensa/internal/core/domain"
)

func updateItemSchema() map[string]any {
	return object(map[string]any{
		"item_id":  str(descItemID),
		"name":     str(descItemName),
		"category": str(descCategory),
		"minimum":  number(descMinimum),
		"portions": portionList(descPortions),
		"aliases":  array(descAliases, str(descAliases)),
		"archive":  boolean(descArchive),
	}, "item_id")
}

// Pointers, so that a field the model left out and a field it set to
// empty stay different: correcting a portion must not wipe the category.
type updateItemArgs struct {
	ItemID string `json:"item_id"`

	Name     *string  `json:"name"`
	Category *string  `json:"category"`
	Minimum  *float64 `json:"minimum"`
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
