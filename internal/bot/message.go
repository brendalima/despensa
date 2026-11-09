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

	// Audio is a voice note or an audio file, when one came instead of
	// words. It carries the provider's id and nothing else: the bytes
	// are fetched after the claim, so a redelivery never pays for the
	// same transcription twice.
	Audio *Audio

	// Choice is a button payload. It is not prose, so it never reaches
	// the model: the id in it is the whole state the button needs.
	Choice string
}

// Audio names an attachment the transcriber will have to hear. A voice
// note and an attached audio file are the same thing here: both have
// to reach the transcriber before the model can read them.
type Audio struct {
	MediaID   string
	MediaType string
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

	// Truncated means the model hit its token ceiling mid-turn, so
	// this turn is half of something: a tool call whose arguments stop
	// in the middle, or a sentence that does not finish.
	//
	// It is never stored. A turn that was cut off is not conversation,
	// and storing one carrying an unanswered tool call would poison
	// the next request outright — an assistant tool_use with no
	// matching tool_result is refused by the API.
	Truncated bool `json:"-"`

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

// Status is a delivery report. WhatsApp answers a send with an id and
// nothing more; whether the message reached the phone, failed, or was
// read arrives later on the same webhook, under the same field. It is
// the only account of what happened after Meta said "accepted", so it
// is parsed and logged even though nothing acts on it.
type Status struct {
	MessageID string
	Recipient string
	Status    string
	Errors    []StatusError
}

// StatusError is why a delivery failed, in Meta's own words.
type StatusError struct {
	Code    int
	Title   string
	Message string
	Details string
}

// Delivery is one webhook call: the messages to act on and the
// delivery reports to record.
type Delivery struct {
	Messages []Message
	Statuses []Status
}
