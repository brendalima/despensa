package agent

import (
	"context"
	"encoding/json"

	"github.com/brendalima/despensa/internal/humanize"
)

type shoppingResult struct {
	Low   []itemView `json:"low"`
	Notes []noteView `json:"notes"`
	Text  string     `json:"text"`
}

func (t *tools) shoppingList(ctx context.Context, _ json.RawMessage) (any, error) {
	list, err := t.pantry.ShoppingList(ctx)
	if err != nil {
		return nil, err
	}
	return shoppingResult{
		Low:   toItemViews(list.Low),
		Notes: toNoteViews(list.Notes),
		Text:  humanize.ShoppingList(list),
	}, nil
}

func addShoppingNoteSchema() map[string]any {
	return object(map[string]any{"text": str(descNoteText)}, "text")
}

type addShoppingNoteArgs struct {
	Text string `json:"text"`
}

func (t *tools) addShoppingNote(ctx context.Context, args json.RawMessage) (any, error) {
	var in addShoppingNoteArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}

	note, err := t.pantry.AddNote(ctx, in.Text)
	if err != nil {
		return nil, err
	}
	return noteView{ID: note.ID, Text: note.Text}, nil
}
