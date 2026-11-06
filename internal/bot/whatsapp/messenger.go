package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/brendalima/despensa/internal/bot"
)

var _ bot.Messenger = (*Messenger)(nil)

const (
	// graphBaseURL and graphVersion are pinned: Meta retires versions
	// on a schedule, and a silent upgrade is a silent change in what
	// the payloads mean.
	//
	// Checked against Meta's own versioning page on 2026-11-06, which
	// names v27.0 the most recent and gives every version at least two
	// years from release. The v23.0 this used to be was good until
	// 2027-10-08, so this is a refresh and not a fix — but it has
	// never been exercised against the live API, because no send has
	// ever reached a phone. The hello_world send that registers a new
	// number is the first thing that will test it.
	graphBaseURL = "https://graph.facebook.com"
	graphVersion = "v27.0"

	// languageCode is the template's language, which Meta matches
	// exactly against the approved template.
	languageCode = "pt_BR"

	// bodyLimit is Meta's cap on an interactive message's body. A
	// receipt is far shorter; a long one loses its buttons rather than
	// being cut in half, and stays undoable through the model.
	bodyLimit = 1024

	// labelLimit is Meta's cap on a button title.
	labelLimit = 20
)

type Messenger struct {
	client *http.Client
	url    string
	token  string
}

type MessengerInput struct {
	PhoneNumberID string
	AccessToken   string

	// BaseURL overrides Meta's host, which is what a test points at.
	BaseURL string

	Client *http.Client
}

func NewMessenger(in MessengerInput) (*Messenger, error) {
	if in.PhoneNumberID == "" {
		return nil, errors.New("missing whatsapp phone number id")
	}
	if in.AccessToken == "" {
		return nil, errors.New("missing whatsapp access token")
	}

	base := in.BaseURL
	if base == "" {
		base = graphBaseURL
	}
	client := in.Client
	if client == nil {
		// Long enough for a cold network, short enough that a stuck
		// send does not hold a goroutine for the rest of the day.
		client = &http.Client{Timeout: 15 * time.Second}
	}

	return &Messenger{
		client: client,
		url:    fmt.Sprintf("%s/%s/%s/messages", base, graphVersion, in.PhoneNumberID),
		token:  in.AccessToken,
	}, nil
}

func MustNewMessenger(in MessengerInput) *Messenger {
	m, err := NewMessenger(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create the whatsapp messenger: %w", err))
	}
	return m
}

func (m *Messenger) SendText(ctx context.Context, to, text string) error {
	return m.send(ctx, map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]any{"preview_url": false, "body": text},
	})
}

func (m *Messenger) SendWithButtons(ctx context.Context, to, text string, buttons []bot.Button) error {
	if len(buttons) == 0 {
		return m.SendText(ctx, to, text)
	}
	if len([]rune(text)) > bodyLimit {
		slog.Warn("receipt too long for buttons, sending it as text", "runes", len([]rune(text)))
		return m.SendText(ctx, to, text)
	}

	replies := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		replies = append(replies, map[string]any{
			"type":  "reply",
			"reply": map[string]any{"id": button.ID, "title": clip(button.Label, labelLimit)},
		})
	}

	return m.send(ctx, map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "interactive",
		"interactive": map[string]any{
			"type":   "button",
			"body":   map[string]any{"text": text},
			"action": map[string]any{"buttons": replies},
		},
	})
}

// SendTemplate is the only way to start a conversation outside the
// 24-hour window, and the only reason the weekly digest needs an
// approved template at all.
func (m *Messenger) SendTemplate(ctx context.Context, to, name string, params []string) error {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]any{
			"name":     name,
			"language": map[string]any{"code": languageCode},
		},
	}

	if len(params) > 0 {
		parameters := make([]map[string]any, 0, len(params))
		for _, p := range params {
			parameters = append(parameters, map[string]any{"type": "text", "text": p})
		}
		template, ok := body["template"].(map[string]any)
		if !ok {
			// Unreachable: the map above is built right here.
			return errors.New("couldn't build the template body")
		}
		template["components"] = []map[string]any{{"type": "body", "parameters": parameters}}
	}
	return m.send(ctx, body)
}

func (m *Messenger) send(ctx context.Context, body map[string]any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("couldn't encode the outgoing message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("couldn't build the send request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Content-Type", "application/json")

	res, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't send the message: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		// Meta explains the refusal in the body, and that explanation
		// is the whole debugging story for a wrong template or an
		// expired token.
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("whatsapp refused the message: %s: %s", res.Status, bytes.TrimSpace(detail))
	}

	// A 2xx means Meta accepted the message, not that it arrived. The
	// id it hands back is the only thing that ties this send to the
	// delivery status that comes later on the webhook, so it is worth
	// a line even on the happy path.
	accepted, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var ack struct {
		Messages []struct {
			ID     string `json:"id"`
			Status string `json:"message_status"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(accepted, &ack); err != nil || len(ack.Messages) == 0 {
		slog.Info("whatsapp accepted the message", "type", body["type"])
		return nil
	}
	slog.Info("whatsapp accepted the message",
		"type", body["type"],
		"whatsapp_message_id", ack.Messages[0].ID,
		"status", ack.Messages[0].Status)
	return nil
}

func clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
