# mcpserver broker — specification

`github.com/dvoyni/cog/mcpserver` is the one plugin that collects capabilities
from every provider in the engine and serves them to an agent over MCP. It
imports `kernel`, `mcp`, the official Go MCP SDK and a JSON-schema library, and
it imports **no provider** — not `gfx`, not `canvas`, not `ui`, not `input`, not
`wgpu`.

That absence is the design. The broker renders; it does not know. Everything it
can say about a capability it learned from an `mcp.Capability` value, and there
is no line in it that names a capability, a package, or a kind of thing an agent
might want.

This document specifies the broker and the transport. The extension point it
serves is
[mcp/docs/specs/mcp.md](../../../mcp/docs/specs/mcp.md), which is
transport-independent — and this document is the proof of that, because almost
nothing in it reaches back.

It is assembled from the resolved tickets of
[An agent-facing extension point across cog](https://github.com/dvoyni/cog/issues/199);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

---

## Contents

- [What the plugin is](#what-the-plugin-is) · [Config](#config)
- [The transport position](#the-transport-position) · [The address](#the-address)
- [Lifecycle](#lifecycle) · [Failing to bind](#failing-to-bind)
- [Rendering a capability](#rendering-a-capability) ·
  [Invoking one](#invoking-one)
- [Concurrency](#concurrency) · [What pause does not reach](#what-pause-does-not-reach)
- [`mcpserver_architecture`](#mcpserver_architecture)
- [The rule that closes the door](#the-rule-that-closes-the-door)
- [Attaching: what a person does once](#attaching-what-a-person-does-once)
- [Required mcpserver changes](#required-mcpserver-changes) ·
  [Out of scope](#out-of-scope)

---

## What the plugin is

```go
package mcpserver

func New(cfg ...Config) kernel.Plugin
```

An ordinary plugin with **no dependencies**, one `Start`, one `Stop`, and one
capability of its own.

**It declares no plugin dependencies, and that is what makes this an extension
point.** `Executioner.ExecuteCommand` performs no declaration check — an
unregistered command returns `ErrExecutingUnknownCommand` as an ordinary value
(`kernel/kernel.go:147`) — and the broker holds no locks, so `checkCoupling`
never engages. Declaring dependencies would do two bad things: make the broker
name every provider it might ever serve, which is the knower at composition
level; and force every app listing `mcpserver.New()` to also list all five
providers, because `Dependencies()` is a flat *required* list and composition
fails on any missing one (`kernel/engine.go:86`). With no dependencies, an app
composes exactly the providers it has and the broker serves exactly what it
finds ([#204](https://github.com/dvoyni/cog/issues/204) §5).

**Composition is the gate.** An app that does not list `mcpserver.New()` has no
agent interface, which is a stronger guarantee than any flag, and it is why no
provider needs its own gate and no separate plugin was created for any
capability.

---

## Config

From [The transport, and how an agent attaches to a running game](https://github.com/dvoyni/cog/issues/206)
§8.

```go
type Config struct {
	Addr    string        // default "127.0.0.1:7654"
	Path    string        // default "/mcp"
	Timeout time.Duration // default 30s
}
```

Three fields, and the absences matter as much as the presences.

**`Timeout` bounds the wait, not the work**, and the spec says so in those
words. It is applied as a `context.WithTimeout` on the executioner the
capability receives, so it cancels lock acquisition and any cooperative wait —
which is what a `Func` body does while waiting for a frame — but **Go cannot
interrupt a command body that is already running**, so a genuinely wedged
handler stays wedged. What the deadline buys is that the agent gets a clean
`Unavailable` in 30 seconds instead of burning five minutes of its session on
the client's idle abort. That is a courtesy, not a safety property, and claiming
otherwise would be a lie a reader would eventually catch.

*Decided without asking, and easy to reverse: the alternative is no broker
deadline at all, leaving each blocking capability to set its own.*

**There is no capture output directory, and there will not be one.** A field
that means something only to `gfx` is the knower arriving through config. The
directory belongs to whichever provider writes files — and no provider has one
either, because the agent names every path
([#207](https://github.com/dvoyni/cog/issues/207) §2).

**There is no enable flag, no auth field and no log level.** Composition is the
gate; auth is a different effort with a different destination; logging is one
line at startup (see
[Attaching](#attaching-what-a-person-does-once)).

---

## The transport position

From [#206](https://github.com/dvoyni/cog/issues/206) §1. These are not
independent knobs — they are one position, and the SDK's own coupling between
them is why.

```
Stateless:                    true
PropagateRequestCancellation: true
JSONResponse:                 true
DisableLocalhostProtection:   false   (the default)
MaxRequestBodyBytes:          4 MiB   (the default)
```

Protocol revision `2026-07-28` is served over streamable HTTP **only** when
`Stateless = true`, and `PropagateRequestCancellation` — which ties a tool
handler's context to the HTTP request's — takes effect only at that revision. So
statelessness is what makes
[#204](https://github.com/dvoyni/cog/issues/204) §3 true: `k.WithContext(req.Context())`
means *the agent hung up* only in stateless mode.

**Statelessness costs exactly two things, and both were already decided against.**
Server→client requests are rejected outright — but the only one cog would want
is `tools/list_changed`, and the tool set is fixed at `Start`, so it is never
sent. And there is no session identity — which is precisely what
[#205](https://github.com/dvoyni/cog/issues/205) ruled out on three separate
grounds. `Stateless = true` is therefore **the transport-level expression of a
contract decision**, enforcing it mechanically rather than by discipline: no
future capability can reach for a session identity the transport does not have.

One caveat belongs here rather than in a reader's discovery: **a client
negotiating an older revision gets no cancellation propagation at all.** A
blocking capability therefore cannot rely on the client's hang-up to unwind it,
and needs its own deadline regardless — which is why the arm-then-wait
discipline requires one
([mcp §Arm-then-wait](../../../mcp/docs/specs/mcp.md#arm-then-wait)).

**Stdio was rejected** while charting, for two reasons that have not changed:
the client would own the game's lifetime, and the engine's stdout — where its
errors go — becomes protocol.

---

## The address

```go
Addr = "127.0.0.1:7654"   // host:port
Path = "/mcp"
```

A game repo can commit a project-scoped `.mcp.json`
([#201](https://github.com/dvoyni/cog/issues/201)), which reduces the one-time
human step to nothing — but only if the URL is stable across runs. That rules
out all three alternatives at once: an ephemeral port, a port written to a file,
and a port printed at startup each trade a stable URL for collision-avoidance
that a single running game does not need.

`127.0.0.1` explicitly, **never `localhost`**: the IPv4/IPv6 mismatch is the
classic failure here, and the SDK's DNS-rebinding protection checks the `Host`
header rather than the bind address, so nothing is lost by being literal.

**The number is arbitrary within a band, and this spec says so rather than
implying a reservation.** The band is real: below the OS ephemeral range (Linux
32768+, Windows 49152+), where a fixed port can otherwise be stolen by an
ephemeral allocation, and clear of the common dev ports. Each game commits its
own `.mcp.json` with its own port anyway, so the default matters less than its
stability.

Localhost-only binding is a **default, not a design**. It is one field, and an
app that changes it has left the scope of this document.

---

## Lifecycle

From [#206](https://github.com/dvoyni/cog/issues/206) §4 and
[#200](https://github.com/dvoyni/cog/issues/200) §3.

### Open at `Start`

At `Start` the broker, in order:

1. Caches `k.Plugins[mcp.Provider]()`.
2. Calls `Capabilities()` on each, exactly once.
3. Validates every capability and renders it as a tool.
4. Retains the `Start` executioner (see
   [mcp §The retained executioner](../../../mcp/docs/specs/mcp.md#the-retained-executioner)).
5. Listens, and starts one goroutine waiting on `k.Context().Done()`.

**A known flaw, stated rather than left to be found.** The broker declares no
dependencies and `orderPlugins` is stable in the app author's listing order
(`kernel/engine.go:160-180`), so the broker typically starts *early* — before,
say, `canvas.Start` mounts the built-in font (`canvas/plugin.go:71`). A tool call
landing in that sub-millisecond window would dispatch into a half-initialized
engine.

The zero-risk alternative was subscribing to `app.InitEvent`, which a host
publishes after every `Start`. **Rejected**: an engine with no host never
publishes it, and an engine with no host is exactly the shape a test or a
CI-driven agent would compose. Keeping headless serving is worth more than
closing a race that requires an already-connected client to fire into a window
it cannot see.

Listing `mcpserver.New()` **last** in the plugin slice is still worth doing —
`orderPlugins` picks the earliest-indexed ready plugin each round, so a
dependency-free plugin listed last starts last and stops first — but it is
belt-and-braces, and the spec must not rely on it.

### Close on engine-context cancellation, not in `Stop`

Three facts decide this, and they are the part of the design most likely to be
got wrong by someone reimplementing it:

- **`Run` calls `e.cancel(nil)` before the `Stop` loop** (`kernel/engine.go:248`).
  The scheduler exits on that cancellation, after which every dispatch returns
  `ErrSchedulerStopped{}` or `context.Canceled`
  (`kernel/scheduler.go:203,215`). A broker that waited for `Stop` would be
  serving an engine that can no longer execute anything.
- **`Stop` order is reverse start order**, which is the app author's listing
  order. A dependency-less broker listed first stops **last**. It cannot rely on
  stopping early.
- **`app` has no plugin at all** — it is pure contract — so nothing can declare
  a dependency on it to force ordering, and `app.QuitEvent` fires only when a
  host exists (`wgpu/plugin.go:119`).

So: the broker runs one goroutine on `k.Context().Done()` that calls
`Server.Shutdown`. That fires before any `Stop`, is independent of listing
order, and needs no dependency on anything. `Stop` closes the listener as a
belt-and-braces second call.

**Obligations, all of them consequences of the retained executioner:**

- Stop accepting requests on engine-context cancellation, and **drain in-flight
  handlers before `Stop` returns**.
- Treat a cancelled-context dispatch error as an ordinary shutdown result, not
  an engine error — see below.
- **The window it cannot close alone**: the host loop has already returned by
  the time cancellation fires, so a tool call in flight waiting for the *next
  frame* waits for a frame that will never come. This is a second, independent
  reason every blocking capability needs its own deadline, and it is why
  `gfx`'s readback contract delivers `ErrCaptureAbandoned{}` through the same
  seam a result would have used rather than going silent.

### Shutdown is not a fault

Any non-`Unavailable` error routes to `kernel.ReportError`, whose default
handler terminates the engine — so an in-flight tool call cancelled *because the
game is closing* would be reported as a system fault on the way out, which is
both wrong and noisy.

**The broker classifies `context.Canceled` and `ErrSchedulerStopped{}` as
`mcp.Unavailable{Reason: "the game is shutting down"}`.** A game exiting is the
normal case here, and the agent reads one sentence and moves on.

---

## Failing to bind

**A bind failure terminates the engine**, through `kernel.ReportError`, with the
address in the error.

The reasoning is the same one that makes a malformed capability fatal: a broker
that is silently absent is undebuggable from the agent's side, because it cannot
tell *not offered* from *not running*. The common cause — a stale game process
still holding the port — is exactly the case where a loud failure saves the most
time.

**js/wasm is the same answer.** `net.Listen` does not work in a browser, and cog
does target it (`storage/disk_js.go`). An app that composed `mcpserver.New()`
into a browser build made a composition mistake, and cog fails composition
loudly. This is deliberately **not** a build tag: excluding the plugin on `js`
would break a `main.go` shared between desktop and web builds at *compile* time,
which is a worse failure than the one build that is actually wrong failing at
startup.

---

## Rendering a capability

The broker owns names, schemas, annotations and the result envelope. A provider
owns the description prose and nothing else that reaches the wire
([#204](https://github.com/dvoyni/cog/issues/204) §8).

**Tool name** is `<plugin>_<capability>`, taken from `Provider.Name()` and
`Capability.Name()`. Underscore, because the MCP name charset is conservative
and `_` is the one separator no client rejects. Uniqueness across providers is
inherited from the engine's own duplicate-plugin-name rejection
(`ErrConflictingPluginName`, `kernel/engine.go:77`).

**Schemas** come from `jsonschema.ForType` over `Capability.RequestType()` and
`ResponseType()`, with `ForOptions.TypeSchemas` populated by walking those types
for `mcp.TextValued` implementors — see
[mcp §Types that cross as text](../../../mcp/docs/specs/mcp.md#types-that-cross-as-text).
That walk is the only place the broker inspects a provider's types for anything
beyond their shape, and it learns a string set, never a meaning.

**Annotations** map as
[mcp §Annotations](../../../mcp/docs/specs/mcp.md#annotations) states:
`ReadOnlyHint = readOnly`, `DestructiveHint` always false, `OpenWorldHint`
always false, `IdempotentHint` untouched.

**A malformed capability terminates the engine.** A deferred `err`, a duplicate
name within a provider, or a request type whose schema root is not `object` all
go to `kernel.ReportError`. Chosen over report-and-skip because a silently
absent tool is close to undebuggable from the agent's side, and because this is
a composition error, which cog fails loudly. The counter-argument — a broken
debug tool should not take down a game — is real but weak here: the broker is
only present in a build being actively debugged.

This also **guards two SDK panics**: `Server.AddTool` panics on a nil or
non-`object` input schema, so descriptor validation at `Start` is a broker
obligation rather than a nicety.

**The tool set is fixed at `Start` and never changes.** Nothing is registered
later, so `HasTools`/`tools/list_changed` is never needed — which is fortunate,
because a stateless server cannot send it, and because
[#201](https://github.com/dvoyni/cog/issues/201) found that list-changed
capabilities must be declared *before* `Connect` or clients are never told.

---

## Invoking one

Per call, on the HTTP goroutine:

1. `reflect.New(c.RequestType())` and unmarshal the arguments into it.
2. `k2 := k.WithContext(ctx)` where `ctx` is the request context, then
   `context.WithTimeout(ctx, cfg.Timeout)`.
3. `c.Invoke(k2, req)`.
4. On `mcp.Unavailable`, return an ordinary tool result with `IsError` set and
   `Reason` as the text.
5. On any other error, `kernel.ReportError` and return a generic failure — no
   internals in the model's context.
6. On success, marshal the response struct into `structuredContent`.

Step 2 is what gives a capability body two lifetimes for free: the dispatch dies
on the agent hanging up **or** on engine shutdown, with no bookkeeping in the
body.

---

## Concurrency

From [#206](https://github.com/dvoyni/cog/issues/206) §3. **There is nothing to
design, and the spec says so explicitly because a reader will otherwise assume a
mechanism exists.**

Two simultaneous tool calls are two dispatches the scheduler already serializes
by resource. A capability body runs on the calling goroutine, so an agent
issuing several calls at once already gets real parallelism wherever the
resources do not overlap. No limit is imposed and none is needed.

The genuinely exclusive cases are already refused at their own layer and in
words: one capture in flight (`ErrCaptureBusy`), one snapshot of a given kind in
flight. The broker renders those refusals like any other `Unavailable` and knows
nothing about why they happened.

The one thing the broker must not do is serialize calls itself. Doing so would
break the pairing recipe in
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment), which
depends on parallel arms landing in one inter-tick gap.

---

## What pause does not reach

The worry that a paused engine might strand the server is unfounded, and it is
worth stating as a property rather than leaving to be rediscovered. The
scheduler's coordinator goroutine and the HTTP goroutines are both independent of
the game loop, so **every non-blocking capability works normally in a paused
engine** — which is the property that makes pause useful to an agent at all, and
it needs no mechanism to hold it up.

Only capabilities that *wait for a frame* are affected, and each answers for
itself: a capture under pause needs no tick, a snapshot performs one step, and
`wgpu_time step` is the thing doing the stepping. The ceilings they sit under
are the broker's 30s and the client's five-minute idle abort
([#201](https://github.com/dvoyni/cog/issues/201)).

---

## `mcpserver_architecture`

From
[What the agent learns about the engine's architecture](https://github.com/dvoyni/cog/issues/210).
The broker implements `mcp.Provider` over itself and returns exactly one
capability.

**One consequence to state rather than discover:
`k.Plugins[mcp.Provider]()` finds the broker itself.** Harmless — collection is
uniform and the broker's own capability arrives through the same path as
everyone else's — but it must be written down, because it looks like a bug to
anyone reading the loop cold.

### Why it exists at all

Not because an architecture dump is nice to have. As `Describe` stands today the
capability **would have ruled itself out of scope**: plugin order restates
`Dependencies()`, ownership restates `Register`, and `DependsOn` restates
`.After[…]()`. An agent debugging a cog game has cog in its module cache and can
`grep` all three — which is precisely why this map ruled out a static
repo-indexing server.

What earns it is the **resolved, transitive lock closure** each handler ends up
holding once `absorb` folds in its declared dispatches, plus the `uses` edges
that explain it. *What does dispatching this actually lock* and *why did my
handler block* cannot be computed by hand, because a handler deliberately
**never names the resources behind a command it uses** (`CONTEXT.md`, *Declared
dispatch*). That is the tool's entire reason to exist, and it is why this
capability depends on `kernel`'s description growing `Reads`/`Writes`/`Uses` —
see
[mcp §Required kernel changes](../../../mcp/docs/specs/mcp.md#required-kernel-changes).

### Shape

- **`mcp.Func`**, whose body calls `k.Describe()` directly. It is the only
  capability that dispatches nothing, under the narrow exception in
  [mcp §The capability-body rule](../../../mcp/docs/specs/mcp.md#the-capability-body-rule).
  Making it an `mcp.Command` would mean the broker registering a command it
  dispatches to itself — ceremony that takes the scheduler for nothing.
- **Flat JSON, four arrays**, mirroring `ArchitectureDescription`. `Dump` stays
  the human spelling.
- **No index, because the type string is the address.** `Uses`, `DependsOn`,
  `Reads` and `Writes` are all joins on it. This is
  [#208](https://github.com/dvoyni/cog/issues/208) §6's addressability rule
  satisfied without inventing a scheme — a second confirmation that the rule was
  about addressability rather than about integers.
- **A `reflect.Type` renders as `Type.String()`** — `gfx.RenderEvent`,
  package-qualified by short name. That is the form appearing in the source the
  agent greps next, which is the entire point of handing it a type name.
  `PkgPath()+"."+Name()` is unambiguous but unsearchable; the short-name
  collision is theoretical in cog, and where it ever bites, `Owner`
  disambiguates in the same record.
- **Optional `path`**, inherited. A subscription DAG is worth grepping.
- **No filter.** A snapshot's filter exists to bound serialization on the game's
  goroutine inside a tick. Nothing here runs there, and the document is tens of
  entries. Inheriting the filter would be cargo.
- **`mcp.ReadOnly()`**, and **no deadline of its own** — nothing waits.
- **The command list ships**, because it answers *which plugin owns this
  behaviour* and is the anchor for the `Uses` edges, which are meaningless
  without it.

### A tool, not an MCP resource

Decided against evidence rather than by assumption, because a
static-for-the-lifetime document is textbook resource material
([#210](https://github.com/dvoyni/cog/issues/210) §3):

- The spec's *application-driven* framing is a **default, not a prohibition** —
  automatic context inclusion is a sanctioned option and `annotations.audience`
  accepts `"assistant"`.
- **Claude Code closes the gap outright**: it provides `ListMcpResourcesTool`
  and `ReadMcpResourceTool` as ordinary model-callable tools, so an agent there
  *can* read a resource with no human gesture.
- **VS Code / Copilot cannot** — resources are read-only context a person
  attaches through *Add Context → MCP Resources*.
- **Cursor is unverified.**

**Portability decides it**: reachable everywhere beats autonomously reachable in
one client. Registering both spellings was rejected on the two-spellings ground
that also killed compound input steps, and serving it as a resource would mean
growing `Capability` with resource-ness for a single member — the move
[#205](https://github.com/dvoyni/cog/issues/205) already rejected for
standing-ness.

Two non-options confirmed dead in passing: **a file written at startup**
reintroduces the writable directory cog deliberately does not have, and **server
instructions** — the one thing that loads unprompted — are truncated at 2 KB,
which `Dump`'s output already exceeds for cog's current plugin set.

### The description prose

Reproduced in full, per the house style, so it is reviewed as prompt text:

> What this engine is actually composed of: the plugins in start order, who owns
> which command, event and resource, the subscription dependency graph, and —
> the part you cannot get by reading source — the full set of resources each
> handler ends up locking once the commands it declares are folded in. Use it
> when you need to know what a dispatch really touches, or why one handler waits
> for another. Commands are listed so you can see who owns behaviour; you cannot
> call them from here, and there is no tool that takes a command name. Pass
> `path` to write the JSON to a file instead of returning it inline.

### The broker does not describe itself

*Which plugins provide, and what* needs nothing built. The agent's client
already receives it from `tools/list`, and `<plugin>_<capability>` namespacing
means the provider list is recoverable from the tool names the agent already
holds. An `mcpserver_providers` capability would be a second, worse spelling of
`tools/list`.

---

## The rule that closes the door

From [#229](https://github.com/dvoyni/cog/issues/229) §7. Stated as a rule
rather than as a verdict, because declining one candidate on its merits leaves
the next one open under a different name.

> **The broker provides a capability of its own only for facts about
> composition.** Anything that *invokes* another provider's capability is the
> knower, whatever it is called.

`mcpserver_architecture` is a **closed set of one**. What `k.Describe()` returns
is the one thing no provider owns; everything else belongs to the plugin that
holds the state.

The candidate this was written against is a generic batch capability, and it
failed on its own terms before the rule was reached: a *sequential* batch cannot
co-arm — `Capability.invoke` is opaque, arm and wait are one function, so entry
1 waits for tick N+1 before entry 2 arms — and only a *concurrent* batch
co-arms, which under pause is the broken form, because a capture costs no tick
while a snapshot steps. What pairs a moment instead is an ordering plus one
driver rule; see
[mcp §Pairing a moment](../../../mcp/docs/specs/mcp.md#pairing-a-moment).

A per-provider batch (`canvas_batch`, `ui_batch`) cannot launder it either: it
multiplies the mechanism by the number of providers to buy strictly less, since
the pairing that motivated it is *cross*-provider by construction.

---

## Attaching: what a person does once

Nothing, if the game repo commits `.mcp.json`:

```json
{ "mcpServers": { "cog": { "type": "http", "url": "http://127.0.0.1:7654/mcp" } } }
```

Project scope is shared through version control and gated by one approval
prompt, and that is the whole of it.

Two client behaviours belong here as workflow rather than being rediscovered by
everyone ([#201](https://github.com/dvoyni/cog/issues/201) §5). A server that
**never** connected is retried three times; a **dropped** one gets five attempts
over roughly 31 seconds. So starting the client before the game marks the server
failed long before the window opens, and the recovery is not something anyone
guesses.

> **Start the game, then attach.** If the game restarts mid-session, `/mcp` →
> reconnect.

And the broker logs its own attach line at startup — a dozen characters of code
that turns *what was the URL* into copy-paste:

```
mcpserver: claude mcp add --transport http cog http://127.0.0.1:7654/mcp
```

**Gap.** No SDK was conformance-tested against a real client; everything above
is read off source, docs and release notes
([#201](https://github.com/dvoyni/cog/issues/201)). The retry counts, the
five-minute idle abort and the 25,000-token inline cap are all documented
values, not observed ones. What would settle it is the first implementation
issue attaching a real client to a real game.

---

## Required mcpserver changes

A checklist for an implementation session. `mcpserver/` does not exist yet; this
is the whole package.

**Module**

- Add `github.com/modelcontextprotocol/go-sdk` and
  `github.com/google/jsonschema-go`. Two direct dependencies, not one — schema
  inference lives in a separate module from the SDK. Both clear cog's Go floor.
- Neither may appear in any other package's import graph. A test that asserts
  this is worth writing: `go list -deps ./gfx` must not mention either.

**`mcpserver/plugin.go`**

- `New(cfg ...Config) kernel.Plugin`; `Name() "mcpserver"`; `Dependencies() nil`.
- `Start`: cache providers, collect and validate capabilities, build the server,
  listen, spawn the cancellation watcher, log the attach line.
- `Stop`: close the listener; in-flight handlers already drained.
- The plugin implements `mcp.Provider` over itself, returning
  `mcpserver_architecture`.

**`mcpserver/config.go`**

- `Config{Addr, Path, Timeout}` with the defaults above, and a `Config` zero
  value that means "all defaults".

**`mcpserver/render.go`**

- Capability → tool: name, schemas via `jsonschema.ForType`, the `TextValued`
  walk building `ForOptions.TypeSchemas`, annotation mapping.
- Validation: deferred `err`, duplicate name within a provider, non-`object`
  schema root. All to `kernel.ReportError`.

**`mcpserver/invoke.go`**

- The per-call sequence in [Invoking one](#invoking-one), including the
  `Unavailable` / fault split and the shutdown classification.

**`mcpserver/architecture.go`**

- The one capability, its request (`{path?}`), its four-array response, and
  `reflect.Type` rendering via `String()`.

**Tests**

- A provider offering a capability whose request type is not a struct fails
  composition rather than being skipped.
- Two providers offering the same capability name compose fine (the plugin
  prefix separates them); one provider offering it twice fails.
- Engine-context cancellation shuts the server down **before** any provider's
  `Stop` runs.
- A dispatch that returns `ErrSchedulerStopped{}` renders as an `Unavailable`,
  not as a `ReportError`.
- The `TextValued` walk finds a type nested inside a slice of structs.

**`CONTEXT.md`** — nothing. Every term this package touches is already defined;
*transport*, *port* and *session* are HTTP and MCP vocabulary, not cog's, and a
glossary that re-explains them is a glossary nobody trusts. The one word this
package might have added, *session*, it removes instead.

---

## Out of scope

- **Authentication, tokens, and shipped-build exposure.** Composition is the
  on/off switch. The moment someone wants this in a shipped build for
  player-facing automation it becomes an auth effort, and that is a different
  map with a different destination.

- **Two games at once.** The address is fixed and a bind failure terminates, so
  **a second instance of the same game kills itself**. Two *different* games
  each commit their own `.mcp.json` with their own port and coexist fine. The
  multi-instance case is fog on the map, not a gap here: the session half of it
  is already answered — nothing belongs to a session — and whose capture is
  whose is answered too, because each agent names its own file and a colliding
  second capture is refused in words.

- **A CLI instead of MCP.** Decided on the map against live counter-evidence:
  two first parties have deprecated their own MCP server in favour of one.
  Neither calls MCP wrong, and Microsoft names the conditions under which it is
  right — "persistent state, rich introspection, and iterative reasoning over
  page structure … long-running autonomous workflows" — which is a cog agent
  iterating on a frame. The distinguishing fact is that **the engine is a live
  process holding state the agent pokes repeatedly**: a CLI would have to attach
  to that process anyway, and would then need the same extension point, the same
  capabilities and the same broker, differing only in how the agent talks to it.
  Almost everything on this map is transport-independent, so it is a cheap late
  reversal rather than a fork in the route.

- **A static repo-indexing MCP server.** A search index over files an agent can
  already `grep`, against unusually good docs.

- **Pushing anything to a connected agent.** A stateless streamable-HTTP server
  cannot make server→client requests at all, and the newest revision requires
  stateless for that transport — the door is closing on its own.
