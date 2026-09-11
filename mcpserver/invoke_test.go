package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool runs one tool handler the way the transport would, without a client
// in the way.
func callTool(t *testing.T, broker *Plugin, capability mcp.Capability, arguments string) *sdk.CallToolResult {
	t.Helper()
	result, err := broker.handle(capability)(t.Context(), &sdk.CallToolRequest{
		Params: &sdk.CallToolParamsRaw{Name: capability.Name(), Arguments: json.RawMessage(arguments)},
	})
	if err != nil {
		t.Fatalf("tool handler returned a protocol error: %v", err)
	}
	return result
}

func resultText(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result carries no content")
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", result.Content[0])
	}
	return text.Text
}

// A dispatch cancelled because the game is closing is a normal outcome, not a
// system fault: reporting it would terminate an engine that is already exiting.
func TestInvoke_ShutdownRendersAsUnavailableRatherThanAFault(t *testing.T) {
	for name, failure := range map[string]error{
		"scheduler stopped": kernel.ErrSchedulerStopped{},
		"context canceled":  context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			capability := mcp.Func("stopping", "", func(kernel.Executioner, echoRequest) (echoResponse, error) {
				return echoResponse{}, failure
			})
			broker := testBroker()
			reported := runEngine(t, &testProvider{name: "probe",
				capabilities: []mcp.Capability{capability}}, broker)

			result := callTool(t, broker, capability, `{}`)
			if !result.IsError {
				t.Fatal("a refusal must be an ordinary result with IsError set")
			}
			if got := resultText(t, result); got != "the game is shutting down" {
				t.Fatalf("reason = %q, want the shutdown wording", got)
			}
			select {
			case err := <-reported:
				t.Fatalf("shutdown was reported as a fault: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

// An Unavailable is the provider's own words, passed through untouched.
func TestInvoke_UnavailableReachesTheAgentVerbatim(t *testing.T) {
	reason := "no frame was rendered within 2s — the game may be paused, minimised, or not rendering"
	capability := mcp.Func("busy", "", func(kernel.Executioner, echoRequest) (echoResponse, error) {
		return echoResponse{}, mcp.Unavailable{Reason: reason}
	})
	broker := testBroker()
	runEngine(t, &testProvider{name: "probe", capabilities: []mcp.Capability{capability}}, broker)

	result := callTool(t, broker, capability, `{}`)
	if !result.IsError || resultText(t, result) != reason {
		t.Fatalf("result = %+v, want the reason verbatim", result)
	}
}

// Anything else is a system fault: it is reported through the kernel and the
// agent gets a generic failure, with no internals in the model's context.
func TestInvoke_SystemFaultIsReportedAndNotLeaked(t *testing.T) {
	secret := errors.New("nil map write in the render queue")
	capability := mcp.Func("broken", "", func(kernel.Executioner, echoRequest) (echoResponse, error) {
		return echoResponse{}, secret
	})
	broker := testBroker()
	reported := runEngine(t, &testProvider{name: "probe",
		capabilities: []mcp.Capability{capability}}, broker)

	result := callTool(t, broker, capability, `{}`)
	if !result.IsError {
		t.Fatal("a fault still answers the agent")
	}
	if strings.Contains(resultText(t, result), secret.Error()) {
		t.Fatalf("the fault leaked into the agent's context: %q", resultText(t, result))
	}
	if err := firstError(t, reported); !errors.Is(err, secret) {
		t.Fatalf("reported %v, want the fault", err)
	}
}

// Arguments that do not fit the schema are a refusal in words, not a fault.
func TestInvoke_UnparseableArgumentsRefuseInWords(t *testing.T) {
	capability := echoing("echo")
	broker := testBroker()
	reported := runEngine(t, &testProvider{name: "probe",
		capabilities: []mcp.Capability{capability}}, broker)

	result := callTool(t, broker, capability, `{"text": 7}`)
	if !result.IsError || !strings.Contains(resultText(t, result), "schema") {
		t.Fatalf("result = %+v, want a refusal naming the schema", result)
	}
	select {
	case err := <-reported:
		t.Fatalf("bad arguments were reported as a fault: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

// The deadline bounds the wait, not the work: the agent gets a clean refusal
// instead of burning its session on the client's idle abort.
func TestInvoke_TimeoutRefusesInWords(t *testing.T) {
	capability := mcp.Func("slow", "", func(k kernel.Executioner, _ echoRequest) (echoResponse, error) {
		<-k.Context().Done()
		return echoResponse{}, k.Context().Err()
	})
	broker := New(Config{Addr: "127.0.0.1:0", Timeout: 20 * time.Millisecond}).(*Plugin)
	reported := runEngine(t, &testProvider{name: "probe",
		capabilities: []mcp.Capability{capability}}, broker)

	result := callTool(t, broker, capability, `{}`)
	if !result.IsError || !strings.Contains(resultText(t, result), "did not answer within") {
		t.Fatalf("result = %+v, want a deadline refusal", result)
	}
	select {
	case err := <-reported:
		t.Fatalf("the deadline was reported as a fault: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}
