package domain

import "time"

// EventKind is what happened. These are data-model values, so they stay
// in English even though the conversation that produces them is in
// Portuguese.
type EventKind string

const (
	// EventConsume is using something up: the delta is negative.
	EventConsume EventKind = "consume"
	// EventRestock is bringing something home: the delta is positive.
	EventRestock EventKind = "restock"
	// EventCorrection is the person stating the truth ("I have about
	// three cups left"). The quantity is absolute, not a change, and the
	// service works out the delta that gets there. It is the reset that
	// makes accumulated drift harmless.
	EventCorrection EventKind = "correction"
	// EventDiscard is throwing something out: negative, like consume,
	// but kept apart so "how much rice do I actually eat" stays honest.
	EventDiscard EventKind = "discard"
)

func AllEventKinds() []EventKind {
	return []EventKind{EventConsume, EventRestock, EventCorrection, EventDiscard}
}

func (k EventKind) Valid() bool {
	for _, known := range AllEventKinds() {
		if k == known {
			return true
		}
	}
	return false
}

// Absolute reports whether the quantity on the event is a level rather
// than a change.
func (k EventKind) Absolute() bool { return k == EventCorrection }

// Where an event came from.
const (
	SourceWhatsApp = "whatsapp"
	SourceMCP      = "mcp"
	SourceCLI      = "cli"
)

// Event is one line of the ledger, which is the inventory: an item's
// on-hand is the sum of the deltas of its events that have not been
// undone. Nothing else is the truth, and that is what makes undo and
// "how much do I use per month" free.
type Event struct {
	ID     int64
	ItemID string
	Kind   EventKind

	// Delta is the signed change this event made, in the item's unit.
	Delta Amount

	// PortionName and PortionQty are what the person said — "2 fatias" —
	// kept alongside the delta so a receipt can be read back in their
	// own words, and so a portion later corrected can be explained.
	PortionName string
	PortionQty  Qty

	// RawMessage is the sentence this came from, for when the arithmetic
	// looks wrong and the question is what was actually said.
	RawMessage string
	Source     string

	UndoneAt  time.Time
	CreatedAt time.Time
}

func (e Event) Undone() bool { return !e.UndoneAt.IsZero() }

// NewItem describes an item the model is creating as part of logging
// something, so "comprei um pacote de lentilha" works with no lentilha in
// the pantry and no form to fill in.
type NewItem struct {
	Name     string
	Unit     Unit
	Category string
	Aliases  []string
	Portions []Portion

	// Threshold is optional: zero means the service picks one.
	Threshold Amount
}

// EventInput is one entry of a batch to record. One sentence can mention
// three things, and all three are one call and one transaction.
type EventInput struct {
	// ItemID names an existing item; NewItem creates one. Exactly one of
	// the two.
	ItemID  string
	NewItem *NewItem

	Kind EventKind

	// PortionName is the word used, and PortionQty how many. For a
	// correction the quantity is the level, not the change.
	PortionName string
	PortionQty  Qty

	// PortionOneHint is the model's estimate of what one of them is
	// worth, used only when the item does not know the word yet. A
	// portion already known is never overwritten by an estimate.
	PortionOneHint Amount

	RawMessage string
	Source     string
}

// Receipt is what one recorded entry did, in enough detail for the border
// to write a line the person can check at a glance.
type Receipt struct {
	Item    Item // as it is after the write
	EventID int64

	Before Amount
	After  Amount

	// PortionLearned is set when the word was written down for the first
	// time, so the first receipt can say "anotei taça = 180 g" and the
	// person gets the chance to correct it.
	PortionLearned *Portion
	ItemCreated    bool

	// CrossedThreshold is set when this write is what took the item to
	// or below its threshold. It rides along on the reply, which is why
	// low-stock alerts need no scheduled job and no message template.
	CrossedThreshold bool

	// Drifted is set when the running total is now below zero, and the
	// border should ask for a rough count.
	Drifted bool
}
