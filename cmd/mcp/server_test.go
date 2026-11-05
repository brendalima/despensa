package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/core/domain"
)

const riceID = "11111111-1111-1111-1111-111111111111"

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

// stubPantry is the smallest thing that satisfies the port: this file
// tests the protocol, not the pantry.
type stubPantry struct {
	recorded []domain.EventInput
	err      error
}

func (p *stubPantry) List(context.Context) ([]domain.Item, error) {
	return []domain.Item{rice()}, p.err
}

func (p *stubPantry) Record(_ context.Context, in []domain.EventInput) ([]domain.Receipt, error) {
	p.recorded = append(p.recorded, in...)
	if p.err != nil {
		return nil, p.err
	}
	item := rice()
	return []domain.Receipt{{
		Item:    item,
		EventID: 11,
		Before:  item.OnHand,
		After:   item.OnHand - 180*domain.Thousand,
	}}, nil
}

func (p *stubPantry) Undo(_ context.Context, eventID int64) (domain.Receipt, error) {
	item := rice()
	return domain.Receipt{Item: item, EventID: eventID, Before: item.OnHand, After: item.OnHand}, p.err
}

func (p *stubPantry) Update(context.Context, domain.ItemChange) (domain.Item, error) {
	return rice(), p.err
}

func (p *stubPantry) ShoppingList(context.Context) (domain.ShoppingList, error) {
	return domain.ShoppingList{Low: []domain.Item{rice()}}, p.err
}

func (p *stubPantry) AddNote(_ context.Context, text string) (domain.Note, error) {
	return domain.Note{ID: 1, Text: text}, p.err
}

// connect runs the real server over an in-memory transport pair, so what
// the tests below exercise is the wiring the host will use, not a copy
// of it.
func connect(t *testing.T, pantry agent.Pantry) *mcp.ClientSession {
	t.Helper()

	server := newServer(agent.MustNew(pantry, domain.SourceMCP))
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("couldn't connect the server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("couldn't connect the client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// What the host lists is what the agent package defines, descriptions
// and schemas included.
func TestServerListsTheSixToolsWithTheirSchemas(t *testing.T) {
	session := connect(t, &stubPantry{})

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}
	if len(result.Tools) != 6 {
		t.Fatalf("the host sees %d tools, want 6", len(result.Tools))
	}

	for _, tool := range result.Tools {
		if tool.Description == "" {
			t.Errorf("%s arrives with no description", tool.Name)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s has no object schema: %#v", tool.Name, tool.InputSchema)
		}
		if schema["type"] != "object" {
			t.Errorf("%s has schema type %v, want object", tool.Name, schema["type"])
		}
		// The one that would pass silently if it were dropped in
		// translation.
		if schema["additionalProperties"] != false {
			t.Errorf("%s reached the host without additionalProperties: false", tool.Name)
		}
	}
}

func TestCallToolReturnsTheStructuredResult(t *testing.T) {
	session := connect(t, &stubPantry{})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_inventory",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("tools/call failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("the call came back as an error: %v", content(result))
	}

	var decoded struct {
		Items []struct {
			ID     string  `json:"id"`
			Name   string  `json:"name"`
			OnHand float64 `json:"on_hand"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(content(result)), &decoded); err != nil {
		t.Fatalf("the content is not the JSON result: %v", err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].Name != "arroz" || decoded.Items[0].OnHand != 5000 {
		t.Errorf("got %+v", decoded.Items)
	}
}

func TestRecordEventsRoundTripsThroughTheProtocol(t *testing.T) {
	pantry := &stubPantry{}
	session := connect(t, pantry)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_events",
		Arguments: map[string]any{"entries": []any{map[string]any{
			"item_id":      riceID,
			"kind":         "consume",
			"portion_name": "taça",
			"portion_qty":  1,
		}}},
	})
	if err != nil {
		t.Fatalf("tools/call failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("the call came back as an error: %v", content(result))
	}

	if len(pantry.recorded) != 1 {
		t.Fatalf("the service got %d entries, want 1", len(pantry.recorded))
	}
	if pantry.recorded[0].Source != domain.SourceMCP {
		t.Errorf("source is %q, want %q", pantry.recorded[0].Source, domain.SourceMCP)
	}
	if !strings.Contains(content(result), "event_id") {
		t.Errorf("the receipt has no event id to undo by: %s", content(result))
	}
}

// A refused argument comes back as a tool result with is_error, not as a
// protocol error: otherwise the model cannot see what it got wrong and
// correct itself.
func TestRefusedArgumentsComeBackAsAToolError(t *testing.T) {
	pantry := &stubPantry{}
	session := connect(t, pantry)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_events",
		Arguments: map[string]any{"entries": []any{map[string]any{
			"item_id":      riceID,
			"kind":         "eaten",
			"portion_name": "taça",
			"portion_qty":  1,
		}}},
	})
	if err != nil {
		t.Fatalf("a refusal arrived as a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("rubbish was accepted")
	}
	if len(pantry.recorded) != 0 {
		t.Error("the pantry was called with input the schema should have refused")
	}
	if !strings.Contains(content(result), "enum") {
		t.Errorf("the model is not told what was wrong: %s", content(result))
	}
}

// A failing pantry is the same shape of answer, and the first line is
// the Portuguese the person would have heard.
func TestAFailingPantryComesBackAsAToolError(t *testing.T) {
	session := connect(t, &stubPantry{err: domain.NewNotFoundError("item not found")})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "shopping_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("a pantry failure arrived as a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a failure was reported as a success")
	}
	if first := result.Content[0].(*mcp.TextContent).Text; !strings.Contains(first, "Não encontrei") {
		t.Errorf("the first line is %q, want the Portuguese the person reads", first)
	}
}

func TestCallingAToolThatDoesNotExistFails(t *testing.T) {
	session := connect(t, &stubPantry{})

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "drop_database",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Error("the server answered a tool it does not have")
	}
}

func content(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range result.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
