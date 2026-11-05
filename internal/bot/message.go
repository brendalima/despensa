// Package bot is the conversation: it takes one message, lets the model
// use the pantry's tools, and answers with a receipt and a Desfazer
// button. It is agnostic of provider and of model — WhatsApp sits
// behind the Messenger and Webhook ports, the model behind Model — so
// the only thing it knows is the shape of a turn.
package bot

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Message is one thing a person sent, whatever carried it.
type Message struct {
	// Provider and ID are the idempotency key. WhatsApp redelivers
	// anything it is not told was received in time.
	Provider string
	ID       string

	// ChatID is where the answer goes; From is who sent it, checked
	// against the allowed list before anything else happens.
	ChatID string
	From   string

	Text string

	// Choice is a button payload. It is not prose, so it never reaches
	// the model: the id in it is the whole state the button needs.
	Choice string
}

// Button is one tap. The payload is the state — there is no table of
// pending actions, because nothing is pending: the write already
// happened.
type Button struct {
	ID    string
	Label string
}

// undoPrefix marks a button that undoes an event.
const undoPrefix = "undo:"

// UndoButtonID is the payload that undoes one event.
func UndoButtonID(eventID int64) string {
	return undoPrefix + strconv.FormatInt(eventID, 10)
}

// undoTarget reads an event id back out of a button payload.
func undoTarget(choice string) (int64, bool) {
	rest, ok := strings.CutPrefix(choice, undoPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// Turn is one entry of the conversation, in a shape that outlives the
// request: it is what gets stored as a conversation_turns row and read
// back on the next message. An assistant turn carries text and the
// tools it asked for; the user turn that answers carries the results.
type Turn struct {
	Role domain.TurnRole `json:"role"`

	Text        string       `json:"text,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`

	// Raw is the provider's own encoding of this turn, stored opaquely
	// and replayed byte for byte. The fields above are what the loop
	// reasons about; this is what the model is given back, because a
	// turn rebuilt from them would be a turn edited — and an edited
	// history invalidates the reasoning blocks that came with it.
	Raw json.RawMessage `json:"raw,omitempty"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`

	// IsError is a tool that refused, handed back as a result so the
	// model can see what it got wrong and fix it. It is never a
	// protocol error: that would end the turn and leave the person
	// with nothing.
	IsError bool `json:"is_error,omitempty"`
}

func (t Turn) wantsTools() bool { return len(t.ToolCalls) > 0 }
