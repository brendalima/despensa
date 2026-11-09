package domain

import "time"

// Note is a line on the shopping list that is not inventory: a one-time
// thing to buy, with no quantity, no portions and no ledger behind it.
// Giving it those would mean inventing a unit for something nobody will
// ever count again, and work is what kills this product.
//
// So a note has an end the computed half of the list does not need: it
// is removed once the thing has been bought. An item that reaches its
// minimum again comes back on its own, while a note that nothing clears
// asks forever for something already in the cupboard.
type Note struct {
	ID        int64
	Text      string
	CreatedAt time.Time
}

// ShoppingList is derived on every read, never stored, so it cannot
// disagree with the pantry.
type ShoppingList struct {
	Low   []Item
	Notes []Note
}

// Empty lets the border say so in one line instead of sending an empty
// list.
func (l ShoppingList) Empty() bool { return len(l.Low) == 0 && len(l.Notes) == 0 }
