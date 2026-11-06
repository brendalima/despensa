package domain

import "time"

// Digest is a weekly list that was owed to someone and could not be
// sent when it was due — no approved template, or Meta refused it.
//
// It carries no text on purpose. The shopping list is derived on every
// read, so one owed on Sunday and paid on Tuesday says what the pantry
// holds on Tuesday; a stored sentence would be the only thing here that
// can disagree with the ledger.
type Digest struct {
	ID     int64
	ChatID string

	// DueAt is when the digest should have gone out, which is the only
	// thing worth saying about a late one.
	DueAt time.Time

	// DeliveredAt is zero while the digest is still owed.
	DeliveredAt time.Time
}

// Owed reports whether this digest is still waiting for a turn to ride
// out on.
func (d Digest) Owed() bool { return d.DeliveredAt.IsZero() }
