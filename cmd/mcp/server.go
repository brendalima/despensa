package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/humanize"
)

// version is reported to the host on the handshake.
const version = "0.1.0"

// mcpStdio is the transport: stdin and stdout, which is what a host
// launching a subprocess expects.
var mcpStdio = mcp.StdioTransport{}

// newServer is the whole adaptation between internal/agent and MCP. It
// is deliberately thin — the tools, their wording and their schemas are
// the agent package's, and this file only moves JSON across.
//
// Server.AddTool, not the generic top-level AddTool: the tools carry
// their own schemas because the enums come from the domain, and they
// validate their own arguments so that the WhatsApp bot, which has no
// MCP SDK in front of it, refuses exactly the same input.
func newServer(tools *agent.ToolSet) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "despensa",
		Version: version,
	}, nil)

	for _, tool := range tools.Tools() {
		server.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Schema,
		}, handlerFor(tools, tool.Name))
	}
	return server
}

func handlerFor(tools *agent.ToolSet, name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args json.RawMessage
		if req.Params != nil {
			args = req.Params.Arguments
		}

		result, err := tools.Call(ctx, name, args)
		if err != nil {
			// A tool failure comes back as a result, never as a
			// protocol error: the model has to be able to see what
			// went wrong and fix it, and the Portuguese sentence is
			// the one the person would have heard.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: humanize.Error(err)},
					&mcp.TextContent{Text: err.Error()},
				},
			}, nil
		}

		encoded, err := json.Marshal(result)
		if err != nil {
			// Unreachable short of a bug in a view: this one is the
			// server's fault, not the model's.
			return nil, fmt.Errorf("couldn't encode the result of %s: %w", name, err)
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(encoded)}},
			StructuredContent: result,
		}, nil
	}
}
