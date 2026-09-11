# mcp

`github.com/dvoyni/cog/mcp` declares the agent-facing extension point: how a
plugin offers typed **capabilities** to an **agent**, and nothing about how
those capabilities reach one. It is a contract-only leaf in the same sense `app`
is — it imports `kernel` and the standard library, and the plugins that
implement it live elsewhere.

The broker that collects capabilities and serves them over the Model Context
Protocol is a different package, [`mcpserver`](../mcpserver/README.md). The
split is load-bearing: `mcp` must never learn protocol vocabulary, and nothing
importing `gfx` should acquire an HTTP server and a JSON-schema library in its
module graph.

## Dependencies

- Go package: `kernel`, standard library
- Plugin dependencies: none; this package declares no plugin.

## Files

`provider.go` declares the provider interface, `capability.go` the capability
value and its two constructors, `option.go` the construction options, `text.go`
the one optional interface a payload type may implement, `err.go` the errors,
and `doc.go` the package documentation, which carries the capability-body rule
in full.

## `Provider`

```go
type Provider interface {
    kernel.Plugin
    Capabilities() []Capability
}
```

One interface, not one per kind of capability, so adding a kind edits neither
this package nor the broker. The broker discovers providers with
`k.Plugins[mcp.Provider]()` at its own `Start` and calls `Capabilities()`
exactly once.

Capabilities are static for the engine lifetime. A provider with nothing to
offer right now says so **inside** its capability — an empty list, or an
`Unavailable` from a call — never by ceasing to satisfy the interface. Empty,
not absent.

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
construction failures.

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

## Specification

[`docs/specs/mcp.md`](docs/specs/mcp.md).
