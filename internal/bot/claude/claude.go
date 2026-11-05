// Package claude is the model adapter: it maps a bot.Request onto the
// Anthropic SDK and the answer back onto a bot.Turn. Nothing else in
// the project imports the SDK, which is what lets the loop's rules be
// tested without a key.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
)

var _ bot.Model = (*Client)(nil)

const (
	// maxTokens is the shape of the answer, not a budget: a receipt is
	// one or two sentences, and anything longer is the model losing the
	// thread rather than being thorough.
	maxTokens = 1024

	// Low effort, deliberately. The judgement that matters here is
	// "how much is a cup of rice", which is recall, not reasoning; the
	// rest is reading a short list and calling one tool.
	effort = anthropic.OutputConfigEffortLow
)

type Client struct {
	api   anthropic.Client
	model string
}

type Input struct {
	APIKey string

	// Model is a configuration value because the right one is an open
	// question: what matters is the quality of portion estimates, and
	// the only way to know is to run on real sentences.
	Model string
}

func New(in Input) (*Client, error) {
	if in.APIKey == "" {
		return nil, errors.New("missing anthropic api key")
	}
	if in.Model == "" {
		return nil, errors.New("missing anthropic model")
	}
	return &Client{
		api:   anthropic.NewClient(option.WithAPIKey(in.APIKey)),
		model: in.Model,
	}, nil
}

func MustNew(in Input) *Client {
	c, err := New(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create the model client: %w", err))
	}
	return c
}

// Reply asks for one turn. Thinking is left unset: the current models
// think by default and the depth is set through effort.
func (c *Client) Reply(ctx context.Context, req bot.Request) (bot.Turn, error) {
	messages, err := messagesOf(req.Turns)
	if err != nil {
		return bot.Turn{}, err
	}

	answer, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:        c.model,
		MaxTokens:    maxTokens,
		System:       []anthropic.TextBlockParam{{Text: req.System}},
		Tools:        toolsOf(req.Tools),
		Messages:     messages,
		OutputConfig: anthropic.OutputConfigParam{Effort: effort},
	})
	if err != nil {
		return bot.Turn{}, fmt.Errorf("couldn't reach the model: %w", err)
	}

	// A declined request stops here rather than being retried under a
	// different wording: a pantry has nothing to argue about, and
	// pretending otherwise would hide a real problem.
	if answer.StopReason == anthropic.StopReasonRefusal {
		return bot.Turn{}, fmt.Errorf("the model declined to answer: %s", answer.StopDetails.Category)
	}

	return turnOf(answer)
}

// turnOf keeps both halves: the fields the loop reads, and the exact
// message to replay. Thinking blocks are in the raw copy and nowhere
// else, which is the only way they survive the next request unedited.
func turnOf(answer *anthropic.Message) (bot.Turn, error) {
	turn := bot.Turn{Role: domain.TurnAssistant}

	for _, block := range answer.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.TextBlock:
			turn.Text += variant.Text
		case anthropic.ToolUseBlock:
			turn.ToolCalls = append(turn.ToolCalls, bot.ToolCall{
				ID:   variant.ID,
				Name: variant.Name,
				Args: json.RawMessage(variant.JSON.Input.Raw()),
			})
		}
	}

	raw, err := json.Marshal(answer.ToParam())
	if err != nil {
		return bot.Turn{}, fmt.Errorf("couldn't keep the model's turn: %w", err)
	}
	turn.Raw = raw
	return turn, nil
}

// messagesOf rebuilds the conversation. An assistant turn is replayed
// from its raw copy whenever there is one; a user turn is built here,
// because its content is ours to begin with.
func messagesOf(turns []bot.Turn) ([]anthropic.MessageParam, error) {
	messages := make([]anthropic.MessageParam, 0, len(turns))

	for _, turn := range turns {
		if len(turn.Raw) > 0 {
			var message anthropic.MessageParam
			if err := json.Unmarshal(turn.Raw, &message); err != nil {
				return nil, fmt.Errorf("couldn't replay a stored turn: %w", err)
			}
			messages = append(messages, message)
			continue
		}

		blocks := blocksOf(turn)
		if len(blocks) == 0 {
			// An empty message is refused by the API, and an empty
			// turn says nothing anyway.
			continue
		}
		if turn.Role == domain.TurnAssistant {
			messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			continue
		}
		messages = append(messages, anthropic.NewUserMessage(blocks...))
	}
	return messages, nil
}

func blocksOf(turn bot.Turn) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion

	// Tool results come first in the turn that answers a tool call, and
	// all of them go in one message: splitting them teaches the model
	// to stop asking for more than one tool at a time.
	for _, result := range turn.ToolResults {
		blocks = append(blocks, anthropic.NewToolResultBlock(result.CallID, result.Content, result.IsError))
	}
	if turn.Text != "" {
		blocks = append(blocks, anthropic.NewTextBlock(turn.Text))
	}
	for _, call := range turn.ToolCalls {
		blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, call.Args, call.Name))
	}
	return blocks
}

// toolsOf hands over the schemas as they were written. The map is the
// single source of what the model is shown and what validates the call,
// so additionalProperties travels in ExtraFields rather than being
// restated here.
func toolsOf(tools []agent.Tool) []anthropic.ToolUnionParam {
	params := make([]anthropic.ToolUnionParam, 0, len(tools))

	for _, tool := range tools {
		schema := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
		for key, value := range tool.Schema {
			switch key {
			case "type":
				// Always "object", and the field's zero value says so.
			case "properties":
				schema.Properties = value
			case "required":
				schema.Required = requiredOf(value)
			default:
				schema.ExtraFields[key] = value
			}
		}

		params = append(params, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        tool.Name,
			Description: anthropic.String(tool.Description),
			InputSchema: schema,
		}})
	}
	return params
}

func requiredOf(value any) []string {
	switch required := value.(type) {
	case []string:
		return required
	case []any:
		names := make([]string, 0, len(required))
		for _, name := range required {
			if s, ok := name.(string); ok {
				names = append(names, s)
			}
		}
		return names
	default:
		return nil
	}
}
