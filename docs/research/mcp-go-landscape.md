# The Go MCP landscape

Research for [research: the Go MCP landscape](https://github.com/dvoyni/cog/issues/201), a ticket on the map [mcp: an agent-facing extension point, and the broker that serves it](https://github.com/dvoyni/cog/issues/199).

**Gathered 2026-09-10.** Both the protocol and the SDKs move fast; every claim below is dated or version-pinned, because a reader six months from now needs to know how stale this is.

**Sources are primary throughout**: the SDK repositories' own `go.mod`, source files, doc comments, release lists and `CONTRIBUTING.md`; the protocol schema files in `modelcontextprotocol/modelcontextprotocol`. Where something is inferred rather than stated, it says so.

## The short version

- The **official Go SDK** (`github.com/modelcontextprotocol/go-sdk`) is real, stable since v1.0.0 on 2025-09-30, currently **v1.7.0** (2026-07-28), Tier 1 in the project's own SDK tiering, and administered jointly by the Go team and Anthropic. It serves streamable HTTP out of the box and **infers a tool's JSON schema from a Go type by reflection** — the thing cog's design most wants.
- Its cost to this module is **+1 direct dependency that drags 6 runtime deps and 3 indirect**, and a **Go 1.25 floor** (cog is on 1.27, so that is free).
- **`mark3labs/mcp-go`** is the only community alternative worth considering — more imported than the official SDK, at v1.0.0 as of 2026-09-02, and the only community SDK implementing the current protocol revision. It carries *two* JSON Schema libraries.
- **Hand-rolling is genuinely viable** at ~250–350 lines for a stdio-only tools-only server with **zero** new dependencies, but the recurring cost is a hand-written JSON Schema per tool, kept manually in sync with the Go struct it unmarshals into. That is precisely the drift the reflection path removes.
- **A protocol revision landed on 2026-07-28 that removes the `initialize` handshake entirely** and, in the official SDK, makes **stateless mode mandatory** for that revision. This bears directly on the transport ticket.
- **Claude Code's own limits settle two open questions.** An inline image result is capped at 25,000 tokens with no per-tool escape hatch and no spill-to-file, while an oversized *text* result is automatically saved to a file and replaced by its path — so the map's path-on-disk choice is close to forced, and a large queue snapshot degrades gracefully on its own. A blocking tool call is aborted after **five minutes idle**, which bounds what "the tool blocks until the next frame" may mean in a paused engine.

## 1. Protocol revisions

The SDK's own constants (`mcp/shared.go:50-63`) are the cleanest statement of what exists:

```
2026-07-28   latest
2025-11-25
2025-06-18
2025-03-26
2024-11-05   original
```

**2026-07-28 is a substantial rewrite, not an increment.** Per the v1.7.0 release notes and the schema file (`schema/2026-07-28/schema.ts`, 3197 lines vs 1613 for 2025-06-18):

- `initialize` / `notifications/initialized` **are gone**, replaced by `server/discover` plus per-request `_meta` carrying protocol version, client identity and capabilities (SEP-2575).
- Server→client *requests* are replaced by multi-round-trip `inputRequests` / `inputResponses` (SEP-2322).
- Free-floating change notifications are replaced by an explicit `subscriptions/listen` stream (SEP-2575).
- `ping`, `logging/setLevel`, `resources/subscribe`/`unsubscribe` are **removed** on that revision.
- **Roots, sampling and logging are deprecated** across the board by SEP-2577, with a stated deprecation window of at least twelve months.
- `inputSchema` loosens from `{type: "object", properties, required}` to arbitrary JSON Schema 2020-12 — `$ref`, `$defs`, `oneOf`, `if`/`then`/`else` all become legal.

The last revision with the classic handshake is **2025-11-25**. Most deployed clients still negotiate 2025-06-18 or 2025-11-25, so a server that wants to be reachable today implements the handshake regardless.

## 2. The official SDK

`github.com/modelcontextprotocol/go-sdk`, in the `modelcontextprotocol` org beside the TypeScript, Python, Kotlin, Java, C#, Rust, Ruby, Swift and PHP SDKs. Created 2025-04-23, 5,084 stars, Apache-2.0 for new contributions (MIT for pre-existing code).

**Governance**, from `CONTRIBUTING.md`:

> "Initially, the Go SDK repository will be administered by the Go team and Anthropic, and they will be the approvers (the set of people able to merge PRs to the SDK), also referred to as the 'Working Group'."

The repo description adds "Maintained in collaboration with Google" — the same pattern as kotlin-sdk/JetBrains and csharp-sdk/Microsoft. Go is **Tier 1** in the project's SDK tiering (published 2026-02-23): 100% conformance-test pass required, new protocol features before spec release.

### Stability

**v1.0.0 shipped 2025-09-30**, and its release notes are the commitment:

> "**This is a stable release of the Go SDK.** … formalizes a compatibility guarantee: **going forward we won't make breaking API changes.** … If we want to improve the current API in the future, we'll do so in backward compatible ways, such as by deprecating and adding."

`CONTRIBUTING.md` §Versioning spells out what counts as breaking, and notes the Go module system makes a breaking release unable to reach existing users by accident (it would import as `/v2`). The policy covers `mcp`, `jsonrpc`, `auth`, `auth/extauth` and `oauthex`; everything under `internal/` is exempt.

This matters because **v0.x promised the opposite** — v0.2.0's README said "don't use it in real projects" — so any v0.x-era documentation or example found on the web is untrustworthy. Concretely: the SDK used to vendor its own `jsonschema` package; it was removed on 2025-08-07 in favour of `github.com/google/jsonschema-go`, so the import path `github.com/modelcontextprotocol/go-sdk/jsonschema` is **dead** and old examples will not compile.

### Version compatibility

| SDK version | Latest MCP spec | All supported |
| --- | --- | --- |
| v1.7.0+ | 2026-07-28 | 2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26, 2024-11-05 |
| v1.4.0–v1.6.1 | 2025-11-25 | 2025-11-25, 2025-06-18, 2025-03-26, 2024-11-05 |
| v1.0.0–v1.1.0 | 2025-06-18 | 2025-06-18, 2025-03-26, 2024-11-05 |

Latest stable **v1.7.0, 2026-07-28**; latest of any kind **v1.8.0-pre.2, 2026-09-04**. Since v1.1.0 every minor is staged through `vX.Y.0-pre.N` tags first.

### Dependencies

`go.mod` at v1.7.0 / main:

```
go 1.25.0

require (
    github.com/golang-jwt/jwt/v5 v5.3.1
    github.com/google/go-cmp v0.7.0
    github.com/google/jsonschema-go v0.4.3
    github.com/segmentio/encoding v0.5.4
    github.com/yosida95/uritemplate/v3 v3.0.2
    golang.org/x/oauth2 v0.35.0
    golang.org/x/time v0.15.0
    golang.org/x/tools v0.42.0
)
require ( // indirect
    github.com/segmentio/asm v1.1.3
    golang.org/x/sync v0.20.0
    golang.org/x/sys v0.41.0
)
```

Eight direct, but `golang.org/x/tools` appears in exactly one file (`mcp/conformance_test.go`) and `go-cmp` is a test comparison library, so the **runtime** direct surface is six: golang-jwt, jsonschema-go, segmentio/encoding, uritemplate, x/oauth2, x/time.

**For cog specifically**: the module is at `go 1.27` with seven direct dependencies, so the Go floor is free and this is one new direct edge. `golang.org/x/sys` and `golang.org/x/text` are already in cog's graph indirectly.

### Serving streamable HTTP

All of this is in package `mcp`:

| Thing | Where |
| --- | --- |
| `NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler` | `mcp/streamable.go:231` |
| `StreamableHTTPHandler` — a plain `http.Handler` | `mcp/streamable.go:47` |
| `StreamableServerTransport` | `mcp/streamable.go:789` |
| `SSEHandler` / `NewSSEHandler` — legacy 2024-11-05 transport | `mcp/sse.go:49,90` |
| `StdioTransport` | `mcp/transport.go:128` |
| `InMemoryTransport` / `NewInMemoryTransports()` | `mcp/transport.go:168,183` |

It is a `http.Handler`, so it mounts on any `http.ServeMux` — a plugin owning its own listener has nothing to fight.

`StreamableHTTPOptions` fields worth knowing for the transport ticket:

- **`Stateless bool`** — and this is the sharp bit. From `docs/protocol.md`: *"**Required for `2026-07-28`**: the streamable HTTP transport accepts requests at protocol version `2026-07-28` **only** when `Stateless = true`."* In stateless mode the session header is neither read nor set, **any server→client request is rejected outright** (nothing can respond to it), and GET and DELETE return 405.
- `JSONResponse bool` — respond `application/json` rather than `text/event-stream`.
- `EventStore` — stream resumption and replay.
- `SessionTimeout time.Duration` — zero means idle sessions never close.
- **`DisableLocalhostProtection bool`** — DNS-rebinding protection is **on by default**, which is what a localhost-bound dev server wants.
- `MaxRequestBodyBytes int64` — defaults to 4 MiB.
- `PropagateRequestCancellation bool` — ties the handler context to the HTTP request context; ≥2026-07-28 only.

Session ID generation is **not** here — it is `ServerOptions.GetSessionID func() string` (`mcp/server.go:164`), defaulting to `crypto/rand.Text`, and returning `""` suppresses the header. Not consulted when stateless.

### Declaring tools — the part cog's design turns on

```go
func NewServer(impl *Implementation, options *ServerOptions) *Server           // server.go:211
func (s *Server) AddTool(t *Tool, h ToolHandler)                              // server.go:315  (low level)
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])      // server.go:603  (generic)
func (s *Server) Run(ctx context.Context, t Transport) error                  // server.go:1354
func (s *Server) Connect(ctx, t Transport, opts *ServerSessionOptions) (*ServerSession, error)
```

**Schema inference exists and is the documented default.** `AddTool` → `toolForErr` → `setSchema[T]` → `jsonschema.ForType(rt, ...)` at `mcp/server.go:528`, using `github.com/google/jsonschema-go`. The doc comment:

> "If the tool's input schema is nil, it is set to the schema inferred from the In type parameter. Types are inferred from Go types, and property descriptions are read from the 'jsonschema' struct tag. … The In type argument must be a map or a struct, so that its inferred JSON Schema has type 'object', as required by the spec."

The inference rules (`jsonschema.For`, `infer.go:44`): strings→`"string"`, bools→`"boolean"`, all int kinds→`"integer"`, floats→`"number"`, slices/arrays→`"array"` with item schema, string-keyed maps and structs→`"object"`. **Struct fields marked `omitempty` or `omitzero` are optional; every other field becomes required.** Property order follows field order. Cycles are an error, as are map keys other than string, funcs, chans, complex and unsafe pointers.

**Exactly two struct tags are read.** `json:` gives the property name and, through `omitempty`/`omitzero`, optionality. `jsonschema:` is the **whole tag value used as the property description** — free text, no `key=value` syntax, and an empty tag is an error:

```go
type CaptureRequest struct {
    Layer  string `json:"layer,omitempty" jsonschema:"only capture this canvas layer; omit for all layers"`
    Format string `json:"format,omitempty" jsonschema:"png or json"`
}
```

Two escape hatches when inference is not enough: set `Tool.InputSchema`/`Tool.OutputSchema` explicitly (both are `any`, so a `*jsonschema.Schema` or a `json.RawMessage` both work, and `setSchema` skips inference when non-nil), or steer inference with `jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema}` and hand-tweak the result.

Guardrails: `Server.AddTool` **panics** if `InputSchema` is nil or its type is not `"object"` (`mcp/server.go:319-341`). Tool names are validated to ≤128 chars of `[A-Za-z0-9_.-]`, but an invalid name only logs.

`ServerOptions.SchemaCache` (`mcp/schema_cache.go:33`) exists for stateless deployments building a fresh `Server` per request — not cog's case, where one long-lived server is the point.

### Notifications and sessions

`func (s *Server) Sessions() iter.Seq[*ServerSession]` (`mcp/server.go:874`) enumerates live sessions. Tool-list changes notify automatically: `AddTool`/`RemoveTools`/etc. funnel through `changeAndNotify`, which debounces 10ms and fans out. **Gated on capabilities** — features registered after `Connect` require `ServerOptions.HasTools` or an explicit capability declaration, else clients are never told.

This matters for cog: **a broker's tool set is only known after providers are collected**, which happens at `Start`. If tools are added after the server is connected, the capability has to be declared up front or the list-changed notification is silently dropped.

`ServerSession.Log` is **deprecated** as of 2026-07-28 (SEP-2577), along with roots and sampling.

## 3. Community SDKs

The project publishes **no community SDK list** — `modelcontextprotocol.io`'s SDK page lists only the ten first-party SDKs. The candidates below came from GitHub search.

### `mark3labs/mcp-go` — the only live alternative

9,094 stars, MIT, created 2024-11-27. **v1.0.0 on 2026-09-02**, 89 releases, last commit 2026-09-02. Not archived, and its README carries no deprecation notice or pointer to the official SDK.

**It is more used than the official SDK**: pkg.go.dev reports **1,630 known importers** for `mcp-go/server` against **1,443** for `go-sdk/mcp`.

**It implements 2026-07-28** — the only community SDK that does — and negotiates all five revisions. It also ships Tasks and elicitation.

Dependencies: 7 direct (2 test-only), so **5 land in a consumer's build graph**, plus ~6 indirect. Note it carries **two** JSON Schema libraries: `google/jsonschema-go` for generation and `santhosh-tekuri/jsonschema/v6` for validation. **`go 1.25.5` is a hard floor** — a patch-level directive.

Both authoring styles, and the builder is the documented default:

```go
tool := mcp.NewTool("calculate",
    mcp.WithDescription("Perform arithmetic operations"),
    mcp.WithString("operation", mcp.Required(), mcp.Enum("add", "subtract"), mcp.Description("...")),
)
// or, reflection:
mcp.NewTool("search", mcp.WithDescription("..."), mcp.WithInputSchema[SearchRequest]())
```

Server transports: stdio, legacy SSE, **streamable HTTP** (`server.NewStreamableHTTPServer`, implements `http.ServeHTTP`), in-process, plus a `mcptest` harness.

### Ruled out

- **`metoro-io/mcp-golang`** (1,230★). `main` dead since 2025-09-02; protocol **hard-coded to 2024-11-05**; the SSE server transport is **entirely commented out** despite the README checking the box; no streamable HTTP; drags `gin-gonic/gin v1.8.1` and 2021-era `golang.org/x/*` into the graph through 29 indirect deps. 92 importers.
- **`ThinkInAIXYZ/go-mcp`** (677★). Genuinely the leanest — 4 direct, 2 indirect, `go 1.18`, hand-rolled schema generation — and it does serve streamable HTTP. But it is pinned to **2025-03-26**: no `outputSchema`, no `structuredContent`, no `ResourceLink`, no elicitation. That gap has not closed in over a year. 22 importers.
- **`strowk/foxy-contexts`** (114★). Never left beta, dead since 2025-07-04, and pulls in `go.uber.org/fx` — a DI framework — plus echo and zap.

None of the four is archived and none declares itself deprecated; the dormancy of metoro and foxy-contexts is **inferred from commit dates and dead code**, not stated by their maintainers.

## 4. Hand-rolling

Read off the schema files directly, for 2025-06-18 (the last widely-negotiated revision with a handshake).

**The wire shapes a tools-only server must implement:**

- `initialize` — `params` required, carrying `protocolVersion`, `capabilities` (every field optional; a tools-only server can decode it as `json.RawMessage` and ignore it) and `clientInfo` (`name` and `version` required).
- `initialize` result — `protocolVersion`, `capabilities`, `serverInfo` required; `instructions` optional. A tools-only server emits `{"tools":{}}`; **presence of the key is the signal**, an empty object still means supported.
- `notifications/initialized` — **no `id`**, so the server must send nothing back. Replying to a notification is the classic hand-rolled bug.
- `tools/list` — paginated; result needs `tools` present, `nextCursor` optional.
- `Tool` — `name` and `inputSchema` required, the latter `{type: "object", properties?, required?}`. `ToolAnnotations` defaults are **not** the Go zero values: `destructiveHint` defaults **true** and `openWorldHint` defaults **true**, so expressing "this tool is safe" needs `*bool`, not `bool` + `omitempty`.
- `tools/call` — result needs `content` present (may be empty). **Tool-level failures go in `isError`, not as a JSON-RPC error** — only "tool not found" and genuinely exceptional conditions become protocol errors, so that the model can see the failure and correct itself. This distinction is worth carrying into cog's own error design: a provider that cannot capture right now is an `isError` result with a sentence explaining why, not a transport failure.
- `ContentBlock` is a five-way union on `type` (`text`, `image`, `audio`, `resource_link`, `resource`). Decoding it needs a two-pass unmarshal — but **a server only encodes content blocks**, so a server-only implementation skips the decoder entirely.

**Honest line counts**, stdio-only and tools-only: ~100 lines of type declarations (~155 with all five content-block kinds), ~160 lines of runtime — read loop, dispatch, notification detection, version negotiation, the three methods. **~250–350 lines total, zero dependencies.**

One trap worth recording: use `bufio.Reader`, not `bufio.Scanner`. Scanner's 64 KB default line cap will silently truncate a large `tools/list` response.

Adding HTTP: a minimal spec-legal streamable HTTP server — POST only, always `application/json`, `202` with empty body for notifications, `405` on GET (the spec explicitly permits both), `Origin` validation (a **MUST**, for DNS rebinding), localhost binding — is **~100–140 lines**. Sessions add ~80, real SSE streaming ~200, resumability ~200. So **~400–500 lines for stdio + stateless-JSON streamable HTTP**, and ~900–1,500 for the full thing.

For calibration: the official SDK's `mcp/streamable.go` is 102 KB and mcp-go's equivalent is ~107 KB across four files. That is full fidelity — resumability, five-revision back-compat, sampling round-trips, session sweeping — and a tools-only server needs none of it.

**The recurring cost is not the server, it is the schemas.** Every tool's `inputSchema` is hand-written JSON kept manually in sync with the Go struct that `arguments` unmarshals into. That drift is exactly what reflection removes, and cog's design — a broker rendering tools from typed capability interfaces — is the case where reflection pays most.

## 5. The client side: what Claude Code actually does

Read from Claude Code's own documentation (`code.claude.com/docs/en/mcp`) on 2026-09-10. This was the gap the first pass left open, and it turns out to carry the most consequential facts in this file.

### Attaching

```bash
claude mcp add --transport http <name> <url>
```

That is the one-liner. `--transport sse` exists for the legacy transport and is deprecated; Claude Code "tries the HTTP transport first and switches to SSE when the server doesn't accept it" (v2.1.265+). In JSON config, `type` accepts `streamable-http` as an alias for `http`.

Three scopes, in precedence order **local → project → user**:

| Scope | File | Meaning |
| --- | --- | --- |
| local (default) | `~/.claude.json` | this project only, private |
| **project** | **`.mcp.json` in the project root** | this project, **shared via version control** |
| user | `~/.claude.json` | all the user's projects, private |

**The project scope is directly useful to cog**: a game repo can commit a `.mcp.json` naming its own MCP endpoint, so an agent working in that repo finds the game without anybody running an `add` command. Claude Code "prompts for approval in interactive sessions before using project-scoped servers from `.mcp.json` files", which is the right amount of friction. This is a concrete answer to what [#206](https://github.com/dvoyni/cog/issues/206) asks about what a person must do once — possibly nothing, if the repo carries the file.

### Disappearing and coming back

This matters because **a game exiting is the normal case here, not an error**.

- **Mid-session drops** on remote servers: "Claude Code reconnects a dropped remote server with exponential backoff: up to five attempts, starting at a one-second delay and doubling it each time." `/mcp` shows the server as pending meanwhile; after five failures it is marked failed, with a manual retry available from `/mcp`.
- **First connection fails** with a transient error — 5xx, connection refused, timeout — retried up to three times. Authentication errors and 404s are not retried, since they need a config change.
- Status is surfaced as `✔ Connected` / `! Needs authentication` / `✘ Failed to connect` (with the HTTP status appended), and `claude mcp get <name>` shows an `Issue:` line carrying the server's error text.

So the loop cog wants — start the game, work, close the game, start it again — is **already handled by the client**, provided the port is stable across runs. That is an argument for a configured fixed port over an ephemeral one, and it belongs in [#206](https://github.com/dvoyni/cog/issues/206).

### Timeouts: the constraint on a blocking tool

| Variable | Meaning | Default |
| --- | --- | --- |
| `MCP_TIMEOUT` | server startup timeout | — |
| `MCP_TOOL_TIMEOUT` | per-server tool wall-clock limit | ~28 hours |
| **`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`** | **idle timeout before aborting a tool call** | **5 min** (HTTP/SSE/WS), 30 min (stdio) |
| `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS` | threshold for backgrounding a long call | 2 min |

There is also a per-request timer covering each request through to the server's **first response byte**, set to the greatest of 60 seconds, the server's tool timeout, and `MCP_TIMEOUT`. A per-server `timeout` field in `.mcp.json` overrides `MCP_TOOL_TIMEOUT` for that server.

**The map's settled capture contract — the tool blocks until the next frame — is comfortably inside all of this at 16ms.** But it is *not* fine in one case the map has already surfaced: a **paused** engine. A capture that waits for "the next frame" in a paused engine waits forever, and the client aborts it after five minutes of idle. [#211](https://github.com/dvoyni/cog/issues/211) and [#207](https://github.com/dvoyni/cog/issues/207) must agree on this, and the answer is now bounded by a real number rather than by taste: **a blocking capability needs its own deadline well under five minutes, and a clear error when it expires.**

### Output limits — this changes a decision

| Threshold | Value |
| --- | --- |
| warning shown | 10,000 tokens |
| **default maximum** | **25,000 tokens** (`MAX_MCP_OUTPUT_TOKENS`) |
| hard ceiling via per-tool annotation | 500,000 characters |

What happens past the limit is the striking part:

> "When a result with no image content exceeds the limit, Claude Code saves it to a file and replaces it in the conversation with a message that names the file path, so Claude reads the file when it needs the content."

**Claude Code already implements the map's path-on-disk pattern, automatically, for oversized text results.** A canvas op-queue snapshot that blows past 25,000 tokens does not need cog to invent spill-to-file — the client does it. That does not make the size question go away (a snapshot the agent must open a file to read is still worse than one it can skim), but it removes the failure mode where a huge snapshot wrecks a session.

A server can raise its own ceiling per tool:

```json
{ "name": "get_schema", "_meta": { "anthropic/maxResultSizeChars": 200000 } }
```

up to 500,000 characters, "useful for tools that return inherently large but necessary outputs, such as database schemas or full file trees" — a canvas queue snapshot is exactly that shape.

**And the exception is the decisive fact for [#207](https://github.com/dvoyni/cog/issues/207):**

> "Tools that return image data are still subject to `MAX_MCP_OUTPUT_TOKENS`" — the per-tool annotation does not apply to images.

So an inline image cannot buy itself more room the way a large text result can, **and** an oversized image result does not get spilled to a file, because the spill path is documented for results *with no image content*. Inline capture delivery is therefore capped at 25,000 tokens with no escape hatch and no graceful degradation. The map chose a path on disk to protect the context window; the client's own limits make that choice close to forced rather than merely prudent.

### Management surface

`claude mcp list` (status per server), `claude mcp get <name>` (config, status, `Issue:` line), `claude mcp remove <name>`, and `/mcp` in-session for status, toggling, reconnecting and viewing tool counts. Worth knowing that `/mcp` shows tool counts, since it is where a user would notice a broker exposing more tools than they expected.

## 6. What this means for the map

Not decisions; those belong to the tickets. But the facts point somewhere:

1. **Reflection-based schema inference is available and idiomatic**, in both the official SDK and mcp-go. The map's settled design — providers hand over typed Go values, the broker renders tools — lines up with `AddTool[In, Out]` almost exactly: a capability's request struct *is* the tool's input schema, and `jsonschema:"..."` tags are where a description lives. This is the strongest single argument against hand-rolling.
2. **Stateless mode and the 2026-07-28 rewrite bear directly on [the transport ticket](https://github.com/dvoyni/cog/issues/206).** A stateless server cannot make server→client requests at all. If cog ever wants to *push* to a connected agent — "the frame you asked about has changed" — that closes the door, and the door is already closing anyway, since the newest revision requires stateless for streamable HTTP.
3. **The list-changed capability has to be declared before `Connect`**, because a broker learns its tool set at `Start` when it collects providers. A server that discovers providers after connecting and never declared `HasTools` will silently fail to tell anyone.
4. **`isError` versus a protocol error is a distinction cog should mirror.** The provider contract ticket asks what a capability returns when it cannot answer; the protocol has already answered the analogous question, and matching it means the agent gets a readable sentence instead of a transport fault.
5. **Dependency cost is real but small**: one direct edge, six runtime deps, on a module that has seven today. Worth stating in the spec as a deliberate acceptance rather than leaving someone to discover it.
6. **Inline image delivery is capped and cannot be raised.** `MAX_MCP_OUTPUT_TOKENS` defaults to 25,000; a per-tool `anthropic/maxResultSizeChars` annotation can raise a *text* result to 500,000 characters, but explicitly does not apply to images, and the automatic spill-to-file is documented only for results with no image content. The map chose a path on disk to protect the context window; the client's limits make it the only delivery that degrades gracefully. -> [#207](https://github.com/dvoyni/cog/issues/207)
7. **A blocking capability needs a deadline under five minutes.** `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT` defaults to five minutes for HTTP servers, so a capture waiting for "the next frame" in a paused engine is aborted by the client rather than hanging forever. That is a real number for [#207](https://github.com/dvoyni/cog/issues/207) and [#211](https://github.com/dvoyni/cog/issues/211) to agree against.
8. **A game repo can ship its own `.mcp.json`.** Project scope is shared through version control and gated by an approval prompt, so "what does a person have to do once" may be answerable with *nothing*, provided the port is stable across runs — which argues for a configured fixed port over an ephemeral one. -> [#206](https://github.com/dvoyni/cog/issues/206)

## Gaps

Named plainly. Claude Code's client side, open after the first pass, is now covered in section 5.

- **No conformance testing of any SDK against a real client** — everything above is read off source and docs.
- **Per-person maintainer roster** for the official SDK could not be established from primary sources; `CONTRIBUTING.md` names institutions only.
- Facts are read off `main` (pushed 2026-09-07) except where a tag is named. `ServerOptions.SupportedProtocolVersions` and some `MCPGODEBUG` removals are **v1.8.0-pre material, not in stable**. The SDK's `ROADMAP.md` is stale and should not be relied on.
