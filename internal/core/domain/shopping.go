package domain

import "time"

// Note is a line on the shopping list that is not inventory: the things
// bought once in a while and never counted. Tracking those as items with
// portions would be work for no gain, and work is what kills this
// product.
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
