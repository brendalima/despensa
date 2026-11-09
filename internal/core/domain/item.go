package domain

import (
	"strings"
	"time"
)

// Unit is what an item is counted in. Closed on purpose: everything in a
// kitchen is a weight, a volume, or a thing you can point at.
type Unit string

const (
	UnitGram       Unit = "g"
	UnitMillilitre Unit = "ml"
	// UnitPiece is "un", the abbreviation already printed on Brazilian
	// packaging.
	UnitPiece Unit = "un"
)

// AllUnits is the source of the tool schema's enum, so adding a unit
// stays a one-line change.
func AllUnits() []Unit { return []Unit{UnitGram, UnitMillilitre, UnitPiece} }

func (u Unit) Valid() bool {
	for _, known := range AllUnits() {
		if u == known {
			return true
		}
	}
	return false
}

// ShoppingMode is what an item wants from the shopping list, which is
// the one thing about an item that is a decision rather than a fact.
type ShoppingMode string

const (
	// ShoppingAuto follows the minimum, which is what every item does
	// until the person says otherwise.
	ShoppingAuto ShoppingMode = "auto"
	// ShoppingNever keeps an item off the list even when it is low: "I
	// know the rice is low, I am not buying rice now."
	ShoppingNever ShoppingMode = "never"
	// ShoppingAlways keeps an item on the list even when it is full:
	// "buy coffee anyway while you are there." It ends by itself, on the
	// restock that clears the minimum.
	ShoppingAlways ShoppingMode = "always"
)

// AllShoppingModes is the source of the tool schema's enum, the same way
// AllUnits is.
func AllShoppingModes() []ShoppingMode {
	return []ShoppingMode{ShoppingAuto, ShoppingNever, ShoppingAlways}
}

func (m ShoppingMode) Valid() bool {
	for _, known := range AllShoppingModes() {
		if m == known {
			return true
		}
	}
	return false
}

// OrAuto resolves the zero value. A struct literal that never mentions
// the mode means the default, not a decision, and the column's CHECK
// never sees an empty string.
func (m ShoppingMode) OrAuto() ShoppingMode {
	if m == "" {
		return ShoppingAuto
	}
	return m
}

const (
	// PortionSeeded is the model's estimate, written down on first use.
	PortionSeeded = "seeded"
	// PortionCorrected is a size the person gave. An estimate never
	// overwrites it.
	PortionCorrected = "corrected"
)

// Portion is one word the person uses for an amount, and what it is
// worth: "taça" = 180 g of rice, "fatia" = 1 un of bread. Name holds
// their own word, so it is data and in Portuguese.
//
// Being consistent matters more than being right: once "taça" is 180 g,
// thirty logged cups add up to something.
type Portion struct {
	Name   string
	One    Amount // what one of them is worth, in the item's unit
	Source string
}

type Item struct {
	ID   string
	Name string
	Unit Unit

	// OnHand is the sum of the deltas of the events not undone. It may
	// be negative: see Drifted.
	OnHand Amount

	// Threshold is the level at or below which the item belongs on the
	// shopping list.
	Threshold Amount

	Category string

	// Shopping is the person's standing answer about the shopping list.
	// It changes nothing else: a 'never' item is still in the pantry,
	// still counted, and keeps its minimum for when it comes back.
	Shopping ShoppingMode

	// Portions is the item's vocabulary; Aliases the other names the
	// person calls it by.
	Portions []Portion
	Aliases  []string

	ArchivedAt time.Time
	CreatedAt  time.Time
}

// Portion finds a word, ignoring case: "Taça" and "taça" are one word.
func (i Item) Portion(name string) (Portion, bool) {
	for _, p := range i.Portions {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Portion{}, false
}

// UnitPortion is the portion worth exactly one canonical unit. It is what
// lets bread be counted in "fatias" instead of "un".
//
// A portion whose name is the unit's own abbreviation does not count as
// one. "g" on an item measured in grams is not a word anybody invented,
// it is the unit written out, and counting in it is what stops 5 kg
// from reading as 5 kg. The model writes these down — it gave rice a
// portion called "g" and milk one called "ml" — so this has to hold for
// rows already in the database, not only for what is written next.
func (i Item) UnitPortion() (Portion, bool) {
	for _, p := range i.Portions {
		if p.One == Thousand && !i.NamesUnit(p) {
			return p, true
		}
	}
	return Portion{}, false
}

// NamesUnit reports that a portion is the canonical unit under its own
// name, adding no vocabulary to the item.
func (i Item) NamesUnit(p Portion) bool {
	return strings.EqualFold(strings.TrimSpace(p.Name), string(i.Unit))
}

func (i Item) Display() Amount { return i.OnHand.AtLeastZero() }

// Drifted reports that the running total went below zero, so the
// arithmetic undercounted. It is a prompt to ask for a rough count, not
// an error.
func (i Item) Drifted() bool { return i.OnHand < 0 }

// BelowThreshold reports that the item belongs on the shopping list. A
// threshold of zero means "tell me when it runs out".
func (i Item) BelowThreshold() bool { return i.OnHand <= i.Threshold }

// OnShoppingList reports that the item belongs on the list now: the
// level, unless the person has overruled it. The zero mode falls through
// to the minimum, so an Item built without one behaves the way every
// item did before this field existed.
func (i Item) OnShoppingList() bool {
	switch i.Shopping {
	case ShoppingNever:
		return false
	case ShoppingAlways:
		return true
	default:
		return i.BelowThreshold()
	}
}

// Pinned reports a standing "buy it anyway": the one mode that puts an
// item on the list without the level saying so.
func (i Item) Pinned() bool { return i.Shopping == ShoppingAlways }

// HeldBack reports that the person asked not to restock this for now.
func (i Item) HeldBack() bool { return i.Shopping == ShoppingNever }

func (i Item) Archived() bool { return !i.ArchivedAt.IsZero() }
