package bot_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
)

const (
	riceID = "11111111-1111-1111-1111-111111111111"

	mine      = "5511999990000"
	stranger  = "5511888880000"
	messageID = "wamid.1"
)

// The real services satisfy the ports without being told to, which is
// the reason the ports live in the bot package.
var (
	_ bot.Chat     = (*services.Chat)(nil)
	_ agent.Pantry = (*services.Pantry)(nil)
)

func rice() domain.Item {
	return domain.Item{
		ID:        riceID,
		Name:      "arroz",
		Unit:      domain.UnitGram,
		OnHand:    5000 * domain.Thousand,
		Threshold: 1000 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "taça", One: 180 * domain.Thousand, Source: domain.PortionSeeded},
		},
	}
}

// fakePantry is the service the tools call. The tools themselves are
// real: a test that stubbed them would not exercise the validation the
// bot relies on.
type fakePantry struct {
	recorded []domain.EventInput
	undone   []int64

	undoErr error
}

func (p *fakePantry) List(context.Context) ([]domain.Item, error) {
	return []domain.Item{rice()}, nil
}

func (p *fakePantry) Record(_ context.Context, in []domain.EventInput) ([]domain.Receipt, error) {
	p.recorded = append(p.recorded, in...)

	receipts := make([]domain.Receipt, 0, len(in))
	for i := range in {
		receipts = append(receipts, domain.Receipt{
			Item:    rice(),
			EventID: int64(100 + i),
			Before:  5000 * domain.Thousand,
			After:   4820 * domain.Thousand,
		})
	}
	return receipts, nil
}

func (p *fakePantry) Undo(_ context.Context, eventID int64) (domain.Receipt, error) {
	if p.undoErr != nil {
		return domain.Receipt{}, p.undoErr
	}
	p.undone = append(p.undone, eventID)
	return domain.Receipt{
		Item:    rice(),
		EventID: eventID,
		Before:  4820 * domain.Thousand,
		After:   5000 * domain.Thousand,
	}, nil
}

func (p *fakePantry) Update(context.Context, domain.ItemChange) (domain.Item, error) {
	return rice(), nil
}

func (p *fakePantry) ShoppingList(context.Context) (domain.ShoppingList, error) {
	return domain.ShoppingList{}, nil
}

func (p *fakePantry) AddNote(_ context.Context, text string) (domain.Note, error) {
	return domain.Note{ID: 1, Text: text}, nil
}

// fakeModel answers from a script and counts the calls, which is how
// "zero model calls" is asserted rather than hoped for.
type fakeModel struct {
	replies []bot.Turn
	err     error

	requests []bot.Request
}

func (m *fakeModel) Reply(_ context.Context, req bot.Request) (bot.Turn, error) {
	m.requests = append(m.requests, req)
	if m.err != nil {
		return bot.Turn{}, m.err
	}
	if len(m.replies) == 0 {
		return bot.Turn{Role: domain.TurnAssistant, Text: "pronto"}, nil
	}
	// The last reply repeats, so a script of one tool call is a model
	// that never stops asking.
	i := min(len(m.requests), len(m.replies)) - 1
	return m.replies[i], nil
}

func (m *fakeModel) calls() int { return len(m.requests) }

type sent struct {
	to      string
	text    string
	buttons []bot.Button
}

type fakeMessenger struct {
	sent []sent
	err  error
}

func (m *fakeMessenger) SendText(_ context.Context, to, text string) error {
	m.sent = append(m.sent, sent{to: to, text: text})
	return m.err
}

func (m *fakeMessenger) SendWithButtons(_ context.Context, to, text string, buttons []bot.Button) error {
	m.sent = append(m.sent, sent{to: to, text: text, buttons: buttons})
	return m.err
}

func (m *fakeMessenger) SendTemplate(_ context.Context, to, name string, _ []string) error {
	m.sent = append(m.sent, sent{to: to, text: name})
	return m.err
}

func (m *fakeMessenger) last(t *testing.T) sent {
	t.Helper()
	if len(m.sent) == 0 {
		t.Fatal("nothing was sent")
	}
	return m.sent[len(m.sent)-1]
}

type fakeChat struct {
	claimed map[string]bool
	history []domain.Turn

	remembered []domain.Turn
	claims     int
}

func newFakeChat() *fakeChat { return &fakeChat{claimed: map[string]bool{}} }

func (c *fakeChat) Claim(_ context.Context, provider, messageID string) (bool, error) {
	c.claims++
	key := provider + ":" + messageID
	if c.claimed[key] {
		return false, nil
	}
	c.claimed[key] = true
	return true, nil
}

func (c *fakeChat) History(context.Context, string) ([]domain.Turn, error) {
	return c.history, nil
}

func (c *fakeChat) Remember(_ context.Context, chatID string, role domain.TurnRole, content []byte) error {
	c.remembered = append(c.remembered, domain.Turn{ChatID: chatID, Role: role, Content: content})
	return nil
}

// harness wires the real tools and the fakes together.
type harness struct {
	bot       *bot.Bot
	pantry    *fakePantry
	model     *fakeModel
	messenger *fakeMessenger
	chat      *fakeChat
}

func newHarness(t *testing.T, replies ...bot.Turn) *harness {
	t.Helper()

	h := &harness{
		pantry:    &fakePantry{},
		model:     &fakeModel{replies: replies},
		messenger: &fakeMessenger{},
		chat:      newFakeChat(),
	}

	b, err := bot.New(bot.Input{
		Tools:          agent.MustNew(h.pantry, domain.SourceWhatsApp),
		Model:          h.model,
		Messenger:      h.messenger,
		Chat:           h.chat,
		AllowedNumbers: []string{"+55 11 99999-0000"},
		MaxIterations:  3,
	})
	if err != nil {
		t.Fatalf("couldn't create the bot: %v", err)
	}
	h.bot = b
	return h
}

func text(body string) bot.Message {
	return bot.Message{
		Provider: domain.SourceWhatsApp,
		ID:       messageID,
		ChatID:   mine,
		From:     mine,
		Text:     body,
	}
}

func toolCall(name, args string) bot.Turn {
	return bot.Turn{
		Role:      domain.TurnAssistant,
		ToolCalls: []bot.ToolCall{{ID: "call-1", Name: name, Args: json.RawMessage(args)}},
	}
}

func answer(body string) bot.Turn {
	return bot.Turn{Role: domain.TurnAssistant, Text: body}
}
