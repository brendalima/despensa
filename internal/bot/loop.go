package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

var _ Handler = (*Bot)(nil)

// maxButtons is WhatsApp's limit on reply buttons, and also enough: a
// sentence that touches more than three items is rare, and the rest
// stay undoable by id.
const maxButtons = 3

// Bot is the loop. The order of its guards is the security of this
// whole service: the allowed list before the model, the idempotency
// claim before any write, the button before the model.
type Bot struct {
	tools     *agent.ToolSet
	model     Model
	messenger Messenger
	chat      Chat
	digests   Digests

	// allowed is the closed list of numbers. Anyone who can send a
	// message can write to the pantry, so this is not a preference.
	allowed map[string]bool

	// maxIterations caps the tool-use loop. A runaway loop is a bug,
	// not an expense: without the cap a confused model reads the pantry
	// twenty times and the bill is how anyone finds out.
	maxIterations int
}

type Input struct {
	Tools     *agent.ToolSet
	Model     Model
	Messenger Messenger
	Chat      Chat
	Digests   Digests

	AllowedNumbers []string
	MaxIterations  int
}

func New(in Input) (*Bot, error) {
	if in.Tools == nil {
		return nil, errors.New("missing tool set dependency")
	}
	if in.Model == nil {
		return nil, errors.New("missing model dependency")
	}
	if in.Messenger == nil {
		return nil, errors.New("missing messenger dependency")
	}
	if in.Chat == nil {
		return nil, errors.New("missing chat dependency")
	}
	if in.Digests == nil {
		return nil, errors.New("missing digests dependency")
	}
	if len(in.AllowedNumbers) == 0 {
		return nil, errors.New("missing allowed numbers")
	}
	if in.MaxIterations <= 0 {
		return nil, errors.New("missing tool iteration limit")
	}

	allowed := make(map[string]bool, len(in.AllowedNumbers))
	for _, number := range in.AllowedNumbers {
		allowed[normalizeNumber(number)] = true
	}

	return &Bot{
		tools:         in.Tools,
		model:         in.Model,
		messenger:     in.Messenger,
		chat:          in.Chat,
		digests:       in.Digests,
		allowed:       allowed,
		maxIterations: in.MaxIterations,
	}, nil
}

func MustNew(in Input) *Bot {
	b, err := New(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create bot: %w", err))
	}
	return b
}

// outcome is what one message did, collected only so that the happy
// path leaves a trace. Without it a working turn and a turn that died
// between the model and the phone look identical in the logs, which is
// the one thing a service nobody watches cannot afford.
type outcome struct {
	kind       string
	iterations int
	tools      []string
	buttons    int

	// digest records that a weekly list owed from an earlier run rode
	// out on this turn, which is otherwise invisible: it is a second
	// message nobody asked for.
	digest bool
}

// Handle takes one message to the end. It returns nothing because
// nobody is waiting: the webhook answered 200 before this started, and
// the only way to report a failure is to say so in the chat.
func (b *Bot) Handle(ctx context.Context, msg Message) {
	started := time.Now()

	var done outcome
	err := b.handle(ctx, msg, &done)
	if err != nil {
		slog.Error("couldn't handle message", "provider", msg.Provider, "message_id", msg.ID, "error", err)
		if sendErr := b.messenger.SendText(ctx, msg.ChatID, humanize.Error(err)); sendErr != nil {
			slog.Error("couldn't send the failure back", "error", sendErr)
		}
		return
	}

	// After the answer, never before it: the digest is a second
	// message, and a second message that arrives first reads as the
	// bot ignoring what was just said.
	done.digest = b.payDigest(ctx, msg, done.kind)

	slog.Info("message handled",
		"provider", msg.Provider,
		"message_id", msg.ID,
		"outcome", done.kind,
		"iterations", done.iterations,
		"tools", done.tools,
		"buttons", done.buttons,
		"digest", done.digest,
		"took", time.Since(started).String())
}

// payDigest settles a digest owed from an earlier week, if one is. It
// runs only for a turn that actually belonged to the household and
// actually did something: a stranger's message must cost nothing, and a
// redelivery already had its chance.
//
// It returns whether a digest went out, and never an error. The turn's
// own answer is already away — failing the message over a late shopping
// list would be the tail wagging the dog.
func (b *Bot) payDigest(ctx context.Context, msg Message, kind string) bool {
	if kind == "refused" || kind == "redelivery" {
		return false
	}
	delivered, err := b.deliverOwed(ctx, msg.ChatID)
	if err != nil {
		slog.Warn("couldn't deliver the owed digest", "chat_id", msg.ChatID, "error", err)
	}
	return delivered
}

func (b *Bot) handle(ctx context.Context, msg Message, done *outcome) error {
	// Before the model, before the database: an unknown number costs
	// nothing and learns nothing.
	if !b.allowed[normalizeNumber(msg.From)] {
		slog.Warn("message from a number outside the list", "from", msg.From)
		done.kind = "refused"
		return b.messenger.SendText(ctx, msg.ChatID, humanize.NotForYou())
	}

	mine, err := b.chat.Claim(ctx, msg.Provider, msg.ID)
	if err != nil {
		return err
	}
	if !mine {
		slog.Info("redelivery ignored", "provider", msg.Provider, "message_id", msg.ID)
		done.kind = "redelivery"
		return nil
	}

	// A button is an id, not a sentence. Sending it to the model would
	// pay for a round trip to learn what the payload already says.
	if msg.Choice != "" {
		done.kind = "button"
		return b.choose(ctx, msg)
	}

	if strings.TrimSpace(msg.Text) == "" {
		done.kind = "empty"
		return b.messenger.SendText(ctx, msg.ChatID, humanize.NothingToRead())
	}
	done.kind = "answered"
	return b.converse(ctx, msg, done)
}

// choose answers a tap. Only one button exists so far, and it undoes.
func (b *Bot) choose(ctx context.Context, msg Message) error {
	eventID, ok := undoTarget(msg.Choice)
	if !ok {
		slog.Warn("unknown button payload", "choice", msg.Choice)
		return b.messenger.SendText(ctx, msg.ChatID, humanize.NothingToRead())
	}

	args, err := json.Marshal(map[string]int64{"event_id": eventID})
	if err != nil {
		return fmt.Errorf("couldn't build the undo arguments: %w", err)
	}

	result, err := b.tools.Call(ctx, agent.ToolUndoEvent, args)
	if err != nil {
		return err
	}
	return b.messenger.SendText(ctx, msg.ChatID, agent.Text(result))
}

// converse is the tool-use loop: history, then the model, then the
// tools it asked for, until it answers in words or the cap is reached.
func (b *Bot) converse(ctx context.Context, msg Message, done *outcome) error {
	turns := b.history(ctx, msg.ChatID)

	asked := Turn{Role: domain.TurnUser, Text: msg.Text}
	turns = append(turns, asked)
	fresh := []Turn{asked}

	var (
		answer   string
		undoable []agent.Recorded
	)

	for i := 0; i < b.maxIterations; i++ {
		done.iterations = i + 1
		reply, err := b.model.Reply(ctx, Request{System: systemPrompt, Tools: b.tools.Tools(), Turns: turns})
		if err != nil {
			b.remember(ctx, msg.ChatID, fresh)
			return err
		}
		turns = append(turns, reply)
		fresh = append(fresh, reply)

		if !reply.wantsTools() {
			answer = strings.TrimSpace(reply.Text)
			break
		}

		results := Turn{Role: domain.TurnUser}
		for _, call := range reply.ToolCalls {
			done.tools = append(done.tools, call.Name)
			result, ids := b.run(ctx, call)
			results.ToolResults = append(results.ToolResults, result)
			undoable = append(undoable, ids...)
		}
		turns = append(turns, results)
		fresh = append(fresh, results)
	}

	b.remember(ctx, msg.ChatID, fresh)

	if answer == "" {
		// The writes that happened, happened — the ledger is the
		// truth either way, so say what is known instead of inventing
		// a receipt.
		slog.Warn("the model stopped without an answer",
			"message_id", msg.ID, "iterations", b.maxIterations)
		answer = humanize.GotLost()
		done.kind = "lost"
	}
	if len(undoable) > maxButtons {
		done.buttons = maxButtons
	} else {
		done.buttons = len(undoable)
	}
	return b.send(ctx, msg.ChatID, answer, undoable)
}

// run calls one tool. Every failure comes back as a result the model
// can read, carrying both the sentence a person would have heard and
// the English detail, so a wrong call is a correction and not the end
// of the turn.
func (b *Bot) run(ctx context.Context, call ToolCall) (ToolResult, []agent.Recorded) {
	result, err := b.tools.Call(ctx, call.Name, call.Args)
	if err != nil {
		slog.Warn("tool refused", "tool", call.Name, "error", err)
		return ToolResult{
			CallID:  call.ID,
			Content: humanize.Error(err) + "\n" + err.Error(),
			IsError: true,
		}, nil
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		// Unreachable short of a bug in a view, and this one is ours.
		slog.Error("couldn't encode a tool result", "tool", call.Name, "error", err)
		return ToolResult{CallID: call.ID, Content: err.Error(), IsError: true}, nil
	}
	return ToolResult{CallID: call.ID, Content: string(encoded)}, agent.RecordedEvents(result)
}

// send puts a Desfazer button on every event this turn wrote.
//
// If the buttons are refused, the receipt goes out as plain text
// instead. That fallback is not politeness: the writes have already
// happened, and returning the error here would make Handle answer with
// humanize.Trouble() — "não anotei nada" — about two rows that are
// sitting in the ledger. A person told that re-sends the sentence and
// logs everything twice, which is the worst outcome this code can
// produce. A receipt without a button is a far smaller loss: the events
// stay undoable by id through the model.
func (b *Bot) send(ctx context.Context, chatID, text string, undoable []agent.Recorded) error {
	if len(undoable) == 0 {
		return b.messenger.SendText(ctx, chatID, text)
	}

	if len(undoable) > maxButtons {
		undoable = undoable[:maxButtons]
	}
	buttons := make([]Button, 0, len(undoable))
	for _, recorded := range undoable {
		// One event needs no disambiguation; several do, and Meta
		// refuses a message whose button titles repeat.
		label := humanize.UndoLabel
		if len(undoable) > 1 {
			label = humanize.UndoLabelFor(recorded.Item)
		}
		buttons = append(buttons, Button{ID: UndoButtonID(recorded.EventID), Label: label})
	}

	if err := b.messenger.SendWithButtons(ctx, chatID, text, buttons); err != nil {
		slog.Warn("the receipt's buttons were refused, sending it as text",
			"chat_id", chatID, "buttons", len(buttons), "error", err)
		return b.messenger.SendText(ctx, chatID, text)
	}
	return nil
}

// history reads the last turns. A history that cannot be read is worth
// a log line and nothing more: the pantry is in the ledger, and
// answering without the last two minutes beats not answering.
func (b *Bot) history(ctx context.Context, chatID string) []Turn {
	stored, err := b.chat.History(ctx, chatID)
	if err != nil {
		slog.Warn("couldn't read the conversation history", "chat_id", chatID, "error", err)
		return nil
	}

	turns := make([]Turn, 0, len(stored))
	for _, row := range stored {
		var turn Turn
		if err := json.Unmarshal(row.Content, &turn); err != nil {
			slog.Warn("couldn't decode a stored turn", "turn_id", row.ID, "error", err)
			continue
		}
		turns = append(turns, turn)
	}
	return turns
}

func (b *Bot) remember(ctx context.Context, chatID string, turns []Turn) {
	for _, turn := range turns {
		content, err := json.Marshal(turn)
		if err != nil {
			slog.Warn("couldn't encode a turn", "error", err)
			continue
		}
		if err := b.chat.Remember(ctx, chatID, turn.Role, content); err != nil {
			slog.Warn("couldn't store a turn", "chat_id", chatID, "error", err)
		}
	}
}

// normalizeNumber keeps the digits. WhatsApp sends "5511999999999" and
// a person writes "+55 11 99999-9999" into the configuration; comparing
// them as typed would lock the household out of its own pantry.
func normalizeNumber(number string) string {
	var b strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
