package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// handle builds the tool handler for one capability. Everything it does runs on
// the HTTP goroutine: the scheduler already serializes two simultaneous calls
// by the resources they lock, so the broker imposes no limit of its own —
// serializing here would break the parallel arms that let an agent pair a
// moment.
func (p *Plugin) handle(capability mcp.Capability) sdk.ToolHandler {
	return func(ctx context.Context, request *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		payload := reflect.New(capability.RequestType())
		if arguments := request.Params.Arguments; len(arguments) > 0 {
			if err := json.Unmarshal(arguments, payload.Interface()); err != nil {
				return refuse(mcp.Unavailable{
					Reason: fmt.Sprintf("the arguments do not fit this tool's schema: %v", err),
				}), nil
			}
		}

		// The deadline and the agent's own hang-up reach the dispatch through
		// one handle: WithContext unbinds the executioner from the engine
		// context, and ExecuteCommand re-links engine cancellation itself, so a
		// dispatch dies on either lifetime with no bookkeeping in the body.
		ctx, cancel := context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()

		response, err := capability.Invoke(p.executioner.WithContext(ctx), payload.Interface())
		if err != nil {
			if unavailable, expected := p.classify(err); expected {
				return refuse(unavailable), nil
			}
			p.executioner.ReportError(err)
			return refuse(mcp.Unavailable{Reason: "the engine failed while running this tool"}), nil
		}

		encoded, err := json.Marshal(response)
		if err != nil {
			p.executioner.ReportError(err)
			return refuse(mcp.Unavailable{Reason: "the engine could not encode this tool's answer"}), nil
		}
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: string(encoded)}},
			StructuredContent: response,
		}, nil
	}
}

// classify splits an expected refusal from a system fault. Normal refusal and a
// broken engine end up on opposite sides of a type boundary rather than inside
// a string.
//
// Shutdown is the case worth naming: a dispatch cancelled because the game is
// closing would otherwise be reported as a system fault on the way out, which
// is both wrong and noisy. A game exiting is the normal case here.
func (p *Plugin) classify(err error) (mcp.Unavailable, bool) {
	var unavailable mcp.Unavailable
	if errors.As(err, &unavailable) {
		return unavailable, true
	}
	var stopped kernel.ErrSchedulerStopped
	if errors.As(err, &stopped) || errors.Is(err, context.Canceled) {
		return mcp.Unavailable{Reason: "the game is shutting down"}, true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the engine did not answer within %s — it may be paused, or another call may hold "+
				"the resources this one needs", p.config.Timeout)}, true
	}
	return mcp.Unavailable{}, false
}

// refuse renders an expected refusal the way the protocol wants it: an ordinary
// tool result carrying the reason, not a JSON-RPC error, so the model reads it
// and picks something else.
func refuse(unavailable mcp.Unavailable) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: unavailable.Reason}},
	}
}
