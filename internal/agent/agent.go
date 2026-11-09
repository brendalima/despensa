// Package agent holds the tools the model calls, as plain Go values. It
// knows nothing about MCP or about any model provider: cmd/mcp hands
// these to an MCP server and the bot hands them to the Anthropic SDK,
// and both get the same seven tools with the same wording.
//
// Keeping the provider out is not tidiness. The tool descriptions are
// the product's behaviour, and the day the bot and Claude Desktop
// disagree about what "taça" means is the day the inventory is wrong.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Handler takes the arguments as they arrived and returns whatever the
// driver should marshal back. Raw JSON rather than a typed parameter
// because the tools are called from two places with two different SDKs;
// the schema has already said no to anything malformed.
type Handler func(ctx context.Context, args json.RawMessage) (any, error)

// Tool is one thing the model can do.
type Tool struct {
	Name        string
	Description string

	// Schema is the input schema, as the exact JSON the model is shown.
	Schema map[string]any

	Handler Handler
}

// ToolSet is the seven tools, with their schemas resolved once so every
// call is checked before the handler runs.
type ToolSet struct {
	tools    []Tool
	resolved map[string]*jsonschema.Resolved
}

// New builds the tool set over a pantry. The source is written on every
// event the tools record, so the ledger says whether a line came from
// Claude Desktop or from WhatsApp; it is a parameter because both
// drivers share these tools.
func New(pantry Pantry, source string) (*ToolSet, error) {
	if pantry == nil {
		return nil, errors.New("missing pantry dependency")
	}
	if source == "" {
		return nil, errors.New("missing event source")
	}

	t := &tools{pantry: pantry, source: source}
	set := &ToolSet{
		tools: []Tool{
			{
				Name:        ToolListInventory,
				Description: descListInventory,
				Schema:      noArgs(),
				Handler:     t.listInventory,
			},
			{
				Name:        ToolRecordEvents,
				Description: descRecordEvents,
				Schema:      recordEventsSchema(),
				Handler:     t.recordEvents,
			},
			{
				Name:        ToolUndoEvent,
				Description: descUndoEvent,
				Schema:      undoEventSchema(),
				Handler:     t.undoEvent,
			},
			{
				Name:        ToolUpdateItem,
				Description: descUpdateItem,
				Schema:      updateItemSchema(),
				Handler:     t.updateItem,
			},
			{
				Name:        ToolShoppingList,
				Description: descShoppingList,
				Schema:      noArgs(),
				Handler:     t.shoppingList,
			},
			{
				Name:        ToolAddShoppingNote,
				Description: descAddShoppingNote,
				Schema:      addShoppingNoteSchema(),
				Handler:     t.addShoppingNote,
			},
			{
				Name:        ToolRemoveShoppingNote,
				Description: descRemoveShoppingNote,
				Schema:      removeShoppingNoteSchema(),
				Handler:     t.removeShoppingNote,
			},
		},
		resolved: map[string]*jsonschema.Resolved{},
	}

	for _, tool := range set.tools {
		resolved, err := resolve(tool.Schema)
		if err != nil {
			return nil, fmt.Errorf("couldn't resolve the schema of %s: %w", tool.Name, err)
		}
		set.resolved[tool.Name] = resolved
	}
	return set, nil
}

func MustNew(pantry Pantry, source string) *ToolSet {
	s, err := New(pantry, source)
	if err != nil {
		panic(fmt.Errorf("couldn't create the tool set: %w", err))
	}
	return s
}

// Tools returns the tools in a stable order, so a golden test and a
// prompt both see the same list every time.
func (s *ToolSet) Tools() []Tool { return s.tools }

// Call validates the arguments against the tool's schema and runs it. A
// refusal here is the cheap one: it costs no transaction and the model
// sees what it got wrong.
func (s *ToolSet) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	for _, tool := range s.tools {
		if tool.Name != name {
			continue
		}
		if err := s.validate(name, args); err != nil {
			return nil, err
		}
		return tool.Handler(ctx, args)
	}
	return nil, domain.NewNotFoundError("unknown tool " + name)
}

func (s *ToolSet) validate(name string, args json.RawMessage) error {
	// No arguments at all is the same as an empty object: the clients
	// that send nothing for a tool that takes nothing are not wrong.
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	var instance any
	if err := json.Unmarshal(args, &instance); err != nil {
		return domain.NewValidationError("arguments", "are not valid JSON")
	}
	if err := s.resolved[name].Validate(instance); err != nil {
		return domain.NewValidationError("arguments", err.Error())
	}
	return nil
}

// resolve turns the wire schema into something that can validate. The
// round trip through JSON is deliberate: the map is the single source,
// so what validates and what the model was shown cannot drift apart.
func resolve(schema map[string]any) (*jsonschema.Resolved, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Resolve(nil)
}

// tools holds what the handlers share.
type tools struct {
	pantry Pantry
	source string
}

// decode reads the arguments into a typed input. The schema has already
// checked the shape, so a failure here is a bug, not bad input.
func decode(args json.RawMessage, into any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, into); err != nil {
		return fmt.Errorf("couldn't decode the arguments: %w", err)
	}
	return nil
}
