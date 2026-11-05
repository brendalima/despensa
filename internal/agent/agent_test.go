package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
)

const (
	cup   = domain.Amount(180 * domain.Thousand)
	slice = domain.Thousand
)

const riceID = "11111111-1111-1111-1111-111111111111"

func rice() domain.Item {
	return domain.Item{
		ID:        riceID,
		Name:      "arroz",
		Unit:      domain.UnitGram,
		OnHand:    5000 * domain.Thousand,
		Threshold: 1000 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "taça", One: cup, Source: domain.PortionSeeded},
			{Name: "pacote", One: 5000 * domain.Thousand, Source: domain.PortionSeeded},
		},
		Aliases: []string{"arroz branco"},
	}
}

// The real service satisfies the port without being told to, which is
// the whole reason the port lives in this package.
var _ agent.Pantry = (*services.Pantry)(nil)

// fakePantry records what the tools asked for, so the tests assert on
// the call and not only on the JSON that came back.
type fakePantry struct {
	items []domain.Item
	notes []domain.Note

	recorded []domain.EventInput
	undone   []int64
	changes  []domain.ItemChange
	added    []string

	// The flags stand in for the three states the service reports and
	// the tools only pass along.
	driftOnRecord   bool
	crossedOnRecord bool
	alreadyUndone   bool

	err error
}

func (p *fakePantry) List(context.Context) ([]domain.Item, error) {
	return p.items, p.err
}

func (p *fakePantry) Record(_ context.Context, in []domain.EventInput) ([]domain.Receipt, error) {
	p.recorded = append(p.recorded, in...)
	if p.err != nil {
		return nil, p.err
	}

	receipts := make([]domain.Receipt, 0, len(in))
	for _, entry := range in {
		item := rice()
		if entry.NewItem != nil {
			item = domain.Item{ID: "new", Name: entry.NewItem.Name, Unit: entry.NewItem.Unit}
		}
		before, after := item.OnHand, item.OnHand-cup
		if p.driftOnRecord {
			before, after = 0, -cup
			item.OnHand = after
		}

		receipt := domain.Receipt{
			Item:             item,
			EventID:          int64(len(receipts) + 1),
			Before:           before,
			After:            after,
			ItemCreated:      entry.NewItem != nil,
			CrossedThreshold: p.crossedOnRecord,
			Drifted:          p.driftOnRecord,
		}
		if entry.PortionOneHint > 0 {
			learned := domain.Portion{Name: entry.PortionName, One: entry.PortionOneHint}
			receipt.PortionLearned = &learned
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func (p *fakePantry) Undo(_ context.Context, eventID int64) (domain.Receipt, error) {
	p.undone = append(p.undone, eventID)
	if p.err != nil {
		return domain.Receipt{}, p.err
	}
	item := rice()
	if p.alreadyUndone {
		return domain.Receipt{Item: item, EventID: eventID, Before: item.OnHand, After: item.OnHand}, nil
	}
	return domain.Receipt{Item: item, EventID: eventID, Before: item.OnHand - cup, After: item.OnHand}, nil
}

func (p *fakePantry) Update(_ context.Context, change domain.ItemChange) (domain.Item, error) {
	p.changes = append(p.changes, change)
	if p.err != nil {
		return domain.Item{}, p.err
	}
	return rice(), nil
}

func (p *fakePantry) ShoppingList(context.Context) (domain.ShoppingList, error) {
	if p.err != nil {
		return domain.ShoppingList{}, p.err
	}
	return domain.ShoppingList{Low: p.items, Notes: p.notes}, nil
}

func (p *fakePantry) AddNote(_ context.Context, text string) (domain.Note, error) {
	p.added = append(p.added, text)
	if p.err != nil {
		return domain.Note{}, p.err
	}
	return domain.Note{ID: 7, Text: text}, nil
}

// call runs a tool and decodes the result into a map, which is what the
// model sees.
func call(t *testing.T, set *agent.ToolSet, name, args string) map[string]any {
	t.Helper()
	result, err := set.Call(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s(%s) failed: %v", name, args, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("couldn't marshal the result of %s: %v", name, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("couldn't decode the result of %s: %v", name, err)
	}
	return decoded
}

func newSet(t *testing.T, pantry *fakePantry) *agent.ToolSet {
	t.Helper()
	set, err := agent.New(pantry, domain.SourceMCP)
	if err != nil {
		t.Fatalf("couldn't build the tool set: %v", err)
	}
	return set
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := agent.New(nil, domain.SourceMCP); err == nil {
		t.Error("a tool set with no pantry was accepted")
	}
	if _, err := agent.New(&fakePantry{}, ""); err == nil {
		t.Error("a tool set with no event source was accepted")
	}
}

// The plan says six tools. A seventh one is a change of product, not a
// refactor, so it should break a test on the way in.
func TestThereAreExactlySixTools(t *testing.T) {
	set := newSet(t, &fakePantry{})

	want := []string{
		"list_inventory",
		"record_events",
		"undo_event",
		"update_item",
		"shopping_list",
		"add_shopping_note",
	}

	tools := set.Tools()
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(tools), len(want))
	}
	for i, tool := range tools {
		if tool.Name != want[i] {
			t.Errorf("tool %d is %q, want %q", i, tool.Name, want[i])
		}
		if tool.Description == "" {
			t.Errorf("%s has no description, and the description is the prompt", tool.Name)
		}
		if tool.Handler == nil {
			t.Errorf("%s has no handler", tool.Name)
		}
		if tool.Schema["type"] != "object" {
			t.Errorf("%s does not take an object, which the protocol requires", tool.Name)
		}
		if tool.Schema["additionalProperties"] != false {
			t.Errorf("%s accepts unknown fields, so a misspelling would pass silently", tool.Name)
		}
	}
}

// The descriptions are prompt. These two sentences are the ones that
// invert what a model would otherwise do, and deleting one leaves a bot
// that works and is useless.
func TestDescriptionsCarryTheRulesThatInvertTheObvious(t *testing.T) {
	set := newSet(t, &fakePantry{})

	want := map[string][]string{
		"record_events": {
			"sem pedir confirmação",
			"botão Desfazer",
			"Nunca pergunte quanto pesa",
			"portion_base_hint",
			"numa chamada só",
			"drift: true",
			"pergunte quanto tem",
		},
		"list_inventory": {
			"Chame sempre esta ferramenta antes de escrever",
			"pergunte qual",
			"não escolha sozinho",
		},
		"update_item": {
			"vence qualquer estimativa minha",
		},
		"add_shopping_note": {
			"não é estoque",
		},
	}

	for _, tool := range set.Tools() {
		for _, phrase := range want[tool.Name] {
			if !strings.Contains(tool.Description, phrase) {
				t.Errorf("the description of %s no longer says %q", tool.Name, phrase)
			}
		}
	}
}

// The enums come from the domain, so the schema cannot fall behind it.
func TestSchemaEnumsComeFromTheDomain(t *testing.T) {
	set := newSet(t, &fakePantry{})

	var schema map[string]any
	for _, tool := range set.Tools() {
		if tool.Name == "record_events" {
			schema = tool.Schema
		}
	}
	if schema == nil {
		t.Fatal("record_events is missing")
	}

	entry := schema["properties"].(map[string]any)["entries"].(map[string]any)["items"].(map[string]any)
	props := entry["properties"].(map[string]any)

	kinds := props["kind"].(map[string]any)["enum"].([]any)
	if len(kinds) != len(domain.AllEventKinds()) {
		t.Errorf("kind has %d options, the domain has %d", len(kinds), len(domain.AllEventKinds()))
	}

	units := props["new_item"].(map[string]any)["properties"].(map[string]any)["unit"].(map[string]any)["enum"].([]any)
	if len(units) != len(domain.AllUnits()) {
		t.Errorf("unit has %d options, the domain has %d", len(units), len(domain.AllUnits()))
	}
}

// Rubbish is refused by the schema, before the handler and before any
// transaction. The pantry must not see the call at all.
func TestSchemaRefusesRubbishBeforeTheHandler(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args string
	}{
		{"unknown kind", "record_events", `{"entries":[{"kind":"eaten","portion_name":"taça","portion_qty":1}]}`},
		{"unknown unit", "record_events", `{"entries":[{"kind":"consume","portion_name":"taça","portion_qty":1,"new_item":{"name":"lentilha","unit":"kg"}}]}`},
		{"misspelled field", "record_events", `{"entries":[{"kind":"consume","portion_name":"taça","portion_qty":1,"portion_hint":180}]}`},
		{"missing kind", "record_events", `{"entries":[{"portion_name":"taça","portion_qty":1}]}`},
		{"no entries", "record_events", `{}`},
		{"quantity as text", "record_events", `{"entries":[{"kind":"consume","portion_name":"taça","portion_qty":"duas"}]}`},
		{"event id as text", "undo_event", `{"event_id":"42"}`},
		{"fractional event id", "undo_event", `{"event_id":1.5}`},
		{"no item", "update_item", `{"minimum":500}`},
		{"note as number", "add_shopping_note", `{"text":7}`},
		{"arguments to a tool that takes none", "list_inventory", `{"page":2}`},
		{"not even JSON", "undo_event", `{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pantry := &fakePantry{items: []domain.Item{rice()}}
			set := newSet(t, pantry)

			if _, err := set.Call(context.Background(), tt.tool, json.RawMessage(tt.args)); err == nil {
				t.Fatal("the schema accepted it")
			}

			if len(pantry.recorded)+len(pantry.undone)+len(pantry.changes)+len(pantry.added) > 0 {
				t.Error("the pantry was called with input the schema should have refused")
			}
		})
	}
}

func TestUnknownToolIsNotFound(t *testing.T) {
	set := newSet(t, &fakePantry{})

	_, err := set.Call(context.Background(), "drop_database", json.RawMessage(`{}`))
	var notFound *domain.NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("got %v, want a not-found error", err)
	}
}

// A tool that takes nothing works whether the host sends {} or nothing.
func TestToolsWithNoArgumentsAcceptNothing(t *testing.T) {
	for _, args := range []string{"", "{}"} {
		pantry := &fakePantry{items: []domain.Item{rice()}}
		set := newSet(t, pantry)

		if _, err := set.Call(context.Background(), "list_inventory", json.RawMessage(args)); err != nil {
			t.Errorf("list_inventory with %q failed: %v", args, err)
		}
	}
}
