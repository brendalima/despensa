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
	effort = anthropic.BetaOutputConfigEffortLow
)

// This adapter is on the beta namespace for one field: block_binding.
//
// A thinking block carries a signature bound to the conversation that
// produced it, and the model refuses a replayed block whose prefix no
// longer matches — "the block is bound to a different conversation".
// That is not a hypothetical: the bot replays a 30-minute, 20-turn
// window of stored turns, and a window is by definition not the whole
// conversation, so sooner or later the prefix a block was signed
// against is missing and every later message in that chat fails with a
// 400. It happened on the second message of a real conversation and the
// person was told the pantry could not be saved.
//
// drop_block makes that survivable: a block whose prefix still matches
// is replayed as before, and one whose prefix does not is dropped
// instead of failing the request. Reasoning is lost only where it could
// not have been valid anyway.
//
// It is a safety net, not a diagnosis. Each dropped block is reasoning
// the model does not get back, so an edit this code makes on purpose is
// still worth finding and removing.
var thinking = anthropic.BetaThinkingConfigParamUnion{
	OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{
		BlockBinding: anthropic.BetaThinkingBlockBindingParam{
			PrefixMismatchBehavior: anthropic.BetaThinkingPrefixMismatchBehaviorDropBlock,
		},
	},
}

// betas is the one beta this adapter needs; block_binding is refused
// without it.
var betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaThinkingBindingControls2026_08_01}

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

	// BaseURL overrides the API's host, which is what a test points at
	// so the mapping can be checked without spending anything.
	BaseURL string
}

func New(in Input) (*Client, error) {
	if in.APIKey == "" {
		return nil, errors.New("missing anthropic api key")
	}
	if in.Model == "" {
		return nil, errors.New("missing anthropic model")
	}
	options := []option.RequestOption{option.WithAPIKey(in.APIKey)}
	if in.BaseURL != "" {
		options = append(options, option.WithBaseURL(in.BaseURL))
	}

	return &Client{
		api:   anthropic.NewClient(options...),
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

// Reply asks for one turn. Thinking is adaptive — which is what the
// current models do anyway when it is unset — and configured only so
// that block_binding can be set; the depth comes from effort.
func (c *Client) Reply(ctx context.Context, req bot.Request) (bot.Turn, error) {
	messages, err := messagesOf(req.Turns)
	if err != nil {
		return bot.Turn{}, err
	}

	answer, err := c.api.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:        c.model,
		MaxTokens:    maxTokens,
		Betas:        betas,
		Thinking:     thinking,
		System:       []anthropic.BetaTextBlockParam{{Text: req.System}},
		Tools:        toolsOf(req.Tools),
		Messages:     messages,
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: effort},
	})
	if err != nil {
		return bot.Turn{}, fmt.Errorf("couldn't reach the model: %w", err)
	}

	// A declined request stops here rather than being retried under a
	// different wording: a pantry has nothing to argue about, and
	// pretending otherwise would hide a real problem.
	if answer.StopReason == anthropic.BetaStopReasonRefusal {
		return bot.Turn{}, fmt.Errorf("the model declined to answer: %s", answer.StopDetails.Category)
	}

	return turnOf(answer)
}

// turnOf keeps both halves: the fields the loop reads, and the exact
// message to replay. Thinking blocks are in the raw copy and nowhere
// else, which is the only way they survive the next request unedited.
func turnOf(answer *anthropic.BetaMessage) (bot.Turn, error) {
	turn := bot.Turn{Role: domain.TurnAssistant}

	for _, block := range answer.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.BetaTextBlock:
			turn.Text += variant.Text
		case anthropic.BetaToolUseBlock:
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
func messagesOf(turns []bot.Turn) ([]anthropic.BetaMessageParam, error) {
	messages := make([]anthropic.BetaMessageParam, 0, len(turns))

	for _, turn := range turns {
		if len(turn.Raw) > 0 {
			var message anthropic.BetaMessageParam
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
			messages = append(messages, newBetaAssistantMessage(blocks...))
			continue
		}
		messages = append(messages, anthropic.NewBetaUserMessage(blocks...))
	}
	return messages, nil
}

// newBetaAssistantMessage stands in for a helper the beta namespace
// does not provide.
func newBetaAssistantMessage(blocks ...anthropic.BetaContentBlockParamUnion) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: blocks,
	}
}

func blocksOf(turn bot.Turn) []anthropic.BetaContentBlockParamUnion {
	var blocks []anthropic.BetaContentBlockParamUnion

	// Tool results come first in the turn that answers a tool call, and
	// all of them go in one message: splitting them teaches the model
	// to stop asking for more than one tool at a time.
	for _, result := range turn.ToolResults {
		blocks = append(blocks, anthropic.NewBetaToolResultBlock(result.CallID, result.Content, result.IsError))
	}
	if turn.Text != "" {
		blocks = append(blocks, anthropic.NewBetaTextBlock(turn.Text))
	}
	for _, call := range turn.ToolCalls {
		blocks = append(blocks, anthropic.NewBetaToolUseBlock(call.ID, call.Args, call.Name))
	}
	return blocks
}

// toolsOf hands over the schemas as they were written. The map is the
// single source of what the model is shown and what validates the call,
// so additionalProperties travels in ExtraFields rather than being
// restated here.
func toolsOf(tools []agent.Tool) []anthropic.BetaToolUnionParam {
	params := make([]anthropic.BetaToolUnionParam, 0, len(tools))

	for _, tool := range tools {
		schema := anthropic.BetaToolInputSchemaParam{ExtraFields: map[string]any{}}
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

		params = append(params, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
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
