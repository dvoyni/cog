// Package mcpserver is the broker: the one plugin that collects capabilities
// from every mcp.Provider in the engine and serves them to an agent over the
// Model Context Protocol. It imports kernel, mcp, the official Go MCP SDK and a
// JSON-schema library, and it imports no provider.
//
// That absence is the design. The broker renders; it does not know. Everything
// it can say about a capability it learned from an mcp.Capability value, and
// there is no line in it that names a capability, a package, or a kind of thing
// an agent might want.
//
// # Composition is the gate
//
// The broker declares no plugin dependencies, which is what makes this an
// extension point: an app composes exactly the providers it has and the broker
// serves exactly what it finds. An app that does not list New() has no agent
// interface at all, which is a stronger guarantee than any flag.
//
// # The retained executioner
//
// kernel's rule stands as written everywhere else: a handle is scoped to the
// dispatch that received it, and retaining it is a bug. This package is a
// named, local exception. It retains the kernel.Executioner it was handed at
// Start, past the return of Start, because reaching a provider correctly means
// dispatching a command and ExecuteCommand is a method on an Executioner value.
//
// What makes the exception safe is mechanical rather than a promise. The Start
// executioner is a root executioner holding no locks, so every dispatch through
// it acquires its own lock set exactly as a lifecycle dispatch does. It is an
// immutable value, so concurrent use from many HTTP goroutines is safe. And it
// carries its own expiry: it is bound to the engine context, which Run cancels
// before the reverse-order Stop loop begins, so every dispatch attempted after
// that point fails on a cancelled context rather than reaching a stopped
// plugin.
//
// The full design is in mcpserver/docs/specs/mcp.md.
package mcpserver

import (
	"context"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Name is the broker plugin's name, and therefore the prefix on its own tool.
const Name kernel.PluginName = "mcpserver"

// serverName is how the engine introduces itself to a client during
// initialization. It names the engine rather than the plugin, because that is
// what the person attaching is looking at.
const serverName = "cog"

// Plugin is the broker. It owns the HTTP listener, the rendered tool set, and
// the executioner every capability body dispatches through.
type Plugin struct {
	config      Config
	executioner kernel.Executioner
	listener    net.Listener
	server      *http.Server

	// stop unblocks the shutdown watcher when Stop runs without the engine
	// context having been cancelled, which only a test composes. drained closes
	// once the watcher has finished draining in-flight handlers.
	stop      chan struct{}
	stopOnce  sync.Once
	drained   chan struct{}
	closeOnce sync.Once
}

// New creates the broker. The optional Config overrides the transport
// defaults; its zero value means all of them.
func New(config ...Config) kernel.Plugin {
	var resolved Config
	if len(config) > 0 {
		resolved = config[0]
	}
	return &Plugin{config: resolved.withDefaults()}
}

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins the broker requires; it has none, so an app
// listing it never has to also list every provider it might serve.
func (p *Plugin) Dependencies() []kernel.PluginName { return nil }

// Register declares nothing: the broker owns no command, event or resource.
func (p *Plugin) Register(*kernel.Registrar, any) error { return nil }

// Capabilities reports what the broker offers an agent itself. Collection is
// uniform, so k.Plugins[mcp.Provider] finds the broker among the providers and
// its own capability arrives through the same path as everyone else's. That
// looks like a bug when read cold, and is not.
//
// The set is closed at one. The broker provides a capability of its own only
// for facts about composition; anything that invokes another provider's
// capability would be the knower, whatever it was called.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(architectureName, architectureDescription, p.architecture, mcp.ReadOnly()),
	}
}

// Start collects every provider's capabilities, renders them as tools, and
// serves them. The provider list is complete and final here regardless of start
// order, because the engine fixes it during composition.
//
// A known flaw, stated rather than left to be found: the broker declares no
// dependencies and plugin ordering is stable in the app author's listing order,
// so it typically starts early — before, say, a provider mounts the assets its
// capability reads. A tool call landing in that sub-millisecond window would
// dispatch into a half-initialized engine. Listing New() last closes it in
// practice; subscribing to a host's init event would close it entirely, at the
// cost of never serving a headless engine, which is exactly the shape a test or
// a CI-driven agent composes.
func (p *Plugin) Start(k kernel.Executioner) error {
	p.executioner = k

	offers, err := collect(k.Plugins[mcp.Provider]())
	if err != nil {
		return err
	}
	tools, err := render(offers)
	if err != nil {
		return err
	}

	// The tool set is fixed here and never changes, so nothing ever needs to
	// send tools/list_changed — which a stateless server could not send anyway.
	server := sdk.NewServer(&sdk.Implementation{Name: serverName}, nil)
	for i, tool := range tools {
		server.AddTool(tool, p.handle(offers[i].capability))
	}

	listener, err := net.Listen("tcp", p.config.Addr)
	if err != nil {
		return ErrListen{Addr: p.config.Addr, Err: err}
	}
	p.listener = listener

	mux := http.NewServeMux()
	mux.Handle(p.config.Path, sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{
			// These are one position rather than independent knobs. The newest
			// protocol revision is served over streamable HTTP only when
			// stateless, and request-cancellation propagation — which ties a
			// tool handler's context to the HTTP request's — takes effect only
			// at that revision. Statelessness is also the transport-level
			// expression of a contract decision: there is no session identity,
			// so no future capability can reach for one.
			Stateless:                    true,
			JSONResponse:                 true,
			PropagateRequestCancellation: true,
		}))
	p.server = &http.Server{Handler: mux}
	go func() { _ = p.server.Serve(listener) }()

	p.stop = make(chan struct{})
	p.drained = make(chan struct{})
	go p.watch(k.Context())

	// One line at startup turns "what was the URL" into copy-paste.
	log.Printf("mcpserver: claude mcp add --transport http %s %s", serverName, p.endpoint())
	return nil
}

// endpoint is the URL an agent's client attaches to. It reports the bound
// address rather than the configured one, so a game that asked for port 0 still
// prints something a person can paste.
func (p *Plugin) endpoint() string {
	return "http://" + p.listener.Addr().String() + p.config.Path
}

// Stop waits for the drain the cancellation watcher is already performing, then
// closes the listener as a belt-and-braces second call.
func (p *Plugin) Stop(kernel.Executioner) error {
	if p.drained == nil {
		return nil
	}
	p.stopOnce.Do(func() { close(p.stop) })
	<-p.drained
	if p.listener != nil {
		_ = p.listener.Close()
	}
	return nil
}

// watch closes the server on engine-context cancellation rather than in Stop.
// Three facts decide this, and they are the part of the design most likely to
// be got wrong by someone reimplementing it. Run cancels the engine context
// before the Stop loop, after which every dispatch fails, so a broker waiting
// for Stop would be serving an engine that can no longer execute anything. Stop
// order is reverse start order, so a dependency-less broker listed first stops
// last. And app has no plugin, so nothing can declare a dependency on it to
// force the ordering.
func (p *Plugin) watch(engine context.Context) {
	defer close(p.drained)
	select {
	case <-engine.Done():
	case <-p.stop:
	}
	p.close()
}

// close stops accepting requests and drains the handlers already running. The
// drain is bounded by the configured timeout for the same reason the timeout
// exists at all: Go cannot interrupt a command body that is already running,
// and a wedged one must not hold the whole engine open.
//
// The window this cannot close alone: the host loop has already returned by the
// time cancellation fires, so a call waiting for the next frame waits for a
// frame that will never come. That is a second, independent reason every
// blocking capability needs a deadline of its own.
func (p *Plugin) close() {
	p.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil {
			_ = p.server.Close()
		}
	})
}
