package domain

import "time"

// Note is a line on the shopping list that is not inventory: the things
// bought once in a while and never counted, like "fermento" or "papel
// toalha". Tracking those as items with portions would be work for no
// gain, and work is what kills this product.
type Note struct {
	ID        int64
	Text      string
	CreatedAt time.Time
}

// ShoppingList is what to buy: the items at or below their threshold,
// plus the notes. It is derived on every read, never stored, so it cannot
// disagree with the pantry.
type ShoppingList struct {
	Low   []Item
	Notes []Note
}

// Empty reports whether there is nothing to buy, so the border can say so
// in one line instead of sending an empty list.
func (l ShoppingList) Empty() bool { return len(l.Low) == 0 && len(l.Notes) == 0 }
