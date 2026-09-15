# mcp

`github.com/dvoyni/cog/bundles/mcp` is the agent-facing extension point: how a
plugin offers typed **capabilities** to an **agent**. Its plugin, the
**broker**, collects every plugin's capabilities and serves them to an agent
over the Model Context Protocol.

mcp is a **Bundle**: it requires no Adapter, collects every Adapter provided for
its `ProviderPort`, and contributes one of its own. The vocabulary is in
[`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

mcp has the declaration-root shape of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md).

- **`bundles/mcp`** is the root, and holds declarations only: the `Provider`
  interface and `ProviderPort`, `Capability`, `Option`, `TextValued`, the
  `McpProvider` Adapter, `Config`, `Unavailable` and the errors, and `Name`. Its
  functions, `Command`, `Func` and `ReadOnly`, are forwarders in `utils.go`. It
  declares no plugin and imports only `kernel` and the standard library, and it
  is what every provider imports.
- **`bundles/mcp/internal/types`** declares `Capability`, whose unexported
  fields only `Command` and `Func` may set, `Option` with the settings it writes,
  and the two deferred construction failures, `ErrInvalidCapabilityName` and
  `ErrNonStructPayload`. The root aliases every one of them.
- **`bundles/mcp/internal`** is the broker: its `New`, the transport, the
  capability-to-tool rendering, the per-call sequence and its one capability,
  `mcpserver_architecture`. It alone imports the official Go MCP SDK and a
  JSON-schema library.
- **`bundles/mcp/mcpplugin`** exports only `New() kernel.Plugin`. Only
  composition roots and tests import it.

The split is load-bearing: `mcp` must never learn protocol vocabulary, and
nothing importing `gfx` should acquire an HTTP server and a JSON-schema library
in its module graph. A test asserts it: neither the SDK nor the schema library
appears in the import graph of any package but the broker and its constructor.

## Plugin

- Name: `mcp.Name` (`"mcpserver"`). The plugin kept the name it had as the
  `mcpserver` package, so its tool is still `mcpserver_architecture`.
- Constructor: `mcpplugin.New() kernel.Plugin`
- Requires: no Adapter
- Collects: every Adapter for `mcp.ProviderPort`
- Contributes: one `mcp.Provider`, as the `mcp.McpProvider` Adapter
- Plugin dependencies: **none**
- Go package dependencies: the root, `kernel` and the standard library; the
  broker adds `github.com/modelcontextprotocol/go-sdk` and
  `github.com/google/jsonschema-go`
- Configuration: an optional `Config` under `mcp.Name` in `kernel.New`'s config
  map

Declaring no dependencies is what makes this an extension point: an app
composes exactly the providers it has and the broker serves exactly what it
finds. Declaring them would make the broker name every provider it might ever
serve, and force every app listing `mcpplugin.New()` to also list all of them.

**Composition is the gate.** An app that does not list `mcpplugin.New()` has no
agent interface, which is a stronger guarantee than any flag. Its providers
still contribute their Adapters, which bind to nothing, and the engine runs.

The broker contributes an `mcp.Provider` to the interface it collects, so its
own capability is bound among everyone else's and arrives through the same
path.

The broker imports **no provider**. That absence is the design. The broker
renders; it does not know. Everything it can say about a capability it learned
from an `mcp.Capability` value, and there is no line in it that names a
capability, a package, or a kind of thing an agent might want.

## `Provider`

```go
type Provider interface {
    Capabilities() []Capability
}
```

`Provider` is the interface of `ProviderPort`, the Port the broker collects. A
plugin declares its Adapter type in its root's `adapters.go` and contributes one
from its `Register`, usually a small unexported value:

```go
type McpProvider kernel.Adapter[mcp.ProviderPort]

registrar.ProvideAdapter[canvas.McpProvider](mcp.Provider(provider{}))
```

One interface, not one per kind of capability, so adding a kind edits neither
this package nor the broker. The broker declares
`CollectAdapters[mcp.ProviderPort]()` in its `Register`, reads the bound set at its
own `Start`, and calls `Capabilities()` on each exactly once. It namespaces tool
names by the `PluginName` of the plugin that contributed each Provider, which
the engine records when it binds the Adapter; a provider carries no name of its
own.

An engine composed without the broker still runs: an Adapter nobody collects is
not an error.

Capabilities are static for the engine lifetime. A provider with nothing to
offer right now says so **inside** its capability — an empty list, or an
`Unavailable` from a call — never by withdrawing its Adapter. Empty, not
absent.

## `Capability`

```go
func Command[TCmd kernel.CommandConstraint[TReq, TResp], TReq, TResp any](
    name, description string, opts ...Option,
) Capability

func Func[TReq, TResp any](
    name, description string,
    invoke func(kernel.Executioner, TReq) (TResp, error),
    opts ...Option,
) Capability
```

`Command` is the common case and carries zero glue: the provider already has a
typed command, and the capability is the statement that an agent may dispatch
it. `Func` is the escape for a capability that cannot be one dispatch — a body
that must arm something and then wait for a frame, where blocking inside a
command handler would hold the lock set against the very work it waits for.

`Capability` is a struct with unexported fields, and only these two constructors
may build one. That is what makes the capability-body rule unforgeable. The
broker reads it through `Name`, `Description`, `RequestType`, `ResponseType`,
`ReadOnly`, `Err` and `Invoke`.

Both `TRequest` and `TResponse` must be **structs**: a JSON schema root has to
be an object, so a one-value answer is a one-field struct. Names are validated
`^[a-z][a-z0-9_]*$`. Neither failure panics — `Capabilities` returns a slice
literal and has nowhere to return an error — so both defer into `Err` for the
broker to collect at `Start`, where they fail composition loudly.

### The capability-body rule

> The capability body runs on the broker's goroutine and holds no locks. It may
> dispatch and it may wait. It may not touch provider state.

One narrow exception:

> Engine-immutable data finalized before `Run` may be read directly from the
> `Executioner`. `Describe` is the only such source. Everything else goes
> through a dispatch.

## Options

`ReadOnly()` states that a capability does not change the game. That is a fact
about a cog command rather than protocol vocabulary, so a provider may state it
and the broker translates. The option list is variadic so later options do not
break every provider.

## Errors

`Unavailable{Reason}` reports that a capability cannot run right now, in words
meant for an agent to read and act on. It is an expected outcome, not a fault:
the broker renders it as an ordinary tool result. **Any other error is a system
fault**, reported through the kernel and returned to the agent as a generic
failure, with no internals in the model's context.

`ErrInvalidCapabilityName` and `ErrNonStructPayload` are the two deferred
construction failures. `ErrListen`, `ErrMalformedCapability`,
`ErrDuplicateCapability` and `ErrNonObjectSchema` are the broker's own failures,
each terminating the engine; they are declared in the root so a caller can match
them.

## Types that cross as text

Schema inference reads the Go **kind**, so a named integer with a `MarshalText`
marshals as a string but infers as an integer — a schema describing nothing the
capability accepts. A payload type may state what it actually accepts:

```go
type TextValued interface {
    encoding.TextMarshaler
    encoding.TextUnmarshaler
    TextValues() (values []string, other string)
}
```

The broker walks every request and response type once at `Start`, finds these
wherever they are nested, and renders each as a string schema carrying its
accepted values — plus a pattern branch when `other` is non-empty. What it
learns is a string set, never a meaning.

## Config

```go
type Config struct {
    Addr    string        // default "127.0.0.1:7654"
    Path    string        // default "/mcp"
    Timeout time.Duration // default 30s
}
```

A zero field takes its default, and no value at all means every default; a
value that is not a `Config` fails registration.

```go
kernel.New(map[kernel.PluginName]any{mcp.Name: mcp.Config{Addr: "127.0.0.1:7655"}})
```

`127.0.0.1` is spelled literally, never
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

In `Register` the broker declares `CollectAdapters[mcp.ProviderPort]()` and
provides its own as `mcp.McpProvider`. At `Start` it reads the bound set, which the engine completed
during composition, calls `Capabilities()` on each exactly once, validates and renders every capability as a tool, retains
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

- **Tool name** is `<plugin>_<capability>`, where `<plugin>` is the
  `PluginName` `CollectAdapters` records for the contributor. Uniqueness across
  plugins is inherited from the engine's own rejection of duplicate plugin
  names; a duplicate *within* one plugin, from one Provider or across several it
  contributed, fails composition.
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
returns flat JSON in five arrays — plugins, resources, ports, commands and
subscriptions — addressed by type string, and writes a file instead when given
an absolute `.json` path. A port entry names the interface, the Port type,
whether it collects or requires Adapters, and the Adapter types bound to it in
plugin order.

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

[`specs/mcp.md`](specs/mcp.md) specifies the extension point, and
[`specs/broker.md`](specs/broker.md) the broker and its transport.
