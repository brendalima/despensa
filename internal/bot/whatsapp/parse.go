package whatsapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
)

// Meta's payload is a tree of entries and changes, and a delivery can
// carry several of each. Only the message types this bot can act on are
// read, plus the delivery statuses, which act on nothing but are the
// only record of what became of a message after Meta accepted it.
// Everything else — a reaction, a photo — is skipped rather than
// refused, because an unreadable notification is not an error worth a
// redelivery.
type payload struct {
	Entry []struct {
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Messages []message `json:"messages"`
				Statuses []status  `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

type message struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Type string `json:"type"`

	Text struct {
		Body string `json:"body"`
	} `json:"text"`

	Interactive struct {
		Type        string `json:"type"`
		ButtonReply struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"button_reply"`
	} `json:"interactive"`

	// Audio is a voice note or an attached audio file. Meta sends an
	// id and a mime type, never the bytes: whoever wants the sound
	// has to ask for it in a second request.
	Audio struct {
		ID       string `json:"id"`
		MimeType string `json:"mime_type"`
		Voice    bool   `json:"voice"`
	} `json:"audio"`

	// Button is the older, template-reply shape. Its payload is what
	// the template put there, so it carries an id the same way.
	Button struct {
		Payload string `json:"payload"`
		Text    string `json:"text"`
	} `json:"button"`
}

type status struct {
	ID          string `json:"id"`
	RecipientID string `json:"recipient_id"`
	Status      string `json:"status"`
	Errors      []struct {
		Code      int    `json:"code"`
		Title     string `json:"title"`
		Message   string `json:"message"`
		ErrorData struct {
			Details string `json:"details"`
		} `json:"error_data"`
	} `json:"errors"`
}

const (
	typeText        = "text"
	typeAudio       = "audio"
	typeInteractive = "interactive"
	typeButton      = "button"

	fieldMessages = "messages"
)

func (w *Webhook) Parse(body []byte) (bot.Delivery, error) {
	var in payload
	if err := json.Unmarshal(body, &in); err != nil {
		return bot.Delivery{}, fmt.Errorf("couldn't decode the whatsapp payload: %w", err)
	}

	out := bot.Delivery{Messages: []bot.Message{}}
	for _, entry := range in.Entry {
		for _, change := range entry.Changes {
			if change.Field != fieldMessages {
				continue
			}
			for _, m := range change.Value.Messages {
				if msg, ok := m.toBot(); ok {
					out.Messages = append(out.Messages, msg)
				}
			}
			for _, s := range change.Value.Statuses {
				out.Statuses = append(out.Statuses, s.toBot())
			}
		}
	}
	return out, nil
}

func (s status) toBot() bot.Status {
	out := bot.Status{
		MessageID: s.ID,
		Recipient: s.RecipientID,
		Status:    s.Status,
	}
	for _, e := range s.Errors {
		out.Errors = append(out.Errors, bot.StatusError{
			Code:    e.Code,
			Title:   e.Title,
			Message: e.Message,
			Details: e.ErrorData.Details,
		})
	}
	return out
}

// toBot reports false for anything with neither words nor a tap in it.
func (m message) toBot() (bot.Message, bool) {
	msg := bot.Message{
		Provider: domain.SourceWhatsApp,
		ID:       m.ID,
		ChatID:   m.From,
		From:     m.From,
	}
	if msg.ID == "" || msg.From == "" {
		return bot.Message{}, false
	}

	switch m.Type {
	case typeText:
		msg.Text = strings.TrimSpace(m.Text.Body)
	case typeAudio:
		// Voice or file, it is the same message: both are a sentence
		// nobody typed, and both cost a transcription before the model
		// can read them.
		if m.Audio.ID != "" {
			msg.Audio = &bot.Audio{
				MediaID:   m.Audio.ID,
				MediaType: mediaTypeOf(m.Audio.MimeType, "audio/ogg"),
			}
		}
	case typeInteractive:
		msg.Choice = m.Interactive.ButtonReply.ID
	case typeButton:
		msg.Choice = m.Button.Payload
	}

	if msg.Text == "" && msg.Choice == "" && msg.Audio == nil {
		return bot.Message{}, false
	}
	return msg, true
}

// mediaTypeOf drops the parameters Meta appends — a voice note arrives
// as "audio/ogg; codecs=opus" — because everything downstream switches
// on the base type alone.
func mediaTypeOf(mimetype, fallback string) string {
	base, _, _ := strings.Cut(mimetype, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	if base == "" {
		return fallback
	}
	return base
}
