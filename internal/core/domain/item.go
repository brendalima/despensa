package domain

import (
	"strings"
	"time"
)

// Unit is what an item is counted in. One of three, closed on purpose:
// everything in a kitchen is a weight, a volume, or a thing you can
// point at.
type Unit string

const (
	UnitGram       Unit = "g"
	UnitMillilitre Unit = "ml"
	// UnitPiece is "un", the abbreviation already printed on Brazilian
	// packaging, so the model and the person use the same word.
	UnitPiece Unit = "un"
)

// AllUnits is the closed list, and the source of the tool schema's enum,
// so adding a unit stays a one-line change.
func AllUnits() []Unit { return []Unit{UnitGram, UnitMillilitre, UnitPiece} }

func (u Unit) Valid() bool {
	for _, known := range AllUnits() {
		if u == known {
			return true
		}
	}
	return false
}

// PortionSource says where a portion's size came from.
const (
	// PortionSeeded is the model's estimate, written down the first time
	// the word was used. It is a guess, and that is the point: being
	// asked to weigh a cup of rice is what makes people stop using an
	// inventory app.
	PortionSeeded = "seeded"
	// PortionCorrected is a size the person gave or fixed themselves.
	// It never gets overwritten by an estimate.
	PortionCorrected = "corrected"
)

// Portion is one word the person uses for an amount of an item, and what
// that word is worth: "taça" = 180 g of rice, "fatia" = 1 un of bread.
//
// Name holds the person's own word, in Portuguese, because it is data and
// not a technical term. Being consistent matters more than being right:
// once "taça" is 180 g, thirty logged cups add up to something, even if a
// real cup is 170 g.
type Portion struct {
	Name   string
	One    Amount // what one of them is worth, in the item's unit
	Source string
}

// Item is one thing in the pantry.
type Item struct {
	ID   string
	Name string
	Unit Unit

	// OnHand is the running total, kept as the sum of every event that
	// has not been undone. It is allowed to be negative: see Drifted.
	OnHand Amount

	// Threshold is the level at or below which the item belongs on the
	// shopping list.
	Threshold Amount

	Category string

	// Portions is the item's vocabulary, and Aliases the other names the
	// person calls it by ("arroz branco" for "arroz").
	Portions []Portion
	Aliases  []string

	ArchivedAt time.Time
	CreatedAt  time.Time
}

// Portion finds a portion by name, ignoring case, so "Taça" and "taça"
// are the same word.
func (i Item) Portion(name string) (Portion, bool) {
	for _, p := range i.Portions {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Portion{}, false
}

// UnitPortion is the portion worth exactly one canonical unit, if the item
// has one. It is what lets bread be counted in "fatias" instead of "un":
// the unit has a name the person actually uses.
func (i Item) UnitPortion() (Portion, bool) {
	for _, p := range i.Portions {
		if p.One == Thousand {
			return p, true
		}
	}
	return Portion{}, false
}

// Display is the on-hand amount as the person should see it.
func (i Item) Display() Amount { return i.OnHand.AtLeastZero() }

// Drifted reports that the running total went below zero, which means the
// arithmetic undercounted: either a purchase was never reported, or a
// portion is set too large. It is a prompt to ask the person for a rough
// count, not an error — it is the only moment the bot asks for a number.
func (i Item) Drifted() bool { return i.OnHand < 0 }

// BelowThreshold reports that the item belongs on the shopping list. A
// threshold of zero means "tell me when it runs out".
func (i Item) BelowThreshold() bool { return i.OnHand <= i.Threshold }

func (i Item) Archived() bool { return !i.ArchivedAt.IsZero() }
