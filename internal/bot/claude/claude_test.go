package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
)

// server stands in for the API: it keeps the request body and answers
// with a canned message, so the whole mapping is exercised without a
// key and without spending anything.
type server struct {
	http    *httptest.Server
	sent    map[string]any
	headers http.Header
	reply   string
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{reply: `{
	  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	  "stop_reason": "end_turn",
	  "content": [{"type": "text", "text": "arroz: 5 kg → 4,8 kg"}],
	  "usage": {"input_tokens": 10, "output_tokens": 5}
	}`}

	s.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("couldn't read the request: %v", err)
		}
		if err := json.Unmarshal(raw, &s.sent); err != nil {
			t.Errorf("the client sent something that is not JSON: %v", err)
		}
		s.headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, s.reply)
	}))
	t.Cleanup(s.http.Close)
	return s
}

func newClient(t *testing.T, s *server) *Client {
	t.Helper()
	c, err := New(Input{APIKey: "sk-test", Model: "claude-opus-5-5", BaseURL: s.http.URL})
	if err != nil {
		t.Fatalf("couldn't create the client: %v", err)
	}
	return c
}

func tools(t *testing.T) []agent.Tool {
	t.Helper()
	return agent.MustNew(stubPantry{}, domain.SourceWhatsApp).Tools()
}

type stubPantry struct{}

func (stubPantry) List(context.Context) ([]domain.Item, error) { return nil, nil }
func (stubPantry) Record(context.Context, []domain.EventInput) ([]domain.Receipt, error) {
	return nil, nil
}
func (stubPantry) Undo(context.Context, int64) (domain.Receipt, error) {
	return domain.Receipt{}, nil
}
func (stubPantry) Update(context.Context, domain.ItemChange) (domain.Item, error) {
	return domain.Item{}, nil
}
func (stubPantry) ShoppingList(context.Context) (domain.ShoppingList, error) {
	return domain.ShoppingList{}, nil
}
func (stubPantry) AddNote(context.Context, string) (domain.Note, error) {
	return domain.Note{}, nil
}
func (stubPantry) RemoveNote(context.Context, int64) (domain.Note, error) {
	return domain.Note{}, nil
}

func TestReplyAsksTheWayThePlanSays(t *testing.T) {
	s := newServer(t)
	c := newClient(t, s)

	turn, err := c.Reply(context.Background(), bot.Request{
		System: "você é a despensa",
		Tools:  tools(t),
		Turns:  []bot.Turn{{Role: domain.TurnUser, Text: "usei uma taça de arroz"}},
	})
	if err != nil {
		t.Fatalf("couldn't ask the model: %v", err)
	}

	if got := s.sent["model"]; got != "claude-opus-5-5" {
		t.Errorf("model %v", got)
	}
	if got := s.sent["max_tokens"]; got != float64(maxTokens) {
		t.Errorf("max_tokens %v, want %d", got, maxTokens)
	}

	output, ok := s.sent["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("no output_config in %v", s.sent)
	}
	if output["effort"] != "low" {
		t.Errorf("effort %v, want low", output["effort"])
	}

	// Thinking is configured for exactly one reason: block_binding.
	// A replayed thinking block whose prefix no longer matches is a
	// 400 that kills the whole turn, and the bot replays a window of
	// stored turns, which is never the whole conversation. Without
	// drop_block the second message of a long chat fails and the
	// person is told the pantry could not be saved.
	thinking, ok := s.sent["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("no thinking config in %v", s.sent)
	}
	if thinking["type"] != "adaptive" {
		t.Errorf("thinking type %v, want adaptive — block_binding is refused with any other", thinking["type"])
	}
	binding, ok := thinking["block_binding"].(map[string]any)
	if !ok {
		t.Fatalf("no block_binding in %v", thinking)
	}
	if binding["prefix_mismatch_behavior"] != "drop_block" {
		t.Errorf("prefix_mismatch_behavior %v, want drop_block", binding["prefix_mismatch_behavior"])
	}

	if sent, ok := s.sent["tools"].([]any); !ok || len(sent) != 7 {
		t.Errorf("sent %v tools, want 7", s.sent["tools"])
	}

	if turn.Role != domain.TurnAssistant {
		t.Errorf("the answer came back as %q", turn.Role)
	}
	if turn.Text != "arroz: 5 kg → 4,8 kg" {
		t.Errorf("text %q", turn.Text)
	}
	if len(turn.Raw) == 0 {
		t.Error("the answer was not kept for replay")
	}
}

// The schemas are the agent package's, down to the closed objects: a
// field restated here would be a second place that decides what the
// model may send.
func TestSchemasCrossOverClosed(t *testing.T) {
	for _, tool := range tools(t) {
		params := toolsOf([]agent.Tool{tool})
		if len(params) != 1 {
			t.Fatalf("%s produced %d tool params", tool.Name, len(params))
		}

		raw, err := json.Marshal(params[0])
		if err != nil {
			t.Fatalf("couldn't encode the tool %s: %v", tool.Name, err)
		}
		var sent struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Type                 string         `json:"type"`
				Properties           map[string]any `json:"properties"`
				Required             []string       `json:"required"`
				AdditionalProperties *bool          `json:"additionalProperties"`
			} `json:"input_schema"`
		}
		if err := json.Unmarshal(raw, &sent); err != nil {
			t.Fatalf("couldn't read back the tool %s: %v", tool.Name, err)
		}

		if sent.Name != tool.Name {
			t.Errorf("tool name %q, want %q", sent.Name, tool.Name)
		}
		if sent.Description != tool.Description {
			t.Errorf("the description of %s changed on the way to the model", tool.Name)
		}
		if sent.InputSchema.Type != "object" {
			t.Errorf("%s has schema type %q", tool.Name, sent.InputSchema.Type)
		}
		if sent.InputSchema.AdditionalProperties == nil || *sent.InputSchema.AdditionalProperties {
			t.Errorf("%s accepts unknown fields, so a misspelling would pass silently", tool.Name)
		}

		want, _ := tool.Schema["required"].([]string)
		if len(want) != len(sent.InputSchema.Required) {
			t.Errorf("%s requires %v, want %v", tool.Name, sent.InputSchema.Required, want)
		}
		wantProps, _ := tool.Schema["properties"].(map[string]any)
		if len(wantProps) != len(sent.InputSchema.Properties) {
			t.Errorf("%s has %d properties, want %d",
				tool.Name, len(sent.InputSchema.Properties), len(wantProps))
		}
	}
}

// A stored assistant turn is replayed byte for byte. Rebuilding one
// would be editing the history, and an edited history invalidates the
// reasoning blocks that came with it.
func TestStoredTurnsAreReplayedUnchanged(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"qual arroz?"}]}`)

	messages, err := messagesOf([]bot.Turn{
		{Role: domain.TurnUser, Text: "usei uma taça de arroz"},
		{Role: domain.TurnAssistant, Text: "qual arroz?", Raw: raw},
		{Role: domain.TurnUser, ToolResults: []bot.ToolResult{
			{CallID: "call-1", Content: `{"items":[]}`},
			{CallID: "call-2", Content: "não achei", IsError: true},
		}},
		{Role: domain.TurnAssistant},
	})
	if err != nil {
		t.Fatalf("couldn't rebuild the conversation: %v", err)
	}

	// The empty assistant turn is left out: an empty message is refused
	// by the API and says nothing anyway.
	if len(messages) != 3 {
		t.Fatalf("rebuilt %d messages, want 3", len(messages))
	}

	replayed, err := json.Marshal(messages[1])
	if err != nil {
		t.Fatalf("couldn't encode the replayed turn: %v", err)
	}
	var want, got map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replayed, &got); err != nil {
		t.Fatal(err)
	}
	if len(got["content"].([]any)) != len(want["content"].([]any)) {
		t.Errorf("the replayed turn is %s", replayed)
	}

	// Both results travel in one message: splitting them teaches the
	// model to stop asking for more than one tool at a time.
	results, err := json.Marshal(messages[2])
	if err != nil {
		t.Fatal(err)
	}
	var user struct {
		Content []struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
		} `json:"content"`
	}
	if err := json.Unmarshal(results, &user); err != nil {
		t.Fatal(err)
	}
	if len(user.Content) != 2 {
		t.Fatalf("the tool results were sent as %s", results)
	}
	if user.Content[0].Type != "tool_result" || !user.Content[1].IsError {
		t.Errorf("the tool results were sent as %s", results)
	}
}

// A tool call the model asked for has to come back as a call the loop
// can run, with the arguments untouched.
func TestToolCallsAreReadBack(t *testing.T) {
	s := newServer(t)
	s.reply = `{
	  "id": "msg_2", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	  "stop_reason": "tool_use",
	  "content": [
	    {"type": "text", "text": "deixa eu ver"},
	    {"type": "tool_use", "id": "toolu_1", "name": "record_events",
	     "input": {"entries": [{"kind": "consume", "portion_name": "taça", "portion_qty": 1}]}}
	  ],
	  "usage": {"input_tokens": 10, "output_tokens": 5}
	}`
	c := newClient(t, s)

	turn, err := c.Reply(context.Background(), bot.Request{System: "p", Tools: tools(t)})
	if err != nil {
		t.Fatalf("couldn't ask the model: %v", err)
	}

	if len(turn.ToolCalls) != 1 {
		t.Fatalf("read %d tool calls", len(turn.ToolCalls))
	}
	call := turn.ToolCalls[0]
	if call.ID != "toolu_1" || call.Name != "record_events" {
		t.Errorf("tool call read as %+v", call)
	}

	var args struct {
		Entries []struct {
			Kind        string  `json:"kind"`
			PortionName string  `json:"portion_name"`
			PortionQty  float64 `json:"portion_qty"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(call.Args, &args); err != nil {
		t.Fatalf("the arguments did not survive: %v", err)
	}
	if len(args.Entries) != 1 || args.Entries[0].PortionName != "taça" {
		t.Errorf("arguments read as %+v", args)
	}
}

// A declined request stops here rather than being retried under a
// different wording: a pantry has nothing to argue about.
func TestARefusalIsAnError(t *testing.T) {
	s := newServer(t)
	s.reply = `{
	  "id": "msg_3", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	  "stop_reason": "refusal",
	  "stop_details": {"type": "refusal", "category": "cyber"},
	  "content": [], "usage": {"input_tokens": 1, "output_tokens": 1}
	}`
	c := newClient(t, s)

	if _, err := c.Reply(context.Background(), bot.Request{System: "p"}); err == nil {
		t.Error("a refusal came back as an answer")
	}
}

func TestClientNeedsItsConfiguration(t *testing.T) {
	if _, err := New(Input{Model: "claude-opus-5-5"}); err == nil {
		t.Error("New accepted an empty api key")
	}
	if _, err := New(Input{APIKey: "sk-test"}); err == nil {
		t.Error("New accepted an empty model")
	}
}

// The block_binding field is refused without its beta, so the header is
// as load-bearing as the body — and a missing header fails the same way
// the bug it fixes did: on the second message of a conversation, in
// production, never in a test that only reads the body.
func TestTheBindingBetaIsSent(t *testing.T) {
	s := newServer(t)
	c := newClient(t, s)

	if _, err := c.Reply(context.Background(), bot.Request{
		System: "oi",
		Tools:  tools(t),
		Turns:  []bot.Turn{{Role: domain.TurnUser, Text: "usei uma taça de arroz"}},
	}); err != nil {
		t.Fatalf("the reply failed: %v", err)
	}

	const want = "thinking-binding-controls-2026-08-01"
	if got := s.headers.Get("Anthropic-Beta"); !strings.Contains(got, want) {
		t.Errorf("anthropic-beta = %q, want it to carry %q", got, want)
	}
}

// max_tokens bounds thinking, the tool-call JSON and the text
// together. A sentence naming a whole pantry produces an entries array
// with a new_item block per line, so a ceiling sized for "a receipt is
// one or two sentences" truncates the call instead of the answer — and
// the refusal that follows reads like a confused model.
func TestMaxTokensFitsABatchToolCall(t *testing.T) {
	const tenItemBatch = 4096
	if maxTokens < tenItemBatch {
		t.Errorf("maxTokens is %d, too small for a sentence naming ten items", maxTokens)
	}
}

// A turn cut off at the ceiling has to be distinguishable from a whole
// one, or the loop runs half a tool call and retries until its budget
// is gone.
func TestATurnCutOffAtTheCeilingIsReportedAsTruncated(t *testing.T) {
	s := newServer(t)
	s.reply = `{
	  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	  "stop_reason": "max_tokens",
	  "content": [{"type": "tool_use", "id": "call-1", "name": "record_events", "input": {}}],
	  "usage": {"input_tokens": 10, "output_tokens": 8192}
	}`
	c := newClient(t, s)

	turn, err := c.Reply(context.Background(), bot.Request{
		System: "oi", Tools: tools(t),
		Turns: []bot.Turn{{Role: domain.TurnUser, Text: "anota tudo que eu tenho"}},
	})
	if err != nil {
		t.Fatalf("a truncated turn should come back, not error: %v", err)
	}
	if !turn.Truncated {
		t.Error("stop_reason max_tokens did not mark the turn truncated")
	}
}

// The ordinary case must not be marked truncated, or every turn ends
// the loop.
func TestAWholeTurnIsNotTruncated(t *testing.T) {
	s := newServer(t)
	c := newClient(t, s)

	turn, err := c.Reply(context.Background(), bot.Request{
		System: "oi", Tools: tools(t),
		Turns: []bot.Turn{{Role: domain.TurnUser, Text: "usei uma taça de arroz"}},
	})
	if err != nil {
		t.Fatalf("the reply failed: %v", err)
	}
	if turn.Truncated {
		t.Error("a turn that ended normally was marked truncated")
	}
}
