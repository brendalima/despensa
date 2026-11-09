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

func removeShoppingNoteSchema() map[string]any {
	return object(map[string]any{"note_id": integer(descNoteID)}, "note_id")
}

type removeShoppingNoteArgs struct {
	NoteID int64 `json:"note_id"`
}

// A note is a one-time thing, so being bought is its end. The answer
// names it, because a line vanishing from the list with an "ok" is the
// kind of silence that makes a person check by hand.
func (t *tools) removeShoppingNote(ctx context.Context, args json.RawMessage) (any, error) {
	var in removeShoppingNoteArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}

	note, err := t.pantry.RemoveNote(ctx, in.NoteID)
	if err != nil {
		return nil, err
	}
	return removedNoteView{ID: note.ID, Removed: note.Text, Text: humanize.NoteRemoved(note)}, nil
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
