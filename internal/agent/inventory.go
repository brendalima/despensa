package agent

import (
	"context"
	"encoding/json"
)

type inventoryResult struct {
	Items []itemView `json:"items"`
}

func (t *tools) listInventory(ctx context.Context, _ json.RawMessage) (any, error) {
	items, err := t.pantry.List(ctx)
	if err != nil {
		return nil, err
	}
	return inventoryResult{Items: toItemViews(items)}, nil
}
