package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoRequest struct {
	Text string `json:"text"`
}

type echoResponse struct {
	Text string `json:"text"`
}

// testProvider is a plugin that offers whatever the test hands it. It is also a
// PluginStopper, which is how the shutdown-ordering test observes the Stop loop.
type testProvider struct {
	name         kernel.PluginName
	capabilities []mcp.Capability
	stop         func()
}

func (p *testProvider) Name() kernel.PluginName               { return p.name }
func (p *testProvider) Dependencies() []kernel.PluginName     { return nil }
func (p *testProvider) Register(*kernel.Registrar, any) error { return nil }
func (p *testProvider) Capabilities() []mcp.Capability        { return p.capabilities }

func (p *testProvider) Stop(kernel.Executioner) error {
	if p.stop != nil {
		p.stop()
	}
	return nil
}

func echoing(name string, opts ...mcp.Option) mcp.Capability {
	return mcp.Func(name, "echo the text back", func(_ kernel.Executioner, request echoRequest) (echoResponse, error) {
		return echoResponse{Text: request.Text + "!"}, nil
	}, opts...)
}

// runEngine composes and runs an engine, returning the errors its handler saw.
// The engine is cancelled and awaited at cleanup, so every test exercises the
// whole shutdown path.
func runEngine(t *testing.T, plugins ...kernel.Plugin) <-chan error {
	t.Helper()
	reported := make(chan error, 8)
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).
		Handler(func(err error) bool { reported <- err; return true }).
		WithPlugins(plugins...)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		engine.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("engine did not finish shutting down")
		}
	})
	<-engine.Ready()
	return reported
}

func testBroker() *Plugin {
	return New(Config{Addr: "127.0.0.1:0"}).(*Plugin)
}

func firstError(t *testing.T, reported <-chan error) error {
	t.Helper()
	select {
	case err := <-reported:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("no error reported")
		return nil
	}
}

func attach(t *testing.T, broker *Plugin) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := sdk.NewClient(&sdk.Implementation{Name: "cog-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: broker.endpoint(), DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect to %s: %v", broker.endpoint(), err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// A game composing the broker serves MCP on its configured localhost address,
// and a client attaches over the transport a committed .mcp.json names.
func TestBroker_ServesCapabilitiesToAnAttachedClient(t *testing.T) {
	broker := testBroker()
	provider := &testProvider{name: "probe", capabilities: []mcp.Capability{echoing("echo", mcp.ReadOnly())}}
	runEngine(t, provider, broker)

	ctx := t.Context()
	session := attach(t, broker)

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !contains(names, "probe_echo") || !contains(names, "mcpserver_architecture") {
		t.Fatalf("tools/list = %v, want the provider's tool and the broker's own", names)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "probe_echo" {
			continue
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Error("ReadOnly() did not reach the tool annotation")
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Error("DestructiveHint must be stated false, never derived from !readOnly")
		}
		if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Error("a running game is a closed world")
		}
	}

	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "probe_echo", Arguments: map[string]any{"text": "hi"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if result.IsError {
		t.Fatalf("tools/call reported an error: %+v", result.Content)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || structured["text"] != "hi!" {
		t.Fatalf("structuredContent = %#v, want the response struct", result.StructuredContent)
	}
}

// The prefix is the plugin name, so the same capability name from two providers
// is two distinct tools; the same name twice from one provider is a composition
// failure.
func TestStart_CapabilityNamesAreUniqueWithinAProvider(t *testing.T) {
	broker := testBroker()
	first := &testProvider{name: "first", capabilities: []mcp.Capability{echoing("echo")}}
	second := &testProvider{name: "second", capabilities: []mcp.Capability{echoing("echo")}}
	runEngine(t, first, second, broker)

	tools, err := attach(t, broker).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !contains(names, "first_echo") || !contains(names, "second_echo") {
		t.Fatalf("tools/list = %v, want both providers' tools", names)
	}
}

func TestStart_DuplicateCapabilityWithinOneProviderFailsComposition(t *testing.T) {
	provider := &testProvider{name: "twice", capabilities: []mcp.Capability{
		echoing("echo"), echoing("echo"),
	}}
	reported := runEngine(t, provider, testBroker())

	var duplicate ErrDuplicateCapability
	if err := firstError(t, reported); !errors.As(err, &duplicate) || duplicate.Capability != "echo" {
		t.Fatalf("reported %v, want ErrDuplicateCapability", err)
	}
}

// A malformed capability fails composition rather than being skipped: a
// silently absent tool is close to undebuggable from the agent's side.
func TestStart_MalformedCapabilityFailsComposition(t *testing.T) {
	provider := &testProvider{name: "bad", capabilities: []mcp.Capability{
		mcp.Func("bare", "not describable", func(kernel.Executioner, string) (echoResponse, error) {
			return echoResponse{}, nil
		}),
	}}
	reported := runEngine(t, provider, testBroker())

	var malformed ErrMalformedCapability
	if err := firstError(t, reported); !errors.As(err, &malformed) {
		t.Fatalf("reported %v, want ErrMalformedCapability", err)
	}
	var payload mcp.ErrNonStructPayload
	if !errors.As(malformed.Err, &payload) {
		t.Fatalf("wrapped error = %v, want the deferred construction failure", malformed.Err)
	}
}

// A bind failure terminates the engine with the address in the error. The same
// answer covers a browser build, where net.Listen never works: no build tag
// excludes the plugin, so the one build that is wrong fails at startup.
func TestStart_BindFailureTerminatesTheEngine(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve an address: %v", err)
	}
	defer held.Close()

	addr := held.Addr().String()
	reported := runEngine(t, New(Config{Addr: addr}))

	var listenErr ErrListen
	if err := firstError(t, reported); !errors.As(err, &listenErr) {
		t.Fatalf("reported %v, want ErrListen", err)
	}
	if listenErr.Addr != addr || !strings.Contains(listenErr.Error(), addr) {
		t.Fatalf("ErrListen = %v, want the address in the error", listenErr)
	}
}

// The broker logs the line that turns "what was the URL" into copy-paste.
func TestStart_LogsItsOwnAttachLine(t *testing.T) {
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	broker := testBroker()
	runEngine(t, broker)

	want := "mcpserver: claude mcp add --transport http cog " + broker.endpoint()
	if !strings.Contains(logged.String(), want) {
		t.Fatalf("log = %q, want it to contain %q", logged.String(), want)
	}
}

// The server closes on engine-context cancellation, not in Stop. The broker is
// listed first, so it stops last: a provider's Stop waiting for the broker's
// drain would deadlock if the drain were gated on the Stop loop.
func TestBroker_ShutsDownBeforeAnyProviderStops(t *testing.T) {
	broker := testBroker()
	drainedBeforeStop := make(chan bool, 1)
	provider := &testProvider{name: "probe", stop: func() {
		select {
		case <-broker.drained:
			drainedBeforeStop <- true
		case <-time.After(5 * time.Second):
			drainedBeforeStop <- false
		}
	}}

	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(broker, provider)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		engine.Run(ctx)
	}()
	<-engine.Ready()
	endpoint := broker.endpoint()

	cancel()
	select {
	case drained := <-drainedBeforeStop:
		if !drained {
			t.Fatal("the provider's Stop ran before the server had shut down")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the provider never stopped")
	}
	<-stopped

	if _, err := net.DialTimeout("tcp", strings.TrimPrefix(strings.TrimSuffix(endpoint, DefaultPath), "http://"), time.Second); err == nil {
		t.Fatal("the listener is still accepting connections after shutdown")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
