package agent

// A driver gets two things out of a tool result that the model does not:
// the sentence the project already wrote for the person, and the event
// ids a Desfazer button can carry. Both are read through these
// functions so the view structs stay unexported — the shape of what the
// model reads is this package's business, and a driver reaching into it
// would be a second place that decides how a receipt is worded.

// Text returns the Portuguese sentence a result carries, or empty when
// the result has none (an inventory listing is read by the model, not
// relayed).
func Text(result any) string {
	switch r := result.(type) {
	case recordResult:
		return r.Text
	case receiptView:
		return r.Text
	case shoppingResult:
		return r.Text
	case itemView:
		return r.Text
	default:
		return ""
	}
}

// Recorded is one event a tool wrote: the id a Desfazer button carries,
// and the item it touched. The item is there because a sentence naming
// three things gets three buttons, and three buttons reading "Desfazer"
// tell the person nothing about which is which.
type Recorded struct {
	EventID int64
	Item    string
}

// RecordedEvents returns the events a result wrote, newest last, so a
// receipt can come back with a button per entry. Only record_events
// returns any: undoing an undo is not a thing.
func RecordedEvents(result any) []Recorded {
	r, ok := result.(recordResult)
	if !ok {
		return nil
	}
	recorded := make([]Recorded, 0, len(r.Receipts))
	for _, receipt := range r.Receipts {
		recorded = append(recorded, Recorded{EventID: receipt.EventID, Item: receipt.Item})
	}
	return recorded
}
