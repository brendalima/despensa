package agent

import (
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

// The views are what the model reads back. Two rules shape them.
//
// Quantities are plain numbers in the item's canonical unit — 4600 for
// 4.6 kg of rice — because thousandths are an implementation detail of
// the ledger and a model asked to divide by a thousand will sometimes
// forget. This is the third float boundary, and it is read-only.
//
// Every view that a person will hear about carries a `text` written by
// internal/humanize, so the model relays the project's own wording
// instead of inventing its own rounding.

type itemView struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Unit     domain.Unit   `json:"unit"`
	OnHand   float64       `json:"on_hand"`
	Minimum  float64       `json:"minimum"`
	Category string        `json:"category,omitempty"`
	Aliases  []string      `json:"aliases,omitempty"`
	Portions []portionView `json:"portions,omitempty"`

	// Low and Drifted save the model the comparison, and drifted is the
	// one state it has to act on.
	Low     bool `json:"low,omitempty"`
	Drifted bool `json:"drifted,omitempty"`

	Text string `json:"text"`
}

type portionView struct {
	Name string  `json:"name"`
	Size float64 `json:"size"`
}

type receiptView struct {
	EventID int64  `json:"event_id"`
	ItemID  string `json:"item_id"`
	Item    string `json:"item"`

	Before float64     `json:"before"`
	After  float64     `json:"after"`
	Unit   domain.Unit `json:"unit"`

	PortionLearned   *portionView `json:"portion_learned,omitempty"`
	ItemCreated      bool         `json:"item_created,omitempty"`
	CrossedThreshold bool         `json:"crossed_threshold,omitempty"`
	Drift            bool         `json:"drift,omitempty"`

	Text string `json:"text"`
}

type noteView struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
}

func toItemView(item domain.Item) itemView {
	portions := make([]portionView, 0, len(item.Portions))
	for _, p := range item.Portions {
		portions = append(portions, portionView{Name: p.Name, Size: p.One.Float()})
	}

	return itemView{
		ID:       item.ID,
		Name:     item.Name,
		Unit:     item.Unit,
		OnHand:   item.OnHand.Float(),
		Minimum:  item.Threshold.Float(),
		Category: item.Category,
		Aliases:  item.Aliases,
		Portions: portions,
		Low:      item.BelowThreshold(),
		Drifted:  item.Drifted(),
		Text:     humanize.Item(item),
	}
}

func toItemViews(items []domain.Item) []itemView {
	views := make([]itemView, 0, len(items))
	for _, item := range items {
		views = append(views, toItemView(item))
	}
	return views
}

func toReceiptView(r domain.Receipt) receiptView {
	view := receiptView{
		EventID:          r.EventID,
		ItemID:           r.Item.ID,
		Item:             r.Item.Name,
		Before:           r.Before.Float(),
		After:            r.After.Float(),
		Unit:             r.Item.Unit,
		ItemCreated:      r.ItemCreated,
		CrossedThreshold: r.CrossedThreshold,
		Drift:            r.Drifted,
		Text:             humanize.Receipt(r),
	}
	if r.PortionLearned != nil {
		view.PortionLearned = &portionView{
			Name: r.PortionLearned.Name,
			Size: r.PortionLearned.One.Float(),
		}
	}
	return view
}

func toNoteViews(notes []domain.Note) []noteView {
	views := make([]noteView, 0, len(notes))
	for _, n := range notes {
		views = append(views, noteView{ID: n.ID, Text: n.Text})
	}
	return views
}
