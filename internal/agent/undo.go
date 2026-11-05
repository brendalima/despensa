package agent

import (
	"context"
	"encoding/json"

	"github.com/brendalima/despensa/internal/humanize"
)

func undoEventSchema() map[string]any {
	return object(map[string]any{"event_id": integer(descEventID)}, "event_id")
}

type undoEventArgs struct {
	EventID int64 `json:"event_id"`
}

func (t *tools) undoEvent(ctx context.Context, args json.RawMessage) (any, error) {
	var in undoEventArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}

	receipt, err := t.pantry.Undo(ctx, in.EventID)
	if err != nil {
		return nil, err
	}

	view := toReceiptView(receipt)
	// Undone, not Receipt: an undo reads "Desfeito. arroz: ...", and a
	// second tap on the same button says so instead of announcing a
	// change that did not happen.
	view.Text = humanize.Undone(receipt)
	return view, nil
}
