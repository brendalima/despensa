package whatsapp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/bot/whatsapp"
)

// recorder stands in for Meta: it keeps the request and answers with
// whatever the test needs.
type recorder struct {
	server *httptest.Server

	path   string
	header http.Header
	body   map[string]any

	status int
	reply  string
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{status: http.StatusOK, reply: `{"messages":[{"id":"wamid.out"}]}`}

	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.path = req.URL.Path
		r.header = req.Header.Clone()

		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("couldn't read the request: %v", err)
		}
		if err := json.Unmarshal(raw, &r.body); err != nil {
			t.Errorf("the messenger sent something that is not JSON: %v", err)
		}

		w.WriteHeader(r.status)
		_, _ = io.WriteString(w, r.reply)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func newMessenger(t *testing.T, r *recorder) *whatsapp.Messenger {
	t.Helper()
	m, err := whatsapp.NewMessenger(whatsapp.MessengerInput{
		PhoneNumberID: "550000",
		AccessToken:   "token-de-envio",
		BaseURL:       r.server.URL,
		Client:        r.server.Client(),
	})
	if err != nil {
		t.Fatalf("couldn't create the messenger: %v", err)
	}
	return m
}

func TestSendTextHitsTheCloudAPI(t *testing.T) {
	r := newRecorder(t)
	m := newMessenger(t, r)

	if err := m.SendText(context.Background(), "5511999990000", "arroz: 5 kg → 4,8 kg"); err != nil {
		t.Fatalf("couldn't send: %v", err)
	}

	if !strings.HasSuffix(r.path, "/550000/messages") {
		t.Errorf("sent to %q", r.path)
	}
	if got := r.header.Get("Authorization"); got != "Bearer token-de-envio" {
		t.Errorf("authorization header %q", got)
	}
	if got := r.body["messaging_product"]; got != "whatsapp" {
		t.Errorf("messaging_product %v", got)
	}
	if got := r.body["type"]; got != "text" {
		t.Errorf("type %v", got)
	}

	text, ok := r.body["text"].(map[string]any)
	if !ok {
		t.Fatalf("no text in %v", r.body)
	}
	if text["body"] != "arroz: 5 kg → 4,8 kg" {
		t.Errorf("body %v", text["body"])
	}
}

// The payload of the button is the state: there is no table of pending
// actions, because nothing is pending.
func TestSendWithButtonsCarriesThePayload(t *testing.T) {
	r := newRecorder(t)
	m := newMessenger(t, r)

	buttons := []bot.Button{{ID: "undo:100", Label: "Desfazer"}}
	if err := m.SendWithButtons(context.Background(), "5511999990000", "pronto", buttons); err != nil {
		t.Fatalf("couldn't send: %v", err)
	}

	if got := r.body["type"]; got != "interactive" {
		t.Fatalf("type %v", got)
	}
	interactive, ok := r.body["interactive"].(map[string]any)
	if !ok {
		t.Fatalf("no interactive block in %v", r.body)
	}
	action, ok := interactive["action"].(map[string]any)
	if !ok {
		t.Fatalf("no action in %v", interactive)
	}
	replies, ok := action["buttons"].([]any)
	if !ok || len(replies) != 1 {
		t.Fatalf("buttons: %v", action["buttons"])
	}

	first, _ := replies[0].(map[string]any)
	reply, _ := first["reply"].(map[string]any)
	if reply["id"] != "undo:100" || reply["title"] != "Desfazer" {
		t.Errorf("button sent as %v", first)
	}
}

// A label longer than Meta allows would be refused outright, and a
// receipt too long for an interactive body loses its buttons rather
// than being cut in half: the undo is still there through the model.
func TestSendWithButtonsStaysInsideMetasLimits(t *testing.T) {
	r := newRecorder(t)
	m := newMessenger(t, r)

	long := strings.Repeat("a", 30)
	if err := m.SendWithButtons(context.Background(), "5511999990000", "pronto",
		[]bot.Button{{ID: "undo:1", Label: long}}); err != nil {
		t.Fatalf("couldn't send: %v", err)
	}

	interactive, _ := r.body["interactive"].(map[string]any)
	action, _ := interactive["action"].(map[string]any)
	replies, _ := action["buttons"].([]any)
	first, _ := replies[0].(map[string]any)
	reply, _ := first["reply"].(map[string]any)
	if title, _ := reply["title"].(string); len([]rune(title)) != 20 {
		t.Errorf("button title is %d runes, want Meta's 20", len([]rune(title)))
	}

	if err := m.SendWithButtons(context.Background(), "5511999990000", strings.Repeat("x", 1025),
		[]bot.Button{{ID: "undo:1", Label: "Desfazer"}}); err != nil {
		t.Fatalf("couldn't send: %v", err)
	}
	if got := r.body["type"]; got != "text" {
		t.Errorf("a receipt too long for buttons was sent as %v", got)
	}
}

func TestSendTemplateCarriesItsParameters(t *testing.T) {
	r := newRecorder(t)
	m := newMessenger(t, r)

	if err := m.SendTemplate(context.Background(), "5511999990000", "lista_semanal",
		[]string{"arroz, feijão"}); err != nil {
		t.Fatalf("couldn't send: %v", err)
	}

	template, ok := r.body["template"].(map[string]any)
	if !ok {
		t.Fatalf("no template in %v", r.body)
	}
	if template["name"] != "lista_semanal" {
		t.Errorf("template name %v", template["name"])
	}
	language, _ := template["language"].(map[string]any)
	if language["code"] != "pt_BR" {
		t.Errorf("template language %v", language["code"])
	}

	components, ok := template["components"].([]any)
	if !ok || len(components) != 1 {
		t.Fatalf("components: %v", template["components"])
	}
	body, _ := components[0].(map[string]any)
	parameters, _ := body["parameters"].([]any)
	if len(parameters) != 1 {
		t.Fatalf("parameters: %v", body["parameters"])
	}
	param, _ := parameters[0].(map[string]any)
	if param["text"] != "arroz, feijão" {
		t.Errorf("parameter %v", param)
	}
}

// Meta explains a refusal in the body, and that explanation is the
// whole debugging story for an expired token or an unapproved template.
func TestARefusalIsReportedWithMetasExplanation(t *testing.T) {
	r := newRecorder(t)
	r.status = http.StatusBadRequest
	r.reply = `{"error":{"message":"Template name does not exist"}}`
	m := newMessenger(t, r)

	err := m.SendText(context.Background(), "5511999990000", "pronto")
	if err == nil {
		t.Fatal("a refused send reported no error")
	}
	if !strings.Contains(err.Error(), "Template name does not exist") {
		t.Errorf("the error does not carry Meta's explanation: %v", err)
	}
}

func TestMessengerNeedsItsCredentials(t *testing.T) {
	if _, err := whatsapp.NewMessenger(whatsapp.MessengerInput{AccessToken: "x"}); err == nil {
		t.Error("NewMessenger accepted an empty phone number id")
	}
	if _, err := whatsapp.NewMessenger(whatsapp.MessengerInput{PhoneNumberID: "x"}); err == nil {
		t.Error("NewMessenger accepted an empty access token")
	}
}
