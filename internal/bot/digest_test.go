package bot_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
)

const template = "lista_semanal"

func lowRice() domain.ShoppingList {
	item := rice()
	item.OnHand = 320 * domain.Thousand
	return domain.ShoppingList{
		Low:   []domain.Item{item},
		Notes: []domain.Note{{ID: 1, Text: "papel toalha"}},
	}
}

func newDigest(t *testing.T, name string, list domain.ShoppingList) (*bot.Digest, *fakeDigests, *fakeMessenger) {
	t.Helper()

	digests := newFakeDigests()
	digests.list = list
	messenger := &fakeMessenger{}

	d, err := bot.NewDigest(bot.DigestInput{
		Digests:    digests,
		Messenger:  messenger,
		Recipients: []string{mine},
		Template:   name,
	})
	if err != nil {
		t.Fatalf("couldn't create the digest: %v", err)
	}
	return d, digests, messenger
}

// A weekly message that says "nothing is missing" is the exact habit
// that teaches a person to stop reading the weekly message — and it is
// the only message here that costs money per send.
func TestAnEmptyListSendsNothingAndOwesNothing(t *testing.T) {
	d, digests, messenger := newDigest(t, template, domain.ShoppingList{})

	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if len(messenger.sent) != 0 {
		t.Fatalf("an empty list was sent anyway: %+v", messenger.sent)
	}
	if len(digests.owes) != 0 {
		t.Fatalf("an empty list was owed: %v", digests.owes)
	}
}

func TestTheDigestGoesOutAsTheApprovedTemplate(t *testing.T) {
	d, digests, messenger := newDigest(t, template, lowRice())

	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	last := messenger.last(t)
	if last.to != mine {
		t.Errorf("sent to %q, want %q", last.to, mine)
	}
	// fakeMessenger records a template send by name.
	if last.text != template {
		t.Errorf("sent template %q, want %q", last.text, template)
	}
	if len(digests.owes) != 0 {
		t.Errorf("a sent digest was also owed: %v", digests.owes)
	}
}

// The parameter is substituted into a sentence the approved template
// already owns, so it has to be one line: a newline in it would break
// that sentence rather than lay out a list.
func TestTheTemplateParameterIsOneLine(t *testing.T) {
	list := lowRice()
	list.Notes = append(list.Notes, domain.Note{ID: 2, Text: "detergente\ne esponja"})

	digests := newFakeDigests()
	digests.list = list
	messenger := &recordingMessenger{}

	d := bot.MustNewDigest(bot.DigestInput{
		Digests:    digests,
		Messenger:  messenger,
		Recipients: []string{mine},
		Template:   template,
	})
	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if len(messenger.params) != 2 {
		t.Fatalf("got %d parameters, want 2: %v", len(messenger.params), messenger.params)
	}
	param := messenger.params[1]
	if strings.ContainsAny(param, "\n\r\t") {
		t.Errorf("the parameter carries whitespace a template cannot hold: %q", param)
	}
	if !strings.Contains(param, "arroz") || !strings.Contains(param, "papel toalha") {
		t.Errorf("the parameter dropped something from the list: %q", param)
	}
}

// Meta matches the parameter count exactly, and the order is the
// template's: "sua despensa nº {{1}}" then "Itens em falta: {{2}}".
// Swapped, this would send the list as the pantry number and the number
// as the list — once a week, to a phone, with nothing in the logs to
// say so.
func TestTheTemplateParametersAreTheNumberThenTheList(t *testing.T) {
	digests := newFakeDigests()
	digests.list = lowRice()
	messenger := &recordingMessenger{}

	d := bot.MustNewDigest(bot.DigestInput{
		Digests:    digests,
		Messenger:  messenger,
		Recipients: []string{mine},
		Template:   template,
	})
	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	if len(messenger.params) != 2 {
		t.Fatalf("got %d parameters, want 2: %v", len(messenger.params), messenger.params)
	}
	if messenger.params[0] != "1" {
		t.Errorf("the first parameter is %q, want the pantry number \"1\"", messenger.params[0])
	}
	if !strings.Contains(messenger.params[1], "arroz") {
		t.Errorf("the second parameter is %q, want the list", messenger.params[1])
	}
}

// Until Meta approves a template this is the only path, and it has to
// lose nothing: the list is late, not gone.
func TestWithoutATemplateTheDigestIsOwed(t *testing.T) {
	d, digests, messenger := newDigest(t, "", lowRice())

	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if len(messenger.sent) != 0 {
		t.Fatalf("something was sent without a template: %+v", messenger.sent)
	}
	if len(digests.owes) != 1 || digests.owes[0] != mine {
		t.Fatalf("the digest was not owed: %v", digests.owes)
	}
}

// A template pending review, a template renamed, a window that was
// never open: Meta refusing the send is the case the debt book exists
// for, so it is a fallback and not a failure.
func TestARefusedTemplateIsOwedRatherThanLost(t *testing.T) {
	d, digests, messenger := newDigest(t, template, lowRice())
	messenger.err = errors.New("whatsapp refused the message: 400")

	if err := d.Send(context.Background()); err != nil {
		t.Fatalf("a refused template should fall back, not fail: %v", err)
	}
	if len(digests.owes) != 1 {
		t.Fatalf("the refused digest was not owed: %v", digests.owes)
	}
}

// Three weeks without an approved template owe one digest, not three:
// it is the same list, and three copies of it on the next message read
// as a broken bot.
func TestOwingTwiceLeavesOneDebt(t *testing.T) {
	d, digests, _ := newDigest(t, "", lowRice())
	ctx := context.Background()

	for range 3 {
		if err := d.Send(ctx); err != nil {
			t.Fatalf("send failed: %v", err)
		}
	}
	if len(digests.owed) != 1 {
		t.Fatalf("got %d standing debts, want 1", len(digests.owed))
	}
}

// The digest rides out on the next message, after that message's own
// answer: a second message that arrives first reads as the bot ignoring
// what was just said.
func TestAnOwedDigestRidesOutOnTheNextMessage(t *testing.T) {
	h := newHarness(t, answer("beleza"))
	h.digests.list = lowRice()
	if _, err := h.digests.Owe(context.Background(), mine); err != nil {
		t.Fatal(err)
	}

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	if len(h.messenger.sent) != 2 {
		t.Fatalf("got %d messages, want the answer and the digest: %+v", len(h.messenger.sent), h.messenger.sent)
	}
	if h.messenger.sent[0].text != "beleza" {
		t.Errorf("the first message was not the answer: %q", h.messenger.sent[0].text)
	}
	if !strings.Contains(h.messenger.sent[1].text, "Resumo semanal da despensa") {
		t.Errorf("the second message was not the digest: %q", h.messenger.sent[1].text)
	}
	if len(h.digests.paid) != 1 {
		t.Errorf("the debt was not settled: %v", h.digests.paid)
	}
	if len(h.digests.owed) != 0 {
		t.Errorf("the debt is still standing after delivery")
	}
}

// The guard order is the security of this service, and the digest is
// now part of it: a number outside the list must not even cause a read.
func TestAStrangerNeverTouchesTheDebtBook(t *testing.T) {
	h := newHarness(t)
	h.digests.list = lowRice()
	if _, err := h.digests.Owe(context.Background(), stranger); err != nil {
		t.Fatal(err)
	}
	h.digests.reads = 0

	msg := text("usei uma taça de arroz")
	msg.From = stranger
	msg.ChatID = stranger
	h.bot.Handle(context.Background(), msg)

	if h.digests.reads != 0 {
		t.Errorf("a stranger's message read the debt book %d times", h.digests.reads)
	}
	if len(h.digests.paid) != 0 {
		t.Errorf("a stranger's message settled a debt: %v", h.digests.paid)
	}
}

// A redelivery already had its turn. Paying the debt again would send
// the same list twice for one message.
func TestARedeliveryDoesNotDeliverTheDigest(t *testing.T) {
	h := newHarness(t, answer("beleza"))
	h.digests.list = lowRice()
	ctx := context.Background()

	h.bot.Handle(ctx, text("usei uma taça de arroz"))
	if _, err := h.digests.Owe(ctx, mine); err != nil {
		t.Fatal(err)
	}
	h.digests.reads = 0

	h.bot.Handle(ctx, text("usei uma taça de arroz"))

	if h.digests.reads != 0 {
		t.Errorf("a redelivery read the debt book %d times", h.digests.reads)
	}
}

// A late shopping list must not break the turn it is riding on: the
// answer is already away.
func TestAFailingDigestDoesNotBreakTheTurn(t *testing.T) {
	h := newHarness(t, answer("beleza"))
	h.digests.list = lowRice()
	h.digests.owedErr = errors.New("the database is gone")

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	last := h.messenger.last(t)
	if last.text != "beleza" {
		t.Errorf("the answer was not sent: %q", last.text)
	}
}

// recordingMessenger keeps the template parameters, which is the one
// thing the shared fake throws away.
type recordingMessenger struct {
	fakeMessenger
	params []string
}

func (m *recordingMessenger) SendTemplate(ctx context.Context, to, name string, params []string) error {
	m.params = params
	return m.fakeMessenger.SendTemplate(ctx, to, name, params)
}
