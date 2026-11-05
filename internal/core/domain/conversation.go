package domain

import "time"

// Turn is one entry of the short conversation history the bot keeps, so
// "e mais uma taça" still means rice a minute later. Content is the
// driver's own JSON: the core stores it and never looks inside, which is
// what keeps the model's message format out of the domain.
//
// The history is deliberately short-lived. It exists to finish a
// sentence, not to remember a household — the ledger is what remembers.
type Turn struct {
	ID     int64
	ChatID string

	Role    TurnRole
	Content []byte

	CreatedAt time.Time
}

// TurnRole is who spoke. A data-model value, so it stays in English.
type TurnRole string

const (
	TurnUser      TurnRole = "user"
	TurnAssistant TurnRole = "assistant"
)

func AllTurnRoles() []TurnRole { return []TurnRole{TurnUser, TurnAssistant} }

func (r TurnRole) Valid() bool {
	for _, known := range AllTurnRoles() {
		if r == known {
			return true
		}
	}
	return false
}
