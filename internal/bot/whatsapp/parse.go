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
// read; everything else — delivery statuses, reactions, a photo — is
// skipped rather than refused, because an unreadable notification is
// not an error worth a redelivery.
type payload struct {
	Entry []struct {
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Messages []message `json:"messages"`
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

	// Button is the older, template-reply shape. Its payload is what
	// the template put there, so it carries an id the same way.
	Button struct {
		Payload string `json:"payload"`
		Text    string `json:"text"`
	} `json:"button"`
}

const (
	typeText        = "text"
	typeInteractive = "interactive"
	typeButton      = "button"

	fieldMessages = "messages"
)

func (w *Webhook) Parse(body []byte) ([]bot.Message, error) {
	var in payload
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("couldn't decode the whatsapp payload: %w", err)
	}

	messages := []bot.Message{}
	for _, entry := range in.Entry {
		for _, change := range entry.Changes {
			if change.Field != fieldMessages {
				continue
			}
			for _, m := range change.Value.Messages {
				if msg, ok := m.toBot(); ok {
					messages = append(messages, msg)
				}
			}
		}
	}
	return messages, nil
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
	case typeInteractive:
		msg.Choice = m.Interactive.ButtonReply.ID
	case typeButton:
		msg.Choice = m.Button.Payload
	}

	if msg.Text == "" && msg.Choice == "" {
		return bot.Message{}, false
	}
	return msg, true
}
