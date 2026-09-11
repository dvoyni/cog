# mcpserver

`github.com/dvoyni/cog/mcpserver` is the **broker**: the one plugin that
collects capabilities from every [`mcp.Provider`](../mcp/README.md) in the
engine and serves them to an agent over the Model Context Protocol. It imports
`kernel`, `mcp`, the official Go MCP SDK and a JSON-schema library, and it
imports **no provider**.

That absence is the design. The broker renders; it does not know. Everything it
can say about a capability it learned from an `mcp.Capability` value, and there
is no line in it that names a capability, a package, or a kind of thing an agent
might want. A test asserts the other half of the split: neither the SDK nor the
schema library appears in any other package's import graph.

## Plugin

- Name: `mcpserver.Name` (`"mcpserver"`)
- Constructor: `mcpserver.New(cfg ...Config) kernel.Plugin`
- Plugin dependencies: **none**
- Go package dependencies: `kernel`, `mcp`,
  `github.com/modelcontextprotocol/go-sdk`, `github.com/google/jsonschema-go`
- Configuration: the optional `Config` passed to `New`

Declaring no dependencies is what makes this an extension point: an app
composes exactly the providers it has and the broker serves exactly what it
finds. Declaring them would make the broker name every provider it might ever
serve, and force every app listing `New()` to also list all of them.

**Composition is the gate.** An app that does not list `mcpserver.New()` has no
agent interface, which is a stronger guarantee than any flag.

The plugin implements `mcp.Provider` over itself, so
`k.Plugins[mcp.Provider]()` finds the broker among the providers and its own
capability arrives through the same path as everyone else's.

## Files

`plugin.go` holds the lifecycle and package documentation, `config.go` the
transport configuration, `render.go` the capability-to-tool rendering,
`invoke.go` the per-call sequence, `architecture.go` the broker's one
capability, and `err.go` the errors.

## Config

```go
type Config struct {
    Addr    string        // default "127.0.0.1:7654"
    Path    string        // default "/mcp"
    Timeout time.Duration // default 30s
}
```

The zero value means all defaults. `127.0.0.1` is spelled literally, never
`localhost`: the IPv4/IPv6 mismatch is the classic failure here. The port is
arbitrary within a band — below the OS ephemeral range and clear of common dev
ports — and what matters is that it is stable across runs, so a game can commit
a `.mcp.json` naming it.

**`Timeout` bounds the wait, not the work.** It is applied as a deadline on the
executioner a capability receives, so it cancels lock acquisition and any
cooperative wait, but Go cannot interrupt a command body that is already
running. What it buys is a clean `mcp.Unavailable` in 30 seconds instead of five
minutes of the agent's session spent on the client's idle abort. That is a
courtesy, not a safety property.

There is no capture output directory, no enable flag, no auth field and no log
level. The agent names every path it wants written; composition is the gate;
auth is a different effort; logging is one line at startup.

## Lifecycle

At `Start` the broker caches `k.Plugins[mcp.Provider]()`, calls `Capabilities()`
on each exactly once, validates and renders every capability as a tool, retains
the `Start` executioner, listens, and starts one goroutine waiting on
`k.Context().Done()`.

The transport position is one decision rather than four knobs:

```
Stateless:                    true
PropagateRequestCancellation: true
JSONResponse:                 true
DisableLocalhostProtection:   false   (the default)
MaxRequestBodyBytes:          4 MiB   (the default)
```

The newest protocol revision is served over streamable HTTP only when
stateless, and cancellation propagation takes effect only at that revision.
Statelessness is also the transport-level expression of a contract decision:
there is no session identity, so no future capability can reach for one, and
nothing is undone when an agent's session ends.

**The server closes on engine-context cancellation, not in `Stop`.** `Run`
cancels the engine context before the `Stop` loop, after which every dispatch
fails — a broker waiting for `Stop` would be serving an engine that can no
longer execute anything. `Stop` waits for the drain and closes the listener as a
belt-and-braces second call.

A tool call cancelled because the game is closing is rendered as
`mcp.Unavailable{Reason: "the game is shutting down"}` rather than reported as a
system fault, because a game exiting is the normal case.

**A bind failure terminates the engine**, with the address in `ErrListen`. A
broker that is silently absent is undebuggable from the agent's side, which
cannot tell *not offered* from *not running*. The same answer covers a browser
build: `net.Listen` does not work there, and an app that composed the broker
into a web build made a composition mistake. This is deliberately not a build
tag — excluding the plugin on `js` would break a `main.go` shared between
desktop and web builds at compile time.

## Rendering a capability

The broker owns names, schemas, annotations and the result envelope. A provider
owns the description prose and nothing else that reaches the wire.

- **Tool name** is `<plugin>_<capability>`. Uniqueness across providers is
  inherited from the engine's own rejection of duplicate plugin names; a
  duplicate *within* one provider fails composition.
- **Schemas** are inferred from the request and response types, with
  `mcp.TextValued` types overridden by the string schema they actually cross the
  wire as.
- **Annotations**: `ReadOnlyHint` is the provider's statement,
  `DestructiveHint` is always false and never derived from `!readOnly`,
  `OpenWorldHint` is always false, `IdempotentHint` is left alone.
- **A malformed capability terminates the engine** rather than being skipped,
  which also guards the SDK's own panic on a nil or non-`object` input schema.

The tool set is fixed at `Start` and never changes, so `tools/list_changed` is
never needed — which is fortunate, because a stateless server cannot send it.

## Invoking one

Per call, on the HTTP goroutine: allocate the request type and unmarshal into
it, derive an executioner bound to the request context with the configured
timeout, invoke, then render an `mcp.Unavailable` as an ordinary result with
`IsError` set, any other error through `kernel.ReportError` plus a generic
failure, and a success as `structuredContent`.

Two simultaneous calls are two dispatches the scheduler already serializes by
resource, so the broker imposes no limit and must not serialize calls itself:
doing so would break the parallel arms an agent uses to pair a moment.

## The retained executioner

`kernel`'s rule stands as written everywhere else — a handle is scoped to the
dispatch that received it, and retaining it is a bug. **This package is a named,
local exception**, because reaching a provider correctly means dispatching a
command and `ExecuteCommand` is a method on an `Executioner` value.

What makes the exception safe is mechanical rather than a promise: the `Start`
executioner is a root executioner holding no locks, so every dispatch through it
acquires its own lock set; it is an immutable value, so concurrent use from many
HTTP goroutines is safe; and it carries its own expiry, because engine
cancellation fires strictly before any plugin's `Stop`.

## `mcpserver_architecture`

The broker's one capability, and a closed set of one: **the broker provides a
capability of its own only for facts about composition.**

What earns it is the resolved, transitive lock closure each handler ends up
holding once the commands it declares are folded in, plus the `uses` edges that
explain it — the one thing an agent cannot compute by reading source, because a
handler deliberately never names the resources behind a command it uses. It
returns flat JSON in four arrays, addressed by type string, and writes a file
instead when given an absolute `.json` path.

## Attaching

Nothing to do, if the game repo commits `.mcp.json`:

```json
{ "mcpServers": { "cog": { "type": "http", "url": "http://127.0.0.1:7654/mcp" } } }
```

**Start the game, then attach.** A client started first marks the server failed
long before the window opens; if the game restarts mid-session, reconnect from
the client. The broker logs its own attach line at startup:

```
mcpserver: claude mcp add --transport http cog http://127.0.0.1:7654/mcp
```

## Specification

[`docs/specs/mcp.md`](docs/specs/mcp.md).
