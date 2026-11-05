package agent

import (
	"context"
	"encoding/json"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

func recordEventsSchema() map[string]any {
	newItem := object(map[string]any{
		"name":     str(descItemName),
		"unit":     enum(descUnit, unitNames()),
		"category": str(descCategory),
		"aliases":  array(descAliases, str(descAliases)),
		"portions": portionList(descPortions),
		"minimum":  number(descMinimum),
	}, "name", "unit")

	entry := object(map[string]any{
		"item_id":           str(descItemID),
		"new_item":          newItem,
		"kind":              enum(descKind, eventKindNames()),
		"portion_name":      str(descPortionName),
		"portion_qty":       number(descPortionQty),
		"portion_base_hint": number(descPortionHint),
		"note":              str(descNote),
	}, "kind", "portion_name", "portion_qty")

	return object(map[string]any{
		"entries": array(descEntries, entry),
	}, "entries")
}

// The enums come from the domain, so adding a unit or a kind stays a
// one-line change and the schema cannot fall behind the code.
func unitNames() []string {
	units := domain.AllUnits()
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, string(u))
	}
	return names
}

func eventKindNames() []string {
	kinds := domain.AllEventKinds()
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, string(k))
	}
	return names
}

type recordEventsArgs struct {
	Entries []entryArgs `json:"entries"`
}

type entryArgs struct {
	ItemID  string       `json:"item_id"`
	NewItem *newItemArgs `json:"new_item"`
	Kind    string       `json:"kind"`

	PortionName     string  `json:"portion_name"`
	PortionQty      float64 `json:"portion_qty"`
	PortionBaseHint float64 `json:"portion_base_hint"`

	Note string `json:"note"`
}

type newItemArgs struct {
	Name     string        `json:"name"`
	Unit     string        `json:"unit"`
	Category string        `json:"category"`
	Aliases  []string      `json:"aliases"`
	Portions []portionArgs `json:"portions"`
	Minimum  float64       `json:"minimum"`
}

type portionArgs struct {
	Name string  `json:"name"`
	Size float64 `json:"size"`
}

type recordResult struct {
	Receipts []receiptView `json:"receipts"`
	Text     string        `json:"text"`
}

// recordEvents is one call, one transaction, one receipt: a sentence
// naming three things lands as three events or as none.
func (t *tools) recordEvents(ctx context.Context, args json.RawMessage) (any, error) {
	var in recordEventsArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}

	inputs := make([]domain.EventInput, 0, len(in.Entries))
	for _, entry := range in.Entries {
		inputs = append(inputs, entry.toDomain(t.source))
	}

	receipts, err := t.pantry.Record(ctx, inputs)
	if err != nil {
		return nil, err
	}

	views := make([]receiptView, 0, len(receipts))
	for _, r := range receipts {
		views = append(views, toReceiptView(r))
	}
	return recordResult{Receipts: views, Text: humanize.Receipts(receipts)}, nil
}

// toDomain converts without validating: the service owns every rule, so
// a bad entry is refused in one place and refused the same way whether
// it came from the model or from the CLI.
func (e entryArgs) toDomain(source string) domain.EventInput {
	in := domain.EventInput{
		ItemID:         e.ItemID,
		Kind:           domain.EventKind(e.Kind),
		PortionName:    e.PortionName,
		PortionQty:     domain.QtyFromFloat(e.PortionQty),
		PortionOneHint: domain.AmountFromFloat(e.PortionBaseHint),
		RawMessage:     e.Note,
		Source:         source,
	}
	if e.NewItem != nil {
		in.NewItem = e.NewItem.toDomain()
	}
	return in
}

func (n newItemArgs) toDomain() *domain.NewItem {
	item := &domain.NewItem{
		Name:      n.Name,
		Unit:      domain.Unit(n.Unit),
		Category:  n.Category,
		Aliases:   n.Aliases,
		Threshold: domain.AmountFromFloat(n.Minimum),
	}
	for _, p := range n.Portions {
		item.Portions = append(item.Portions, domain.Portion{
			Name: p.Name,
			One:  domain.AmountFromFloat(p.Size),
			// An estimate, so a number the person gives later wins.
			Source: domain.PortionSeeded,
		})
	}
	return item
}
