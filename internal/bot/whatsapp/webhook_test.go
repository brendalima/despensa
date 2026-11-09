package whatsapp_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/bot/whatsapp"
	"github.com/brendalima/despensa/internal/core/domain"
)

const (
	verifyToken = "token-da-assinatura"
	appSecret   = "segredo-do-app"
)

func newWebhook(t *testing.T) *whatsapp.Webhook {
	t.Helper()
	w, err := whatsapp.NewWebhook(verifyToken, appSecret)
	if err != nil {
		t.Fatalf("couldn't create the webhook: %v", err)
	}
	return w
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func post(t *testing.T, body []byte, signature string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(string(body)))
	if signature != "" {
		r.Header.Set("X-Hub-Signature-256", signature)
	}
	return r
}

// Without this check the webhook is a public write API for the
// household's pantry, so every shape of a wrong signature has to fail
// the same way.
func TestVerifyFailsClosed(t *testing.T) {
	w := newWebhook(t)
	body := []byte(`{"entry":[]}`)

	cases := map[string]string{
		"no header":        "",
		"no prefix":        hex.EncodeToString([]byte("whatever")),
		"wrong algorithm":  "sha1=" + hex.EncodeToString([]byte("whatever")),
		"not hex":          "sha256=não-é-hex",
		"empty digest":     "sha256=",
		"another secret":   sign(body, "outro-segredo"),
		"another body":     sign([]byte(`{"entry":[{}]}`), appSecret),
		"truncated digest": sign(body, appSecret)[:20],
	}

	for name, signature := range cases {
		t.Run(name, func(t *testing.T) {
			err := w.Verify(post(t, body, signature), body)
			if !errors.Is(err, whatsapp.ErrSignature) {
				t.Errorf("Verify accepted %s: %v", name, err)
			}
		})
	}
}

func TestVerifyAcceptsMetasSignature(t *testing.T) {
	w := newWebhook(t)
	body := []byte(`{"entry":[{"changes":[]}]}`)

	if err := w.Verify(post(t, body, sign(body, appSecret)), body); err != nil {
		t.Errorf("Verify refused a correctly signed delivery: %v", err)
	}
}

func TestChallengeNeedsTheConfiguredToken(t *testing.T) {
	w := newWebhook(t)

	get := func(query string) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp?"+query, nil)
	}

	challenge, err := w.Challenge(get("hub.mode=subscribe&hub.verify_token=" + verifyToken + "&hub.challenge=1234"))
	if err != nil {
		t.Fatalf("the handshake was refused: %v", err)
	}
	if challenge != "1234" {
		t.Errorf("challenge %q, want 1234", challenge)
	}

	refused := map[string]string{
		"wrong token":  "hub.mode=subscribe&hub.verify_token=errado&hub.challenge=1234",
		"wrong mode":   "hub.mode=unsubscribe&hub.verify_token=" + verifyToken + "&hub.challenge=1234",
		"no challenge": "hub.mode=subscribe&hub.verify_token=" + verifyToken,
	}
	for name, query := range refused {
		t.Run(name, func(t *testing.T) {
			if _, err := w.Challenge(get(query)); err == nil {
				t.Errorf("the handshake accepted %s", name)
			}
		})
	}
}

// Meta's payload is a tree, a delivery can carry several messages, and
// most of what arrives is not a message at all.
func TestParseReadsWhatCanBeActedOn(t *testing.T) {
	w := newWebhook(t)

	body := []byte(`{
	  "object": "whatsapp_business_account",
	  "entry": [{
	    "id": "1",
	    "changes": [
	      {"field": "messages", "value": {"messages": [
	        {"id": "wamid.1", "from": "5511999990000", "type": "text", "text": {"body": "  usei uma taça de arroz  "}},
	        {"id": "wamid.2", "from": "5511999990000", "type": "interactive",
	         "interactive": {"type": "button_reply", "button_reply": {"id": "undo:42", "title": "Desfazer"}}},
	        {"id": "wamid.3", "from": "5511999990000", "type": "button",
	         "button": {"payload": "undo:43", "text": "Desfazer"}},
	        {"id": "wamid.4", "from": "5511999990000", "type": "image", "image": {"id": "x"}},
	        {"id": "wamid.5", "from": "5511999990000", "type": "text", "text": {"body": "   "}},
	        {"id": "", "from": "5511999990000", "type": "text", "text": {"body": "sem id"}}
	      ]}},
	      {"field": "message_template_status_update", "value": {"messages": [
	        {"id": "wamid.9", "from": "5511999990000", "type": "text", "text": {"body": "outro campo"}}
	      ]}}
	    ]
	  }]
	}`)

	delivery, err := w.Parse(body)
	if err != nil {
		t.Fatalf("couldn't parse the payload: %v", err)
	}
	messages := delivery.Messages
	if len(messages) != 3 {
		t.Fatalf("parsed %d messages, want 3: %+v", len(messages), messages)
	}

	first := messages[0]
	if first.Provider != domain.SourceWhatsApp {
		t.Errorf("provider %q", first.Provider)
	}
	if first.ID != "wamid.1" || first.From != "5511999990000" || first.ChatID != "5511999990000" {
		t.Errorf("message read as %+v", first)
	}
	if first.Text != "usei uma taça de arroz" {
		t.Errorf("text %q, want it trimmed", first.Text)
	}
	if first.Choice != "" {
		t.Errorf("a text message carried the choice %q", first.Choice)
	}

	if messages[1].Choice != "undo:42" || messages[1].Text != "" {
		t.Errorf("the button reply read as %+v", messages[1])
	}
	if messages[2].Choice != "undo:43" {
		t.Errorf("the template button read as %+v", messages[2])
	}
}

// A voice note carries an id and a mime type, never the bytes. Meta
// appends codec parameters to the type and nothing downstream wants
// them, so they are dropped here.
func TestParseReadsAVoiceNote(t *testing.T) {
	w := newWebhook(t)

	body := []byte(`{
	  "entry": [{"changes": [{"field": "messages", "value": {"messages": [
	    {"id": "wamid.1", "from": "5511999990000", "type": "audio",
	     "audio": {"id": "media-1", "mime_type": "audio/ogg; codecs=opus", "voice": true}},
	    {"id": "wamid.2", "from": "5511999990000", "type": "audio",
	     "audio": {"id": "media-2", "mime_type": "audio/mpeg", "voice": false}},
	    {"id": "wamid.3", "from": "5511999990000", "type": "audio", "audio": {"mime_type": "audio/ogg"}}
	  ]}}]}]
	}`)

	delivery, err := w.Parse(body)
	if err != nil {
		t.Fatalf("couldn't parse the payload: %v", err)
	}
	// The third has no id, so there is nothing to fetch and nothing to
	// act on: it is skipped like a photo.
	if len(delivery.Messages) != 2 {
		t.Fatalf("parsed %d messages, want 2: %+v", len(delivery.Messages), delivery.Messages)
	}

	note := delivery.Messages[0]
	if note.Audio == nil {
		t.Fatalf("the voice note came through without its audio: %+v", note)
	}
	if note.Audio.MediaID != "media-1" || note.Audio.MediaType != "audio/ogg" {
		t.Errorf("the audio read as %+v", note.Audio)
	}
	if note.Text != "" || note.Choice != "" {
		t.Errorf("a voice note carried words or a tap: %+v", note)
	}

	// A voice recording and an attached file are the same message
	// here: both are a sentence nobody typed.
	file := delivery.Messages[1]
	if file.Audio == nil || file.Audio.MediaType != "audio/mpeg" {
		t.Errorf("the attached audio read as %+v", file.Audio)
	}
}

func TestParseRefusesRubbish(t *testing.T) {
	w := newWebhook(t)

	if _, err := w.Parse([]byte(`not json`)); err == nil {
		t.Error("Parse accepted something that is not JSON")
	}

	empty, err := w.Parse([]byte(`{"entry":[]}`))
	if err != nil {
		t.Fatalf("couldn't parse an empty delivery: %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Errorf("an empty delivery produced %d messages", len(empty.Messages))
	}
	if len(empty.Statuses) != 0 {
		t.Errorf("an empty delivery produced %d statuses", len(empty.Statuses))
	}
}

// A delivery report is the only account of what happened after Meta
// answered "accepted", so a failed one has to survive the parse with
// its reason intact.
func TestParseReadsDeliveryStatuses(t *testing.T) {
	w := newWebhook(t)

	body := []byte(`{
	  "entry": [{"id": "1", "changes": [
	    {"field": "messages", "value": {"statuses": [
	      {"id": "wamid.a", "recipient_id": "5511999990000", "status": "delivered"},
	      {"id": "wamid.b", "recipient_id": "5511999990000", "status": "failed",
	       "errors": [{"code": 131030, "title": "Recipient not allowed",
	                   "error_data": {"details": "add the number to the list"}}]}
	    ]}},
	    {"field": "message_template_status_update", "value": {"statuses": [
	      {"id": "wamid.c", "status": "delivered"}
	    ]}}
	  ]}]
	}`)

	delivery, err := w.Parse(body)
	if err != nil {
		t.Fatalf("couldn't parse the payload: %v", err)
	}
	if len(delivery.Statuses) != 2 {
		t.Fatalf("parsed %d statuses, want 2: %+v", len(delivery.Statuses), delivery.Statuses)
	}

	if got := delivery.Statuses[0]; got.MessageID != "wamid.a" || got.Status != "delivered" || len(got.Errors) != 0 {
		t.Errorf("the delivered status read as %+v", got)
	}

	failed := delivery.Statuses[1]
	if failed.Status != "failed" || failed.Recipient != "5511999990000" {
		t.Errorf("the failed status read as %+v", failed)
	}
	if len(failed.Errors) != 1 {
		t.Fatalf("the failure carried %d errors, want 1", len(failed.Errors))
	}
	if failed.Errors[0].Code != 131030 || failed.Errors[0].Details != "add the number to the list" {
		t.Errorf("the failure reason read as %+v", failed.Errors[0])
	}
}

func TestWebhookNeedsItsSecrets(t *testing.T) {
	if _, err := whatsapp.NewWebhook("", appSecret); err == nil {
		t.Error("NewWebhook accepted an empty verify token")
	}
	if _, err := whatsapp.NewWebhook(verifyToken, ""); err == nil {
		t.Error("NewWebhook accepted an empty app secret")
	}
}
