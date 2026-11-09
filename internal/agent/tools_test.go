package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
)

// list_inventory is the tool the model is told to call before writing,
// so what it carries is what the model has to work with: the id, the
// words, and the sizes behind them.
func TestListInventoryCarriesWhatTheModelNeedsToWrite(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	result := call(t, set, "list_inventory", `{}`)
	items := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	item := items[0].(map[string]any)
	if item["id"] != riceID {
		t.Errorf("id is %v, want %s", item["id"], riceID)
	}
	if item["unit"] != "g" {
		t.Errorf("unit is %v, want g", item["unit"])
	}
	// Canonical units, not thousandths: 5 kg of rice reads as 5000 g.
	if item["on_hand"] != 5000.0 {
		t.Errorf("on_hand is %v, want 5000", item["on_hand"])
	}
	if item["minimum"] != 1000.0 {
		t.Errorf("minimum is %v, want 1000", item["minimum"])
	}

	portions := item["portions"].([]any)
	if len(portions) != 2 {
		t.Fatalf("got %d portions, want 2", len(portions))
	}
	first := portions[0].(map[string]any)
	if first["name"] != "taça" || first["size"] != 180.0 {
		t.Errorf("the first portion is %v, want taça = 180", first)
	}
	if aliases := item["aliases"].([]any); len(aliases) != 1 || aliases[0] != "arroz branco" {
		t.Errorf("aliases are %v, want [arroz branco]", item["aliases"])
	}
}

// One sentence naming two things is one call: the batch reaches the
// service whole, so it is one transaction and one receipt.
func TestRecordEventsSendsTheWholeBatchInOneCall(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	result := call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"taça","portion_qty":1,"note":"usei uma taça de arroz e duas fatias de pão"},
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"fatia","portion_qty":2,"portion_base_hint":1}
	]}`)

	if len(pantry.recorded) != 2 {
		t.Fatalf("the service got %d entries, want 2 in one call", len(pantry.recorded))
	}
	if receipts := result["receipts"].([]any); len(receipts) != 2 {
		t.Errorf("got %d receipts, want 2", len(receipts))
	}
	// The text is humanize's, so the model relays the project's wording
	// rather than rounding the numbers itself.
	if text, _ := result["text"].(string); !strings.Contains(text, "arroz") || !strings.Contains(text, "→") {
		t.Errorf("text is %q, want the receipt lines", text)
	}

	first := pantry.recorded[0]
	if first.ItemID != riceID || first.Kind != domain.EventConsume {
		t.Errorf("the first entry is %+v", first)
	}
	if first.PortionQty != domain.QtyOf(1) {
		t.Errorf("portion_qty is %v, want one portion", first.PortionQty)
	}
	if first.RawMessage == "" {
		t.Error("the sentence was dropped, and it is what the ledger is read back with")
	}
	if first.Source != domain.SourceMCP {
		t.Errorf("source is %q, want %q", first.Source, domain.SourceMCP)
	}

	// The estimate arrives in the item's unit: one slice is 1 un.
	if pantry.recorded[1].PortionOneHint != domain.Thousand {
		t.Errorf("portion_base_hint became %v, want one unit", pantry.recorded[1].PortionOneHint)
	}
}

// The source is the tool set's, not the package's: phase C writes
// whatsapp on the same seven tools, and a ledger that calls every line mcp
// cannot answer where a wrong number came from.
func TestRecordEventsWritesTheSourceItWasBuiltWith(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set, err := agent.New(pantry, domain.SourceWhatsApp)
	if err != nil {
		t.Fatalf("couldn't build the tool set: %v", err)
	}

	call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"taça","portion_qty":1}
	]}`)

	if got := pantry.recorded[0].Source; got != domain.SourceWhatsApp {
		t.Errorf("source is %q, want %q", got, domain.SourceWhatsApp)
	}
}

// Half a portion has to survive the trip, because it is how people
// actually talk.
func TestRecordEventsKeepsHalfPortions(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"fatia","portion_qty":1.5,"portion_base_hint":1}
	]}`)

	if got := pantry.recorded[0].PortionQty; got != domain.Qty(1500) {
		t.Errorf("portion_qty is %v, want 1500", got)
	}
}

// An item the model has never seen is created on the way, with no
// question asked, which is the point.
func TestRecordEventsCreatesAnItemOnTheWay(t *testing.T) {
	pantry := &fakePantry{}
	set := newSet(t, pantry)

	result := call(t, set, "record_events", `{"entries":[{
		"kind":"restock","portion_name":"pacote","portion_qty":1,"portion_base_hint":500,
		"new_item":{"name":"lentilha","unit":"g","category":"grãos","aliases":["lentilhas"],
		            "portions":[{"name":"pacote","size":500},{"name":"taça","size":200}],"minimum":100}
	}]}`)

	in := pantry.recorded[0]
	if in.NewItem == nil {
		t.Fatal("new_item was dropped")
	}
	if in.NewItem.Name != "lentilha" || in.NewItem.Unit != domain.UnitGram {
		t.Errorf("the new item is %+v", in.NewItem)
	}
	if in.NewItem.Threshold != domain.AmountOf(100) {
		t.Errorf("minimum became %v, want 100 g", in.NewItem.Threshold)
	}
	if len(in.NewItem.Portions) != 2 {
		t.Fatalf("got %d portions, want 2", len(in.NewItem.Portions))
	}
	if in.NewItem.Portions[0].One != domain.AmountOf(500) {
		t.Errorf("pacote became %v, want 500 g", in.NewItem.Portions[0].One)
	}
	// Seeded, so a number the person gives later outranks it.
	if in.NewItem.Portions[0].Source != domain.PortionSeeded {
		t.Errorf("a portion the model described is %q, want %q",
			in.NewItem.Portions[0].Source, domain.PortionSeeded)
	}

	receipt := result["receipts"].([]any)[0].(map[string]any)
	if receipt["item_created"] != true {
		t.Error("the receipt does not say the item was created")
	}
}

// A new word comes back in the receipt, so a bad estimate is corrected
// in one sentence instead of skewing the totals for weeks.
func TestRecordEventsReportsAPortionItLearned(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	result := call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"concha","portion_qty":1,"portion_base_hint":220}
	]}`)

	receipt := result["receipts"].([]any)[0].(map[string]any)
	learned, ok := receipt["portion_learned"].(map[string]any)
	if !ok {
		t.Fatalf("portion_learned is missing from %v", receipt)
	}
	if learned["name"] != "concha" || learned["size"] != 220.0 {
		t.Errorf("portion_learned is %v, want concha = 220", learned)
	}
	if text, _ := receipt["text"].(string); !strings.Contains(text, "concha") {
		t.Errorf("the receipt text does not mention the new word: %q", text)
	}
}

// drift is what the model is told to act on, so it has to be on the
// receipt by that name.
func TestRecordEventsPassesDriftThrough(t *testing.T) {
	pantry := &fakePantry{driftOnRecord: true}
	set := newSet(t, pantry)

	result := call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"taça","portion_qty":1}
	]}`)

	receipt := result["receipts"].([]any)[0].(map[string]any)
	if receipt["drift"] != true {
		t.Errorf("drift is %v, want true", receipt["drift"])
	}
	if receipt["after"] != -180.0 {
		t.Errorf("after is %v, want the real negative total", receipt["after"])
	}
}

// crossing the minimum rides along on the reply, which is why low stock
// needs no scheduled job.
func TestRecordEventsPassesTheThresholdMomentThrough(t *testing.T) {
	pantry := &fakePantry{crossedOnRecord: true}
	set := newSet(t, pantry)

	result := call(t, set, "record_events", `{"entries":[
		{"item_id":"`+riceID+`","kind":"consume","portion_name":"taça","portion_qty":1}
	]}`)

	receipt := result["receipts"].([]any)[0].(map[string]any)
	if receipt["crossed_threshold"] != true {
		t.Errorf("crossed_threshold is %v, want true", receipt["crossed_threshold"])
	}
}

func TestUndoEventRevertsByID(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	result := call(t, set, "undo_event", `{"event_id":42}`)

	if len(pantry.undone) != 1 || pantry.undone[0] != 42 {
		t.Fatalf("undone %v, want [42]", pantry.undone)
	}
	if result["event_id"] != 42.0 {
		t.Errorf("event_id is %v, want 42", result["event_id"])
	}
	if text, _ := result["text"].(string); !strings.Contains(text, "Desfeito") {
		t.Errorf("text is %q, want the undo wording", text)
	}
}

// A second tap on the same button says so, instead of announcing a
// change that did not happen.
func TestUndoEventSaysSoWhenItWasAlreadyUndone(t *testing.T) {
	pantry := &fakePantry{alreadyUndone: true}
	set := newSet(t, pantry)

	result := call(t, set, "undo_event", `{"event_id":42}`)

	text, _ := result["text"].(string)
	if strings.Contains(text, "Desfeito.") {
		t.Errorf("text is %q, which claims an undo that did not happen", text)
	}
	if !strings.Contains(text, "desfeito") {
		t.Errorf("text is %q, want it to say the event was already undone", text)
	}
}

// A size the person gives reaches the service as a change, and the
// service is what writes it as corrected.
func TestUpdateItemSendsOnlyWhatChanged(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	call(t, set, "update_item", `{"item_id":"`+riceID+`","minimum":2000,"portions":[{"name":"taça","size":200}]}`)

	change := pantry.changes[0]
	if change.ItemID != riceID {
		t.Errorf("item_id is %q", change.ItemID)
	}
	if change.Threshold == nil || *change.Threshold != domain.AmountOf(2000) {
		t.Errorf("minimum is %v, want 2000 g", change.Threshold)
	}
	if len(change.Portions) != 1 || change.Portions[0].One != domain.AmountOf(200) {
		t.Errorf("portions are %v, want taça = 200 g", change.Portions)
	}
	// Not mentioned must not mean emptied.
	if change.Name != nil || change.Category != nil || change.Archive != nil || change.Aliases != nil ||
		change.Shopping != nil {
		t.Errorf("fields nobody touched were sent as changes: %+v", change)
	}
}

// The three modes are the person's decision about the list, and they
// reach the service as the domain's own type: the schema's enum comes
// from there, so a value the schema allows is one the service knows.
func TestUpdateItemCarriesTheShoppingMode(t *testing.T) {
	for _, mode := range domain.AllShoppingModes() {
		t.Run(string(mode), func(t *testing.T) {
			pantry := &fakePantry{items: []domain.Item{rice()}}
			set := newSet(t, pantry)

			call(t, set, "update_item", `{"item_id":"`+riceID+`","shopping_mode":"`+string(mode)+`"}`)

			change := pantry.changes[0]
			if change.Shopping == nil || *change.Shopping != mode {
				t.Fatalf("shopping_mode reached the service as %v, want %q", change.Shopping, mode)
			}
			if change.Threshold != nil || change.Name != nil {
				t.Errorf("the mode dragged other fields along: %+v", change)
			}
		})
	}
}

// A held-back item reads low: true with the mode beside it. The level
// and the decision are two facts, and keeping them separate is what
// lets the model answer "tira o arroz da lista" correctly.
func TestInventoryShowsTheModeBesideTheLevel(t *testing.T) {
	heldBack := rice()
	heldBack.OnHand = 500 * domain.Thousand
	heldBack.Shopping = domain.ShoppingNever

	plain := rice()
	plain.ID = "22222222-2222-2222-2222-222222222222"

	set := newSet(t, &fakePantry{items: []domain.Item{heldBack, plain}})
	result := call(t, set, "list_inventory", `{}`)

	items, ok := result["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v", result["items"])
	}

	first, _ := items[0].(map[string]any)
	if first["shopping_mode"] != "never" {
		t.Errorf("shopping_mode = %v, want never", first["shopping_mode"])
	}
	if first["low"] != true {
		t.Errorf("low = %v, want true: the mode must not move the level", first["low"])
	}

	// The default is not a decision, so it is not reported.
	second, _ := items[1].(map[string]any)
	if _, ok := second["shopping_mode"]; ok {
		t.Errorf("an auto item reported a mode: %v", second["shopping_mode"])
	}
}

// Being bought is a note's end, and the answer names what went: a line
// disappearing with an "ok" is what makes a person check by hand.
func TestRemoveShoppingNote(t *testing.T) {
	pantry := &fakePantry{notes: []domain.Note{{ID: 7, Text: "fermento"}}}
	set := newSet(t, pantry)

	result := call(t, set, "remove_shopping_note", `{"note_id":7}`)

	if len(pantry.removed) != 1 || pantry.removed[0] != 7 {
		t.Fatalf("removed = %v, want the one id", pantry.removed)
	}
	if result["removed"] != "fermento" {
		t.Errorf("removed = %v, want the note's text", result["removed"])
	}
	if text, _ := result["text"].(string); !strings.Contains(text, "fermento") {
		t.Errorf("text = %q, want it to name the note", text)
	}
}

func TestUpdateItemCanArchive(t *testing.T) {
	pantry := &fakePantry{items: []domain.Item{rice()}}
	set := newSet(t, pantry)

	call(t, set, "update_item", `{"item_id":"`+riceID+`","archive":true}`)

	change := pantry.changes[0]
	if change.Archive == nil || !*change.Archive {
		t.Errorf("archive is %v, want true", change.Archive)
	}
}

func TestShoppingListCarriesTheItemsTheNotesAndTheWording(t *testing.T) {
	pantry := &fakePantry{
		items: []domain.Item{rice()},
		notes: []domain.Note{{ID: 3, Text: "papel toalha"}},
	}
	set := newSet(t, pantry)

	result := call(t, set, "shopping_list", `{}`)

	if low := result["low"].([]any); len(low) != 1 {
		t.Errorf("got %d low items, want 1", len(low))
	}
	notes := result["notes"].([]any)
	if len(notes) != 1 {
		t.Fatalf("got %d notes, want 1", len(notes))
	}
	note := notes[0].(map[string]any)
	if note["id"] != 3.0 || note["text"] != "papel toalha" {
		t.Errorf("the note is %v", note)
	}
	if text, _ := result["text"].(string); !strings.Contains(text, "papel toalha") {
		t.Errorf("text is %q, want the rendered list", text)
	}
}

func TestAddShoppingNoteReturnsTheIDToRemoveItBy(t *testing.T) {
	pantry := &fakePantry{}
	set := newSet(t, pantry)

	result := call(t, set, "add_shopping_note", `{"text":"fermento"}`)

	if len(pantry.added) != 1 || pantry.added[0] != "fermento" {
		t.Fatalf("added %v, want [fermento]", pantry.added)
	}
	if result["id"] != 7.0 || result["text"] != "fermento" {
		t.Errorf("the note is %v", result)
	}
}

// A failing dependency comes back as an error for the driver to turn
// into a tool result, never as a half-written success.
func TestToolsReportADependencyFailure(t *testing.T) {
	for _, tt := range []struct{ tool, args string }{
		{"list_inventory", `{}`},
		{"record_events", `{"entries":[{"item_id":"` + riceID + `","kind":"consume","portion_name":"taça","portion_qty":1}]}`},
		{"undo_event", `{"event_id":1}`},
		{"update_item", `{"item_id":"` + riceID + `","minimum":1}`},
		{"shopping_list", `{}`},
		{"add_shopping_note", `{"text":"fermento"}`},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			set := newSet(t, &fakePantry{err: domain.NewNotFoundError("item not found")})

			if _, err := set.Call(context.Background(), tt.tool, json.RawMessage(tt.args)); err == nil {
				t.Error("a failing pantry was reported as a success")
			}
		})
	}
}
