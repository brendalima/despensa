package domain

import "time"

// EventKind is what happened. Data-model values, so they stay in English
// even though the conversation that produces them is in Portuguese.
type EventKind string

const (
	EventConsume EventKind = "consume"
	EventRestock EventKind = "restock"
	// EventCorrection is the person stating the truth ("tenho umas três
	// taças"): the quantity is a level, not a change, and the service
	// works out the delta. It is the reset that makes drift harmless.
	EventCorrection EventKind = "correction"
	// EventDiscard is negative like consume, kept apart so "how much
	// rice do I actually eat" stays honest.
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

// Absolute reports whether the quantity is a level rather than a change.
func (k EventKind) Absolute() bool { return k == EventCorrection }

const (
	SourceWhatsApp = "whatsapp"
	SourceMCP      = "mcp"
	SourceCLI      = "cli"
)

// Event is one line of the ledger, and the ledger is the inventory: an
// item's on-hand is the sum of the deltas not undone.
type Event struct {
	ID     int64
	ItemID string
	Kind   EventKind

	// Delta is the signed change, in the item's unit.
	Delta Amount

	// PortionName and PortionQty are what the person said — "2 fatias" —
	// kept so a receipt can be read back in their own words.
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

// NewItem is an item the model creates while logging something, so
// "comprei um pacote de lentilha" works with no lentilha in the pantry.
type NewItem struct {
	Name     string
	Unit     Unit
	Category string
	Aliases  []string
	Portions []Portion

	// Threshold is optional: zero means the service picks one.
	Threshold Amount
}

// EventInput is one entry of a batch. One sentence can mention three
// things, and all three are one call and one transaction.
type EventInput struct {
	// Exactly one of ItemID and NewItem.
	ItemID  string
	NewItem *NewItem

	Kind EventKind

	PortionName string
	PortionQty  Qty

	// PortionOneHint is the model's estimate of what one is worth, used
	// only when the item does not know the word yet. A known portion is
	// never overwritten by an estimate.
	PortionOneHint Amount

	RawMessage string
	Source     string
}

// Receipt is what one recorded entry did, in enough detail for the
// border to write a line the person can check at a glance.
type Receipt struct {
	Item    Item // as it is after the write
	EventID int64

	Before Amount
	After  Amount

	// PortionLearned is set when the word was written down for the first
	// time, so the receipt can say "anotei taça = 180 g" and a bad
	// estimate gets corrected instead of skewing the totals for weeks.
	PortionLearned *Portion
	ItemCreated    bool

	// CrossedThreshold is set when this write is what took the item to
	// or below its threshold, so the low-stock alert rides along on the
	// reply and needs no scheduled job.
	CrossedThreshold bool

	// Drifted is set when the running total is now below zero.
	Drifted bool
}
