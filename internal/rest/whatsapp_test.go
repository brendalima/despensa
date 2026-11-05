package rest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/rest"
)

// fakeWebhook is the provider's half, so the handler's own rules — the
// signature before the payload, the 200 before the work — are what is
// being tested here.
type fakeWebhook struct {
	verifyErr error
	parseErr  error
	messages  []bot.Message

	verified bool
	parsed   bool
}

func (w *fakeWebhook) Challenge(r *http.Request) (string, error) {
	if token := r.URL.Query().Get("hub.verify_token"); token != "certo" {
		return "", errors.New("wrong verify token")
	}
	return r.URL.Query().Get("hub.challenge"), nil
}

func (w *fakeWebhook) Verify(*http.Request, []byte) error {
	w.verified = true
	return w.verifyErr
}

func (w *fakeWebhook) Parse([]byte) ([]bot.Message, error) {
	w.parsed = true
	return w.messages, w.parseErr
}

// fakeBot records what it was handed, and a test waits on it instead of
// sleeping: the handler answers before the work starts.
type fakeBot struct {
	mu       sync.Mutex
	handled  []bot.Message
	finished sync.WaitGroup
}

func (b *fakeBot) Handle(_ context.Context, msg bot.Message) {
	defer b.finished.Done()

	b.mu.Lock()
	defer b.mu.Unlock()
	b.handled = append(b.handled, msg)
}

func (b *fakeBot) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.handled)
}

func post(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/webhooks/whatsapp", strings.NewReader(body))
}

// The signature is checked before the payload is looked at, and a
// delivery that fails it reaches neither the parser nor the model.
func TestBadSignatureIsRefusedBeforeAnythingIsRead(t *testing.T) {
	webhook := &fakeWebhook{
		verifyErr: errors.New("the payload signature does not match"),
		messages:  []bot.Message{{ID: "wamid.1", Text: "usei uma taça de arroz"}},
	}
	theBot := &fakeBot{}
	handler := rest.MustNewWhatsApp(webhook, theBot)

	res := httptest.NewRecorder()
	handler.Receive(res, post(`{"entry":[]}`))
	handler.Wait()

	if res.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", res.Code)
	}
	if webhook.parsed {
		t.Error("the payload was parsed although the signature was wrong")
	}
	if theBot.count() != 0 {
		t.Errorf("%d messages were handled although the signature was wrong", theBot.count())
	}
}

// The 200 goes out before the work starts, because Meta redelivers
// anything that takes too long and a redelivery is another model call.
func TestReceiveAnswersBeforeTheWork(t *testing.T) {
	webhook := &fakeWebhook{messages: []bot.Message{
		{ID: "wamid.1", Text: "usei uma taça de arroz"},
		{ID: "wamid.2", Choice: "undo:1"},
	}}
	theBot := &fakeBot{}
	theBot.finished.Add(2)

	handler := rest.MustNewWhatsApp(webhook, theBot)

	res := httptest.NewRecorder()
	handler.Receive(res, post(`{"entry":[{}]}`))

	if res.Code != http.StatusOK {
		t.Errorf("status %d, want 200", res.Code)
	}

	theBot.finished.Wait()
	handler.Wait()

	if theBot.count() != 2 {
		t.Errorf("handled %d messages, want both", theBot.count())
	}
}

// A payload that cannot be read will not read any better next time, so
// it is answered rather than retried.
func TestAnUnreadablePayloadIsNotRetried(t *testing.T) {
	webhook := &fakeWebhook{parseErr: errors.New("couldn't decode the whatsapp payload")}
	theBot := &fakeBot{}
	handler := rest.MustNewWhatsApp(webhook, theBot)

	res := httptest.NewRecorder()
	handler.Receive(res, post(`not json`))
	handler.Wait()

	if res.Code != http.StatusOK {
		t.Errorf("status %d, want 200 so Meta stops redelivering", res.Code)
	}
	if theBot.count() != 0 {
		t.Errorf("%d messages were handled from an unreadable payload", theBot.count())
	}
}

func TestVerifyEchoesTheChallenge(t *testing.T) {
	handler := rest.MustNewWhatsApp(&fakeWebhook{}, &fakeBot{})

	res := httptest.NewRecorder()
	handler.Verify(res, httptest.NewRequest(http.MethodGet,
		"/webhooks/whatsapp?hub.mode=subscribe&hub.verify_token=certo&hub.challenge=1234", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", res.Code)
	}
	if got := res.Body.String(); got != "1234" {
		t.Errorf("answered %q, want the challenge", got)
	}
}

func TestVerifyRefusesTheWrongToken(t *testing.T) {
	handler := rest.MustNewWhatsApp(&fakeWebhook{}, &fakeBot{})

	res := httptest.NewRecorder()
	handler.Verify(res, httptest.NewRequest(http.MethodGet,
		"/webhooks/whatsapp?hub.mode=subscribe&hub.verify_token=errado&hub.challenge=1234", nil))

	if res.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", res.Code)
	}
}

func TestWhatsAppHandlerNeedsItsDependencies(t *testing.T) {
	if _, err := rest.NewWhatsApp(nil, &fakeBot{}); err == nil {
		t.Error("NewWhatsApp accepted a missing webhook")
	}
	if _, err := rest.NewWhatsApp(&fakeWebhook{}, nil); err == nil {
		t.Error("NewWhatsApp accepted a missing bot")
	}
}
