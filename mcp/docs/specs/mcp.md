# mcp extension point — specification

`github.com/dvoyni/cog/mcp` is a contract-only leaf package, in the same sense
`app` is one: it declares types that *other* plugins implement, and imports
nothing but `kernel` and the standard library. It declares how a plugin offers
typed **capabilities** to an **agent**, and nothing about how those capabilities
reach one.

The broker that collects them and serves them over MCP is a different package,
`mcpserver`, specified in
[mcpserver/docs/specs/mcp.md](../../../mcpserver/docs/specs/mcp.md). The split
is load-bearing: `mcp` must never learn protocol vocabulary, and nothing that
imports `gfx` should acquire an HTTP server and a JSON-schema library in its
module graph.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

The one claim this family cannot make on its own evidence is stated up front, in
[What this design has not been shown to do](#what-this-design-has-not-been-shown-to-do).

---

## Contents

- [Vocabulary](#vocabulary) · [The two packages](#the-two-packages)
- [`Provider`](#provider) · [`Capability`](#capability)
- [The capability-body rule](#the-capability-body-rule)
- [Request and response types](#request-and-response-types)
- [Errors](#errors) · [Annotations](#annotations)
- [The write direction, and the absence of lifecycle](#the-write-direction-and-the-absence-of-lifecycle)
- [Arm-then-wait](#arm-then-wait) · [Pairing a moment](#pairing-a-moment)
- [The retained executioner](#the-retained-executioner)
- [Description house style](#description-house-style)
- [The capability set](#the-capability-set)
- [Required mcp changes](#required-mcp-changes) ·
  [Required kernel changes](#required-kernel-changes)
- [Out of scope](#out-of-scope) ·
  [What this design has not been shown to do](#what-this-design-has-not-been-shown-to-do)

---

## Vocabulary

These five words are used precisely throughout, and they are in `CONTEXT.md`
under **Agent Interface**. They were pinned in
[The provider contract, and where the mcp package sits](https://github.com/dvoyni/cog/issues/204)
before any rule was written, because the whole design is a statement about who
may say what.

- **Agent** — an external, LLM-driven client attached to a *running* engine. It
  observes through capabilities and reaches the game only through synthetic
  input.
- **Capability** — a named, described, typed unit of engine functionality a
  provider offers. It carries its own request and response types and is fixed
  for the engine lifetime.
- **Provider** — a plugin that offers capabilities. It speaks cog contracts
  only; it never emits protocol vocabulary.
- **Broker** — the single plugin that collects capabilities from every provider
  and serves them to an agent. It holds no knowledge of what any capability
  does.
- **Tool** — the protocol rendering of one capability: its name, JSON schema,
  description and annotations as an agent's client sees them. Written by the
  broker, never by a provider.

Two more are defined by the capabilities rather than by the contract, and are
used here only in passing: **Capture** (pixels off the GPU, written to a file
the agent names), **Snapshot** (one tick's recorded declarations, rendered while
they are still alive). **Burst** and **Synthetic input** likewise. All four are
in `CONTEXT.md`.

The word **contribution** is deliberately absent. It was reserved while
[May the broker write into a provider?](https://github.com/dvoyni/cog/issues/205)
ran, and that ticket concluded it names a category with one member and should
not exist — see
[The write direction](#the-write-direction-and-the-absence-of-lifecycle).

---

## The two packages

From
[The provider contract, and where the mcp package sits](https://github.com/dvoyni/cog/issues/204)
§4.

|                     | `mcp/`                          | `mcpserver/`                  |
| ------------------- | ------------------------------- | ----------------------------- |
| what it is          | contract leaf                   | broker plugin                 |
| imports             | `kernel`, stdlib                | `kernel`, `mcp`, the MCP SDK  |
| a provider writes   | `mcp.Capability`                | —                             |
| an app writes       | —                               | `mcpserver.New()`             |

`app` is the precedent the map picked, and it is exact: `app` is the contract,
named for what it is about, while `wgpu` — its single implementor — is named for
itself. Providers name `mcp` on every capability they add; an app names
`mcpserver` once, in its plugin list. The good name goes to the many.

One package was rejected. It would put the MCP SDK and
`github.com/google/jsonschema-go` in the module graph of anything importing
`gfx`, and would force one package doc to describe both an extension point and
an HTTP server.

**Every package hosts its own provider.** There are no `mcp`-only packages
beyond these two: `gfx`, `canvas`, `ui`, `input` and `wgpu` each implement
`mcp.Provider` themselves
([#205](https://github.com/dvoyni/cog/issues/205) §3). A separate plugin per
capability was rejected — it is buildable, but its only advantage was a finer
composition gate, and `mcpserver`'s own presence is already the gate. An app
that does not list `mcpserver.New()` has no agent interface at all.

---

## `Provider`

```go
package mcp

// Provider is a plugin that offers capabilities to an agent.
type Provider interface {
	kernel.Plugin
	Capabilities() []Capability
}
```

**One interface, not one per kind.** `PluginWithCapture`, `PluginWithSnapshot`
and `PluginWithInput` were rejected: they put the set of kinds inside the
broker, which is the knower this whole design exists to avoid; they close the
set at the type level, so adding a kind edits `mcp` and touches every provider;
and they would have killed the app-escape-hatch question before it could be
asked (see [Out of scope](#out-of-scope)).

Embedding `kernel.Plugin` gives `Name()`, which is what the broker namespaces
tool names with, without a second lookup.

**Discovery is `k.Plugins[mcp.Provider]()` at broker `Start`, cached.** That is
the one method
[What kernel exposes so one plugin can find another](https://github.com/dvoyni/cog/issues/200)
added for this — see [Required kernel changes](#required-kernel-changes). The
list is complete and final by then regardless of start order, because
`e.plugins` is set inside `WithPlugins`, long before the `Start` loop runs
(`kernel/engine.go:122`, `:223`).

**Capabilities are static for the engine lifetime.** A type assertion is static,
and that is fixed as the rule rather than tolerated as a limitation: a plugin
provides or it does not, for the whole engine lifetime. A provider with nothing
to offer *right now* says so **inside** its capability — an empty capability
list, or an `Unavailable` from a call — never by ceasing to satisfy the
interface. **Empty, not absent.**

Dynamic capability would make an interface assertion the wrong instrument and
would change every answer in this document. It is also the door to a plugin set
that changes at runtime, which cog does not have and whose absence is
load-bearing: `WithPlugins` validates the complete graph once and `finalize`
closes it (`kernel/engine.go:117`).

`Capabilities()` is called **exactly once**, during broker `Start`.

---

## `Capability`

From [#204](https://github.com/dvoyni/cog/issues/204) §§2–3.

A capability is a named, described, typed **dispatch**. Two constructors, and
only these two:

```go
// Command is the whole capability: dispatch this command with the agent's
// request.
func Command[TCmd kernel.CommandConstraint[TReq, TResp], TReq, TResp any](
	name, description string, opts ...Option,
) Capability

// Func is the escape for a capability that is more than one dispatch.
func Func[TReq, TResp any](
	name, description string,
	invoke func(kernel.Executioner, TReq) (TResp, error),
	opts ...Option,
) Capability
```

`Command` is the common case and carries **zero glue**: the provider already has
a typed command, and the capability is simply the statement that an agent may
dispatch it. It enforces
[the capability-body rule](#the-capability-body-rule) *by construction* — there
is nowhere to put code that would run on the broker's goroutine.

`Func` exists because some capabilities cannot be one dispatch. The original
case was capture, which must arm a flag and then wait for a frame; blocking
inside a command handler would hold the render lock against the very frame it
waits for. Five capabilities use `Func` today and each names its reason in its
own spec.

The erased value:

```go
type Capability struct {
	name, description string
	request, response reflect.Type
	invoke            func(kernel.Executioner, any) (any, error) // any holds *TRequest
	readOnly          bool
	err               error // deferred construction failure
}
```

with exported accessors for the broker. Four decisions inside it:

- **A struct with unexported fields, not an interface.** Only `Command` and
  `Func` may construct one, which makes the body rule *unforgeable*: no other
  package can implement `Capability` and slip work onto the broker's goroutine.
  An interface would hand that back.
- **`invoke` takes `any` holding `*TRequest`**, with the generic constructor
  closing over the assertion. The broker does `reflect.New(c.RequestType())` →
  unmarshal → validate → `Invoke`.
- **The response type is carried and an output schema is emitted.** This costs a
  rule — `TResponse` must be a struct, so a capability returning one string
  returns a one-field struct — and buys structured content the agent reads
  without parsing prose.
- **Construction failures defer into `err` rather than panicking.**
  `Capabilities()` returns a slice literal; there is nowhere to return an error.
  The broker collects the failures at `Start`, which is also what converts the
  SDK's own `AddTool` panic into an ordinary cog composition failure.

**`invoke` receives an `Executioner` already bound to the agent's request
context.** The broker does `k.WithContext(req.Context())` per call, so there is
no separate `context.Context` parameter; `k.Context()` carries it.
`Kernel.WithContext` sets `bounded = false` (`kernel/kernel.go:47`), after which
`ExecuteCommand` re-links engine cancellation with
`context.AfterFunc(engine.ctx, cancel)` (`kernel/kernel.go:154`). One handle
therefore carries two lifetimes with no bookkeeping: a dispatch dies on
**either** the agent hanging up (including the client's five-minute idle abort)
**or** engine shutdown.

Capability names are validated `^[a-z][a-z0-9_]*$` at construction. The broker
renders the tool name as `<plugin>_<capability>`; see
[The capability set](#the-capability-set).

---

## The capability-body rule

From [#204](https://github.com/dvoyni/cog/issues/204) §2, with one clarification
from
[What the agent learns about the engine's architecture](https://github.com/dvoyni/cog/issues/210)
§5.

> The capability body runs on the broker's goroutine and holds no locks. It may
> dispatch and it may wait. It may not touch provider state.

This is the central constraint on the whole shape. The broker calls a capability
from an HTTP goroutine while the render thread is mid-frame, and in cog **a
value read from a handle is valid only while the handler holds its lock**
(`CONTEXT.md`, *Resource handle*). So the body must be a *dispatch*, never an
access. That is precisely what an `Executioner` permits and nothing more, which
is why the constraint is expressible in the type the body receives.

One narrow exception, and it is stated explicitly because an unstated exception
is how a rule like this erodes:

> Engine-immutable data finalized before `Run` may be read directly from the
> `Executioner`. `Describe` is the only such source. Everything else goes
> through a dispatch.

`Executioner.Describe` reads registry state that is immutable after `finalize`
and returns a detached value; it reads no handle, needs no lock, no tick and no
scheduler. `mcpserver_architecture` is the only capability that resolves without
dispatching anything ([#210](https://github.com/dvoyni/cog/issues/210) §5).

There is **no handoff mechanism** anywhere in this design, and a reader expecting
one should stop looking. `scheduler.execute` acquires a task's locks from the
coordinator and then runs the body **on the calling goroutine**
(`kernel/scheduler.go:191-219`), and the coordinator is its own goroutine,
independent of the game loop. An agent's dispatch therefore runs on the HTTP
goroutine, serialized against the game only by the locks it declares. No queue,
no next-tick deferral, no thread affinity
([The transport](https://github.com/dvoyni/cog/issues/206) §3).

---

## Request and response types

Both `TRequest` and `TResponse` must be **structs**: the JSON schema root has to
be `object`, so a one-value answer is a one-field struct. The broker terminates
the engine at `Start` if a request type's schema root is not `object`
([#204](https://github.com/dvoyni/cog/issues/204) §6).

**The schema is the broker's, inferred from the Go type.** A provider never
writes a schema, and this is what makes tool-definition drift structural rather
than a promise:
[research: the Go MCP landscape](https://github.com/dvoyni/cog/issues/201) found
that the official SDK's `AddTool[In, Out]` infers a tool's JSON schema from a Go
type by reflection, reading property descriptions from a `jsonschema:"…"` struct
tag. A capability's request struct *is* the tool's input schema.
[research: what makes an agent-facing tool surface usable](https://github.com/dvoyni/cog/issues/202)
found two mature DCC servers arguing in opposite directions about whether narrow
typed wrappers drift from what they wrap; cog has taken the side that says they
must not, and inherited the obligation.

### Types that cross as text

**Reflection infers from the Go *kind*, and a type that marshals as text breaks
that.** `input.Key` is an integer with `MarshalText`, so it marshals as a JSON
string but infers as `{"type": "integer"}` — a schema that describes nothing the
capability accepts. This was flagged to the assembly by
[input synthesis: what the agent may press](https://github.com/dvoyni/cog/issues/209)
§9 and is answered here, because it is the one place a cog type and its schema
can silently disagree.

`jsonschema-go` consults `encoding.TextMarshaler` only for **map keys**
(`jsonschema/infer.go:203` at v0.4.3); a named integer used as a value is
rendered by its kind. The override hook exists —
`jsonschema.ForOptions.TypeSchemas map[reflect.Type]*Schema` — but the broker
cannot populate it, because it would have to name `input.Key`, which is the
knower again.

So `mcp` declares one optional interface, and it is the only thing this document
adds to the contract that four consecutive tickets did not:

```go
// TextValued is implemented by a type that crosses the wire as a JSON string
// rather than as whatever its Go kind implies. Values lists the strings it
// accepts. Other, when non-empty, is a regular expression admitting strings
// outside that list; it exists for a type that can legitimately produce a
// value the list does not name.
type TextValued interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
	TextValues() (values []string, other string)
}
```

The broker walks each request and response type once at `Start`, collects every
type implementing `TextValued`, and builds the `TypeSchemas` map from them:
`{"type": "string", "enum": […]}`, or an `anyOf` of that and
`{"type": "string", "pattern": other}` when `other` is non-empty. Nothing here
is protocol vocabulary — a type stating which strings it accepts is a fact about
the type — and `mcp` gains an `encoding` import and nothing else.

`input.Key` is the only implementor today, returning its name table and
`^#-?[0-9]+$`; see
[input/docs/specs/mcp.md](../../../input/docs/specs/mcp.md).

*Rejected: the broker silently rendering every `TextMarshaler` as a bare
`{"type": "string"}`.* It adds nothing to `mcp` at all, and it is the cheaper
answer if this ever needs simplifying — but it throws away the enum, which is
what makes a mistyped key fail in the client rather than reaching the engine and
coming back as an `Unavailable`. For a field whose whole hazard is an agent's
wrong prior about key codes, client-side rejection is worth one interface.

*Rejected: a schema override passed through `mcp.Option`.* It would put
`jsonschema.Schema` in the contract leaf, undoing
[The two packages](#the-two-packages).

**Gap.** The `anyOf` shape has not been round-tripped against a real client;
only the SDK's inference behaviour has been read off source at
`jsonschema-go@v0.4.3` and `go-sdk@v1.6.0`. What would settle it is the first
implementation issue calling `input_send` from Claude Code with a bad key name
and observing whether the rejection is client-side.

### Delivery: `path`

From
[The capture request: flag, next frame, and what comes back](https://github.com/dvoyni/cog/issues/207)
§3. Every capability that produces a document follows one delivery contract, and
it is the same field in every one of them:

- **`path` is required for an image and optional for a structured dump.** Omit
  it on a dump and the content comes back inline; supply it and a file is
  written and the path returned.
- **The path is absolute**, and its extension must match what will be written. A
  relative path would silently resolve against the *game's* working directory,
  which need not be the agent's.
- **Parent directories are created.** An existing file is **overwritten without
  complaint** — re-writing the same name is the iterate-and-look loop.
- **Every check happens before anything is armed**, so a typo costs
  microseconds rather than frames.

An image has no choice in the matter: [#201](https://github.com/dvoyni/cog/issues/201)
established that inline image content has no escape hatch —
`MAX_MCP_OUTPUT_TOKENS` defaults to 25,000, the `anthropic/maxResultSizeChars`
annotation "does not apply to images", and the automatic spill-to-file is
documented only for results with **no** image content. A dump has a choice
because the inverse holds: an oversized *text* result is spilled to a file
automatically, under a name the agent did not choose. A large dump becomes a
file either way; the only question is whether cog controls it. And a file is
**greppable**, which is
[#202](https://github.com/dvoyni/cog/issues/202)'s *reference over value* rule
expressed as a parameter the agent controls rather than as a size heuristic
nobody can tune.

**cog needs no writable directory of its own, and has none by design.** The
agent names every path it wants written. This deletes a capture directory, a
retention rule, a numbering scheme and a startup wipe, and it is why
`mcpserver.Config` has no output directory
([#206](https://github.com/dvoyni/cog/issues/206) §8) and `gfx` has no config at
all ([#207](https://github.com/dvoyni/cog/issues/207) §2). It also means
`storage` is not in this picture anywhere: `storage.PermanentFS` exposes no OS
path, its default location is outside the agent's working directory, and on
js/wasm it is `localStorage`, where a path means nothing
([#206](https://github.com/dvoyni/cog/issues/206) §9).

---

## Errors

From [#204](https://github.com/dvoyni/cog/issues/204) §9.

```go
// Unavailable reports that a capability cannot run right now, in words meant
// for an agent to read and act on. It is an expected outcome, not a fault.
type Unavailable struct{ Reason string }
```

The broker renders an `Unavailable` as an ordinary tool result with `IsError`
set and `Reason` as the text; the agent reads it and picks something else. This
mirrors the protocol, which
[#201](https://github.com/dvoyni/cog/issues/201) confirmed already answers the
question: tool-level failure is `isError` inside a normal result, not a JSON-RPC
error, so the model can read it and correct itself.

**Any other error is a system fault** in cog's existing sense — reported through
`kernel.ReportError` and returned to the agent as a generic failure, without
leaking internals into the model's context. Normal refusal and a broken engine
end up on opposite sides of a type boundary rather than inside a string.

Two classes are named as `Unavailable` rather than faults, and both matter
because the alternative is an engine that terminates while exiting normally:

- **Shutdown.** `context.Canceled` and `ErrSchedulerStopped{}` are
  `Unavailable{Reason: "the game is shutting down"}`. A game exiting is the
  normal case ([#206](https://github.com/dvoyni/cog/issues/206) §4).
- **Busy, refused, and not-right-now.** `ErrCaptureBusy{}`,
  `ErrCaptureAbandoned{}`, `ErrCaptureUnsupported{Format}`, a second snapshot of
  the same kind, an over-cap input sequence, a bad path. All are words.

Every `Unavailable` reason should name the likely cause, not just the condition.
`"no frame was rendered within 2s — the game may be paused, minimised, or not
rendering"` is the model the others follow.

---

## Annotations

From [#204](https://github.com/dvoyni/cog/issues/204) §11, as amended by
[#207](https://github.com/dvoyni/cog/issues/207) §11.

One option, on both constructors:

```go
mcp.Command[CaptureCmd, CaptureRequest, CaptureResponse]("capture", "…", mcp.ReadOnly())
```

"This capability changes nothing" is a fact about a cog command, not protocol
vocabulary, so a provider may state it and the broker translates. The mapping:

| MCP hint          | cog                                        |
| ----------------- | ------------------------------------------ |
| `ReadOnlyHint`    | `readOnly`                                 |
| `DestructiveHint` | **always false** — its own statement, never `!readOnly` |
| `OpenWorldHint`   | always false — a running game is a closed world |
| `IdempotentHint`  | left alone                                 |

**`ReadOnly` means this capability does not change the game.** That is cog's
reading and the spec states it outright, because it is looser than the hint's
own wording. A capture writes exactly one file it was told to write; under the
original mapping, dropping `ReadOnly()` to be pedantic about "does not modify
its environment" would have marked a screenshot *destructive*, a plainly bigger
falsehood, and would have put an approval prompt on the single most-looped
capability in the design. The question every client actually uses the hint for is
*safe to auto-approve?*, and for an agent that already holds an unrestricted
write tool the answer is yes.

The option list is variadic so later options do not break every provider.

---

## The write direction, and the absence of lifecycle

From [May the broker write into a provider?](https://github.com/dvoyni/cog/issues/205),
as amended by
[What a debug overlay is: what ui offers an agent](https://github.com/dvoyni/cog/issues/231).

**The extension point is two-way, and the second direction needs nothing built.**
A capability whose command mutates provider-held state is an ordinary
capability: request in, response out, dispatched through the provider's own
command. Nothing is added to `Capability`, to `mcp`, or to the broker.

What the agent may set today, all of it free:

- **`app.SetDesiredViewportCmd`** — declared in `app`, handled by `gfx`
  (`app/commands.go:31`, `gfx/plugin.go:65`), available as
  `mcp.Command[app.SetDesiredViewportCmd, …]` with **zero new code**. An agent
  reproducing a layout bug at a named aspect ratio is a standing mutation that
  already exists.
- **A synthetic key held down** across frames, which persists in `input.State`
  until a matching release
  ([#209](https://github.com/dvoyni/cog/issues/209)).
- **Paused-ness**, which the driver holds until something resumes it
  ([#211](https://github.com/dvoyni/cog/issues/211)).

`#205` named a second tier — *content the agent authors* — whose only member was
a ui debug overlay. `#231` ruled that out of scope: the agent says in chat
everything it would have drawn, to the human who is already reading it. **The
tier is empty and is not reintroduced for one member.** The structural claim is
unaffected, because it was never a claim about content: the extension point is
two-way from day one, with no retrofit, and the retrofit is what the claim was
protecting against.

### The contract has no lifecycle

**Nothing is undone when an agent's session ends.** A standing mutation persists
until it is replaced, until a capability clears it, or until the process exits.

This is the loudest rule in the document, because it forbids a class of
machinery rather than adding one. It rules out: a session-teardown hook on the
broker, session-scoped provider state, a "standing" flag on `Capability`, and
any notion of the broker undoing what a provider was told to do.

The SDK makes the alternative *implementable*, which is why it is ruled out
rather than merely unmentioned — `Server.Sessions()` is public,
`ServerRequest.Session` reaches every handler, and `ServerOptions.KeepAlive`
closes sessions whose peer stops answering pings. Three reasons it loses:

1. It would drag standing-ness back into the contract, which is deliberately
   invisible to it. A capability body receives `(kernel.Executioner, TReq)` and
   no session identity; giving it one is the knower arriving by a side door.
2. It is not even prompt. Streamable HTTP has no disconnect signal; a crashed
   agent's session lingers until the ping interval elapses, and only if
   `KeepAlive` was configured at all. Cleanup would fire minutes late, or never.
3. It would erase the agent's mark at exactly the moment the agent hands a
   running game to a human.

The transport enforces it mechanically as well as by rule: `Stateless = true`
means there is no session identity to reach for
([#206](https://github.com/dvoyni/cog/issues/206) §6).

**The consequence to state in every provider spec that has standing state:** a
held synthetic key and a paused engine both survive a disconnect, and nothing
will clear them. `input_state` and `wgpu_time status` exist so an agent can find
them; `input_send`'s 10s cap and `wgpu_time step`'s 600 cap exist so the window
in which one can be *created and then orphaned* is bounded.

---

## Arm-then-wait

From [#207](https://github.com/dvoyni/cog/issues/207) §12, confirmed by
[#208](https://github.com/dvoyni/cog/issues/208) and
[#211](https://github.com/dvoyni/cog/issues/211), extended by
[#229](https://github.com/dvoyni/cog/issues/229) §6.

Several capabilities must bind to a frame: a capture needs a render, and a
snapshot needs a tick, and neither can be served by reading something *now*.
What they share is a **discipline, not a type**. No shared struct is added to
`mcp`, which must never learn that an engine has frames; five capabilities cite
this section and none restates it.

1. **Validate first.** Anything checkable without the engine is checked before
   arming, so a bad request costs no frames.
2. **Arm by dispatch.** The request becomes a command, and the command's
   response carries a **buffered** receive-only channel plus whatever state the
   body will need but cannot read.
3. **Bind to a recorded tick**, so the result reflects everything the agent did
   beforehand.
4. **Wait with an own deadline**, below the broker's 30s and the client's five
   minutes, and fail with a reason that names the likely cause.
5. **Refuse a second in words.** One of a kind in flight; `mcp.Unavailable`,
   never a fault, never a queue.
6. **Do the expensive part on the broker's goroutine** — un-striding, encoding,
   serializing, writing. The engine's threads hand over bytes and nothing more.
7. **Deliver by path when the agent gives one.**

Rule 3 is what makes the whole design compose without a composition mechanism:

> A capture or a snapshot shows the game as of a tick that began after the
> request was made.

An agent that presses a key and then captures **cannot** get the pre-key frame,
so *"the frame after I press this key"* is not a feature either side has to
build. It costs one extra tick, about 16 ms at 60 Hz, and it dissolves a
question rather than answering it.

Rule 5's "of a kind" is exact: `canvas_draws`, `ui_layout`, `gfx_frame` and
`gfx_capture` may all be in flight **together** — they are separate flags filled
during one tick, and refusing them would destroy the pairing below. A second
capture, or a second snapshot of the *same* kind, is refused.

**Not every capability is frame-bound, and that is what keeps this section
meaning something.** `input_send`, `input_state` and `mcpserver_architecture`
bind to no frame at all; `wgpu_time` binds to one for `step` and to none for
`pause`, `resume` and `status`.

### Under pause

Pause splits the frame-bound capabilities on a fact about the data, not on a
preference: **a capture needs a render, a snapshot needs a tick.** Renders
continue while paused; ticks do not; and canvas's and ui's queues are *empty*
between ticks rather than stale (`defer write.reset()`, `defer frame.clear()`).

- **A capture under pause costs no tick** and is served from the next render.
  Rule 3 is satisfied vacuously rather than weakened: a paused engine has no
  staleness, because no tick can begin at all, so the last completed tick *is*
  the present. Two captures under one pause are byte-identical.
- **A snapshot under pause performs exactly one step, or joins one already
  pending, and says so in its response.** There is no honest way to produce one
  without running a tick, and refusing would make snapshots unreachable under
  pause, since a blocking arm cannot ask the agent to step for it.
- **Every snapshot names the tick it describes.** *Stepped* and *joined* say
  what this call did; `tick` says which moment the document is of, which is
  the only thing two snapshots can be compared on. See
  [Pairing a moment](#pairing-a-moment).

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).** The third bullet is new, and
> the second one's "or joins one already pending" is weaker than it reads: the
> join only reaches a step that is still pending, which the next drawn frame
> ends. `wgpu_time hold` is what holds it open.

---

## Pairing a moment

From [#229](https://github.com/dvoyni/cog/issues/229) §§4–6. This is the recipe
every frame-bound capability's description points at, and it is the whole reason
[Batching round trips](https://github.com/dvoyni/cog/issues/229) declined to add
a batch capability.

```
wgpu_time pause
wgpu_time hold                         (the step window stops belonging to the frame clock)
canvas_draws + ui_layout + gfx_frame   (parallel arms, coalesced onto ONE step -> tick N+1)
wgpu_time release                      (or let the hold expire)
                                       -> all three report tick: N+1, or they did not pair
gfx_capture                            (no tick; the frozen frame IS N+1)
```

**Capture goes last, always.** It costs no tick, so it shows whatever the last
step produced and can never be the thing that decides the moment. Armed first,
it resolves against the *current* frozen frame and straddles two ticks, with
neither response saying so. It is also why the capture needs no tick number of
its own: under pause it photographs whatever the shared step produced.

The middle line rests on one driver rule: **arming a snapshot while a step is
pending joins that step rather than requesting another**. Read per-arm, three
concurrent arms would be three steps on three different ticks — the precise
opposite of what arming them together is for. The rule is a `wgpu` tick-source
behaviour before it is an agent-facing one; see
[wgpu/docs/specs/mcp.md](../../../wgpu/docs/specs/mcp.md).

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).**
> The recipe shipped without the hold and the check, and **it did not describe
> one tick**. Driven against **feuds-26** on a real window and GPU over
> streamable HTTP, three concurrent arms advanced the engine **two ticks,
> eleven attempts out of eleven**; two arms paired four times in five. The
> join rule is real but *opportunistic*: the window is only as wide as the gap
> before the next drawn frame consumes the batch, and three calls over three
> connections do not reliably fit inside one.
>
> Two lines are therefore new. **`wgpu_time hold`** stops a frame from
> consuming the step until `release` or until the hold's own deadline, so the
> window belongs to the agent; and **every snapshot now reports the `tick` it
> describes**, so the agent confirms the pairing from the responses instead of
> trusting timing. The non-guarantee below is reworded to match: what "under
> pause they provably do" was asserting is true only under a hold, and it is
> now checkable either way rather than provable by argument.

And the non-guarantee, stated in the same voice as every other one in this
family:

> Arms issued in parallel usually describe one tick, and may not. Under pause
> **and a hold** they do, and every response names the tick it describes, so
> an agent never has to take it on trust.

Two arms issued as parallel tool calls arrive within a millisecond of each other
and will usually land inside one ~16 ms inter-tick gap. But that depends on the
client choosing to parallelise, which no spec requires and cog cannot — and,
measured, three arms over three connections usually do not. Silence is what
made this the failure mode that mattered: an agent comparing a capture against
a snapshot it believes is simultaneous would misread one tick of drift as a bug
in the game, and had no field to check. The `tick` field is the end of the
silence; the hold is the end of the drift.

---

## The retained executioner

From [#200](https://github.com/dvoyni/cog/issues/200) §3. This is a **named,
local exception** to a `kernel` rule that remains in force everywhere else, and
it is written here rather than in `kernel/README.md` for exactly that reason.

`kernel`'s rule stands as written: a handle is scoped to the dispatch that
received it, and retaining it is a bug. **`mcpserver` retains the `Executioner`
it was handed at `Start`**, past the return of `Start`, because reaching a
provider correctly means dispatching a command and `ExecuteCommand` is a method
on an `Executioner` value (`kernel/kernel.go:131`) — there is no package-level
dispatch function.

What makes the exception safe is mechanical rather than a promise:

- The `Start` executioner is a **root** executioner —
  `Executioner{Kernel{engine: e, ctx: e.ctx, scope: e.ctx, bounded: true}}`
  (`kernel/engine.go:281`). It holds **no locks**, so every dispatch through it
  acquires its own lock set exactly as a lifecycle dispatch does.
- It is an immutable value, so concurrent use from many HTTP goroutines is safe.
- **It carries its own expiry.** It is bound to `e.ctx`, and `Run` calls
  `e.cancel(nil)` *before* the shutdown executioner is built and the reverse-order
  `Stop` loop begins (`kernel/engine.go:247-250`). Engine cancellation therefore
  fires strictly before any provider's `Stop`, and every dispatch attempted
  after that point fails on a cancelled context rather than reaching a stopped
  plugin.

**`wgpu` is not precedent for this.** `wgpu.Plugin.Run` captures its
`Executioner` into the gogpu callbacks (`wgpu/plugin.go:109-114`), but `Run`
never returns until shutdown: the handler that received the handle is still on
the stack the whole time it is used. The broker's `Start` returns immediately
and the handle outlives it. Different move, correctly treated differently.

The obligations this places on the broker are in
[mcpserver/docs/specs/mcp.md](../../../mcpserver/docs/specs/mcp.md).

---

## Description house style

From [#204](https://github.com/dvoyni/cog/issues/204) §8, which resolved a
contradiction rather than picking a preference. Two settled notes on the map
could not both be literally true: *a broker, not a knower* and *descriptions
want writing once, by someone thinking about agents*. If the broker holds the
description strings, it knows every capability by name, and the extension point
is not an extension point.

**Resolved in favour of the provider.** Writing a sentence about when an agent
should call something is not speaking MCP — it is a doc comment. What a provider
must never emit is a JSON schema, a tool name, or an annotation.

The original concern is answered differently rather than dropped: this section
is the house style, and **each `<package>/docs/specs/mcp.md` reproduces its own
descriptions in full**, so they are reviewed as prompt text rather than buried
as string literals in code.

A description says, in this order:

1. **What it does, in the agent's terms** — "take a screenshot of what the game
   is drawing", not "arm the capture flag".
2. **What it returns**, including the shape, so the agent can plan the next call
   without making it.
3. **What it costs** — blocks about 50 ms, performs one step, does nothing while
   paused. Anything that surprises an agent mid-loop.
4. **The follow-up call**, explicitly, when there is an obvious one. "Divide one
   by the other to turn a point you can see into a point you can click."
5. **The trap**, concretely and by worked example, when a plausible prior is
   wrong. `#13` is `M`, not Enter — a generic "prefer names" loses to every
   ASCII and keyCode prior an agent has, and only the counter-example defuses
   it.

What a description never says: how the engine implements it, what a resource is
called, or anything an agent cannot act on.
[#202](https://github.com/dvoyni/cog/issues/202) found Epic stating the
principle — descriptions "should be written with the same care as the public
surface of any other API" — and found that **nothing upstream decides this**:
the MCP project defines `Tool.description` in one clause and has never advised
how to write one.

Two findings that shape what descriptions must *not* do:

- **Never truncate silently.** Blender's `get_scene_info` stops at ten objects
  with no cursor and no flag, and the agent cannot tell. Whatever a cog
  capability omits, it says it omitted it and offers a way to reach the rest —
  which is why a burst reports `Indices` and a snapshot reports its filter.
- **Prune the view without shrinking the action surface.** An elided item stays
  addressable. In cog that falls out of source indices and type strings rather
  than needing a handle scheme; see
  [#208](https://github.com/dvoyni/cog/issues/208) §6.

---

## The capability set

Fixed at `Start`, in `Plugins[T]` registration order, which is dependency order —
so `tools/list` is deterministic, and that matters because it is the order the
agent reads them in.

| tool | provider | shape | frame-bound | read-only |
| --- | --- | --- | --- | --- |
| `gfx_capture` | `gfx` | `Func` | render (ticks unless paused) | yes |
| `gfx_frame` | `gfx` | `Func` | tick | yes |
| `canvas_draws` | `canvas` | `Func` | tick | yes |
| `ui_layout` | `ui` | `Func` | tick | yes |
| `input_send` | `input` | `Func` | no | no |
| `input_state` | `input` | `Command` | no | yes |
| `wgpu_time` | `wgpu` | `Func` | `step` only | no |
| `mcpserver_architecture` | `mcpserver` | `Func` | no | yes |

`wgpu_time`'s row read *per action* until
[#250](https://github.com/dvoyni/cog/issues/250) implemented it. An annotation
is per tool, not per argument, and three of that tool's four actions change the
game, so the whole tool is not read-only; `status` says it only reports in its
description. Per-action approval annotation is out of scope for this effort —
it would need vocabulary here and in the broker, which
[#211](https://github.com/dvoyni/cog/issues/211) §12 rules out.

> **Amended at implementation ([#259](https://github.com/dvoyni/cog/issues/259)).** `wgpu_time` has six actions
> rather than four: `hold` and `release` join it so that several snapshots can
> be made to describe one tick — see
> [Pairing a moment](#pairing-a-moment). The row is unchanged in every column.
> The annotation question in particular does not reopen: a capability is
> `mcp.ReadOnly()` or it is not, `hold` and `release` change nothing about
> that, and the table still has eight tools in it. **The broker learned
> nothing**, which was the constraint the fix had to respect.

Eight tools, seven of them one per question an agent actually asks. The set is
small on purpose:
[#202](https://github.com/dvoyni/cog/issues/202) found granularity has no norm
in this space — the two official servers sit at opposite ends, 6 tools against
34, and Epic ships ~1,091 behind three discovery meta-tools — so the count is a
design choice rather than a convention to follow, and cog chose one tool per
question.

`<plugin>_<capability>` is the naming rule, with underscore rather than dot
because the MCP name charset is conservative and `_` is the one separator no
client rejects. Plugin names are already bare lowercase words, so nothing needs
mangling, and **uniqueness is inherited rather than re-enforced**: the engine
already rejects duplicate plugin names (`ErrConflictingPluginName`,
`kernel/engine.go:77`), so a collision across providers cannot happen. A
duplicate *within* a provider is a construction failure.

The rule survives one case that looks like an exception and is not.
`wgpu_time`'s subject is the engine's tick source, not the driver — but `app`
has no plugin, so `app` cannot provide, and `wgpu` can. **Pausing is a property
of the host that owns the loop**, and a different host genuinely is a different
thing with a different answer: it would offer `sdl_time`. Amending the rule so a
provider declares its own prefix was rejected — the rule's whole value is that
the prefix is unforgeable and unique by construction
([#211](https://github.com/dvoyni/cog/issues/211) §7).

`mcpserver_architecture` is the same shape read the other way: the prefix names
who offers it, the description names what it is about.

---

## Required mcp changes

A checklist for an implementation session. `mcp/` does not exist yet; this is
the whole package.

**`mcp/provider.go`**

- `Provider interface { kernel.Plugin; Capabilities() []Capability }`.

**`mcp/capability.go`**

- `Capability` struct with unexported fields, plus exported accessors: `Name`,
  `Description`, `RequestType`, `ResponseType`, `ReadOnly`, `Err`, `Invoke`.
- `Command[TCmd, TReq, TResp]` and `Func[TReq, TResp]`, both variadic in
  `Option`.
- Name validation `^[a-z][a-z0-9_]*$`, and non-struct `TReq`/`TResp` detection,
  both deferring into `err`.

**`mcp/option.go`**

- `Option`, and `ReadOnly()`.

**`mcp/error.go`**

- `Unavailable{Reason string}` with `Error() string`.

**`mcp/text.go`**

- `TextValued`, per
  [Types that cross as text](#types-that-cross-as-text).

**`mcp/doc.go`**

- The package doc carries
  [the capability-body rule](#the-capability-body-rule) verbatim and points at
  this document. It is the one rule a provider author must read before writing a
  `Func`.

**Tests**

- A `Command` built over a command whose request type is not a struct produces a
  deferred `err` rather than panicking.
- `Invoke` passes `*TRequest` through to the underlying dispatch unchanged.
- A `TextValued` implementor is detected through a slice field and through a
  nested struct field, not only at the top level — `input.Action` reaches `Key`
  through `[]Action`.

**`CONTEXT.md`** — already applied. **Agent Interface** carries **Agent**,
**Capability**, **Provider**, **Broker**, **Tool**, **Capture**, **Burst**,
**Readback**, **Snapshot** and **Synthetic input**; **Tick source** and
**Architecture description** are under Runtime Architecture. Nothing further is
added by this document: *contribution* was ruled out, *Overlay* was never
defined, and the arm-then-wait discipline is a section heading rather than a
term — the right weight for a pattern with five members and no independent
existence.

---

## Required kernel changes

Two methods, and nothing else about the engine's encapsulation is relaxed. From
[#200](https://github.com/dvoyni/cog/issues/200) §§1–2 and
[#210](https://github.com/dvoyni/cog/issues/210) §2. These belong in
`kernel/README.md` and its public API index, not here; they are listed because
this family does not compile without them.

```go
// Plugins returns every registered plugin that satisfies T, in registration
// order.
func (e Executioner) Plugins[T any]() []T

// Describe returns the finalized architecture. An Executioner exists only once
// Run begins, so the description it returns is always the final one.
func (e Executioner) Describe() ArchitectureDescription
```

- `Plugins[T]` **subsumes the engine's three private lookups** — `PluginHost`
  (`kernel/engine.go:103`), `PluginStarter` (`:226`), `PluginStopper` (`:253`) —
  which are reimplemented over one unexported `pluginsOf[T]` helper. The three
  public contracts are unchanged, and `PluginHost` keeps its zero-or-one
  composition rule.
- Both are **phase-gated for free**: an `Executioner` is only minted once `Run`
  begins (`kernel/engine.go:223`), so neither is reachable from `Register`.
- `CommandDescription` and `SubscriptionDescription` gain `Reads`, `Writes` and
  `Uses` — the **post-`absorb`** transitive sets, sorted by `compareTypes` like
  everything else in `Describe`. This is the one fact in the description that
  exists nowhere else, because a handler deliberately never names the resources
  behind a command it uses.
- `Dump` grows the same columns, and stops being dead code the moment
  `Executioner.Describe` gives it a caller.

**The encapsulation cost, stated plainly:** `Plugins[kernel.Plugin]()` returns
everything. It is deliberately not policed — a runtime panic on `T == Plugin`
would be theatre against a caller who could write the assertion loop by hand.
What the engine gives up is the property that a plugin reaches another plugin
only through a typed command, a published event or a locked resource. It gives
that up knowingly, once, in exchange for making available a lookup it already
performs privately three times.

`kernel` does **not** spend the word *capability*. It lists plugins; the type
parameter filters them. The two senses — "any interface a plugin might satisfy"
and "a typed thing an agent can invoke" — are different concepts, and naming
both the same would make the glossary worse.

---

## Out of scope

Recorded so nobody reopens them believing they were overlooked. The map carries
the full list; these are the ones that bear on the contract.

- **Arbitrary command dispatch by type name.** The agent reaches the game through
  the input seam or not at all. `mcpserver_architecture` ships the command list
  anyway, and **the door stays shut because no tool takes a command name**, not
  because the catalogue is hidden — `ExecuteCommand` is generic over the command
  type, not over a `reflect.Type`, so there is no mechanism to add without
  adding one. Hiding the list would be theatre against an agent that can read
  `gfx/plugin.go` ([#210](https://github.com/dvoyni/cog/issues/210) §8).

- **A generic batch capability.** Declined by
  [#229](https://github.com/dvoyni/cog/issues/229), and the rule that closes the
  door is stated rather than the verdict alone: **the broker provides a
  capability of its own only for facts about composition.** Anything that
  *invokes* another provider's capability is the knower, whatever it is called.
  `mcpserver_architecture` is a closed set of one, not the first of a series.
  Note that `Batch` is also taken in `CONTEXT.md` — *one run of recorded work
  merged into a single draw call* — which is worth recording because it is the
  word every prior-art server uses.

- **A ui debug overlay.** Closed by
  [#231](https://github.com/dvoyni/cog/issues/231): the agent says in chat
  everything it would have drawn. Beyond the cost, an overlay lands in every
  Capture and Snapshot taken afterwards, and `gfx` cannot suppress it because it
  knows nothing of `ui` — a debug instrument that corrupts the other
  instruments.

- **Session identity, teardown and reconnection state.** There is no session,
  in the contract or in the transport.

- **Authentication and shipped-build exposure.** Composition is the on/off
  switch. The moment someone wants this in a shipped build it becomes an auth
  effort with a different destination.

- **A stable public MCP API for third-party cog apps.** A consequence of doing
  this well, not a destination.

One thing that is emphatically **in** scope and looks like it should not be: **a
game's own plugin may be a provider.** It implements `mcp.Provider` and returns
`mcp.Command[BattleStateCmd, …]("battle_state", "…")`. The broker never knows
the difference and no mechanism is added. This is what makes the design an
extension point rather than a fixed integration, and it is the reason
`mcpserver` declares no plugin dependencies.

---

## What this design has not been shown to do

Stated here rather than buried, because the map's Notes require it and because
every spec in this family inherits it.

**Nobody has watched an agent use any of this.** The specs are written from
prior art, and
[#202](https://github.com/dvoyni/cog/issues/202)'s honest headline is that every
quote in that survey is a vendor describing what it does — no post-mortem, no
observed failure, no before-and-after from a server that changed its surface and
measured. **No vendor anywhere publishes tool-selection failure rates**, and
both Anthropic and Microsoft answer the central question with *evaluate it on
your own task*.

So **the first implementation issue owns finding out whether an agent picks the
right capability from the descriptions written here**, and the descriptions are
the part of this family most likely to be wrong. They are reproduced in full in
each provider spec so that they can be revised as prompt text, by someone
reading an actual transcript, without touching a design decision.

Two smaller things no one in-repo can validate:

- **`input_send`'s typing and scrolling steps have no consumer in cog.** Nothing
  reads `KeyEvent`, `TextEvent` or `State.Text()`, and nothing consumes
  `ScrollChange`. They are specified from the driver's own behaviour and cannot
  be exercised end to end until a game uses them.
- **The `no-prototype` decision was reaffirmed against this evidence**, not in
  ignorance of it. The seams are all located and the risk was judged to be
  design rather than feasibility. The counter-evidence is recorded on the map.
