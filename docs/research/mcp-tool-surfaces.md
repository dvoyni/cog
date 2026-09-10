# What makes an agent-facing tool surface usable

Research for [research: what makes an agent-facing tool surface usable](https://github.com/dvoyni/cog/issues/202), a ticket on the map [mcp: an agent-facing extension point, and the broker that serves it](https://github.com/dvoyni/cog/issues/199).

**Gathered 2026-09-10**, extended the same day with the browser servers, the MCP project's own material, and the serialisation literature. This exists because the map builds **no prototype**: nothing in the effort ever watches an agent try to use these tools, so prior art is the only evidence available before the specs are written. Section 7 is where that decision starts to hurt.

Counts and quotes below come from reading registration source files and official vendor documentation directly. Where a number is a README claim rather than something counted from source, it says so. **Section 9 lists what was not reached** — the gaps are load-bearing and should not be papered over.

## The short version

Sixteen servers read from source across browsers, engines and design tools, five of them first-party; plus the MCP specification, three vendors' published design guidance, and six measured studies on serialisation. What the evidence supports:

- **The browser servers are the answer to this ticket** (§2). Microsoft's Playwright MCP and Google's `chrome-devtools-mcp` solve cog's exact problem, and they agree with each other: hand the model an **indented accessibility tree with an opaque handle per line**, never a picture, and say so in the description string.
- **The state comes back with the action, but by reference.** Playwright auto-attaches a fresh snapshot to every click, drag and navigation — and **writes it to a file**, returning a link plus a one-line header. Inline only when the agent explicitly asked. That single pattern reframes [queue capture](https://github.com/dvoyni/cog/issues/208).
- **Trees and logs want different machinery.** Every server here paginates flat sequences and drills in by index; none prunes one. Trees get pruned, rooted and depth-capped by the servers that can tell which nodes are decorative, and paged one level at a time by the one that cannot. cog has one of each shape.
- **The failure mode to design against is the silent truncation.** Blender — the most-used server in this space — caps its scene at ten objects with the true count printed beside it, no cursor, no flag, no way to reach the eleventh (§3.3).
- **Compression and addressing are separable.** Playwright's tree distiller drops nodes from the rendered text while leaving every ref resolvable — "refs of removed nodes still resolve". A big tree can be made small to read without being made small to act on.
- **When a team actually needs a tree to fit, they drop a field.** Four independent parties — Playwright's distiller, Unity's commented-out transform, Coplay's opt-in transform, Anthropic's measured 206→72 tokens — reached for omission, not for a cleverer encoding. The measured literature agrees (§7).
- **Google publishes the only hard numbers**: 2 MB as the inline-image/file-path cutover, and — counter-intuitively — "image tokens scale with **dimensions** rather than encoded bytes", which kills the idea of a quality knob on a capture request.
- **The MCP project publishes no guidance on writing tool descriptions.** The spec defines the field in one clause, never says how to fill it, and its docs site delegates the question to an Anthropic plugin. Nothing upstream will decide any of this for cog.
- **Google and Anthropic contradict each other on granularity** — "composable tools, not magic buttons" versus "consider a `schedule_event` tool" — and each is right about its own problem. cog is standing where Google is.
- **Two mature DCC servers ship the same arbitrary-code escape hatch and give opposite advice about it**, in the description text the model reads. Playwright supplies a third stance, Chrome a fourth, and Unity a fifth that dissolves the argument: a *structured* hatch whose calling convention carries undo and object tracking (§3.2).
- **There is no convergent granularity norm** — 6 tools to ~1,091 among first parties — but there *is* a unanimous meta-norm: **the full registry is never the default surface.** Four official or canonical servers, four different mechanisms for shrinking it, including one that lets the agent change its own surface at runtime.
- **On serialisation there is real measured evidence, and it is deflationary** (§7): format choice is a cost lever, not an accuracy lever; rankings reverse between models; and progressive disclosure is scale-conditional, with hierarchy actively harmful.
- **Two first parties have now deprecated their own MCP server in favour of a CLI** — Microsoft on token grounds, Unity on iteration speed and stability (§2.8, §3.1). Neither says MCP is wrong. Both say it is the wrong default for an agent that is mostly writing code, and [the map](https://github.com/dvoyni/cog/issues/199) should hold that consciously.

## 1. Granularity: no norm, and first parties disagree

| Server | Tools | Shape | Escape hatch |
| --- | --- | --- | --- |
| Epic / Unreal 5.8 (**official**) | ~1,091 in 52 toolsets | narrow, one per operation | none needed |
| Chrome DevTools (**official**) | 61 defined, **57 default**, **3 in `--slim`** | narrow, one per operation | `evaluate_script`; removable via `--no-javascript-evaluation` |
| Figma (**official**) | 34 | narrow reads, **one mega write tool** | — |
| Playwright (**official**) | 80 defined, **24 default** | narrow, capability-gated | `browser_run_code_unsafe` + `browser_evaluate` |
| Roblox Studio (**official**) | 6 | everything behind `run_code` | *is* the escape hatch |
| Unity `com.unity.ai.assistant` (**official, deprecated**) | 20 defined, **1 default** | coarse action-enum | `Unity.RunCommand` — *the only tool on by default* |
| Unity `CoplayDev` | 48 defined, **30 default** | coarse action-enum + 27 resources | `execute_code` — **off by default** |
| Unity `CoderGamester` | 34 | **narrow, one per operation** | none but `execute_menu_item` |
| Blender `ahujasid` | 28, of which **21 are asset marketplaces** | *is* the escape hatch | `execute_blender_code` |
| Houdini `capoomgit` | 25 | narrow, one per graph op | `execute_houdini_code` |
| Cinema 4D `ttiimmaacc` | 25 | narrow, noun-shaped | `execute_python_script` |
| Maya `PatrickPalmer` | 18 | hybrid; `mesh_operations` is an 8-way action enum | none |
| Godot `Coding-Solo` | 14 | narrow, one per operation | none exposed |
| Unreal `flopperam` | 43 | hybrid + task-level macros | none |
| Unreal `chongdashu` | 30 | narrow | **none — hard capability ceiling** |
| Unreal `runreal` | 20 | coarse | `editor_run_python`, `editor_console_command` |
| Unreal `kvick-games` | 8 | very broad | `execute_python` |

The inverse correlation is the pattern: **the fewer tools a server has, the more likely it ships an arbitrary-code escape hatch**, and the servers with the most tools need none. `chongdashu` is the cautionary case — 30 narrow tools and no escape hatch means anything not on the list is simply impossible, and its README advertises viewport control that the source shows commented out as buggy.

**The browser servers break the correlation in a way that matters.** Both are large *and* ship a hatch — and both then shrink what they advertise. Chrome's `--slim` mode is the correlation restated as a switch: compress to three tools and the a11y snapshot is the first thing to go, an escape hatch is what replaces it, and the result is Roblox's shape reached from the opposite direction. §2.7 has the counts.

**Epic's tool-search mode** is the one architectural answer anybody has published. From Epic's own documentation:

> "By default, the plugin runs in tool-search mode (`bEnableToolSearch = true`). In this mode, `tools/list` returns three discovery meta-tools rather than every advertised Tool"

The three are `list_toolsets`, `describe_toolset` and `call_tool`, and the stated rationale is that the agent "walks this discovery path on demand, **which keeps `tools/list` responses small even when the registry exposes hundreds of Tools**." Epic also ships *zero* tools in the core plugin — toolsets are a separate plugin — which is the same broker-and-providers split this map is designing.

**Figma is the instructive hybrid.** Reads are many narrow tools organised around a hub, with an explicitly cheaper fallback for size:

> "`get_design_context` is the default entry point: the other tools in this group are either inputs to it, or fallbacks when a selection is too large or you need a specific asset format."

`get_metadata` is that fallback, documented as "Useful for very large designs where `get_design_context` produces output with a large context size." Writes, by contrast, are consolidated behind a single `use_figma` covering Design files, FigJam boards and Slides decks.

**Consolidation happens, and does not always stick.** Figma deprecated four per-kind shader tools (`list_shader_effects`, `list_shader_fills`, `get_shader_effect`, `get_shader_fill`) into two, and renamed `get_code` to `get_design_context`. `flopperam` documents a reduction "from 44 to 21 tools" — but the stated motive is "reduce complexity", **not** context limits, and the count has since drifted back to 43 with the removed tools re-added.

## 2. The browser servers: the closest analogue to what cog is building

Two official servers — Microsoft's [Playwright MCP](https://github.com/microsoft/playwright-mcp) and Google's [`chrome-devtools-mcp`](https://github.com/ChromeDevTools/chrome-devtools-mcp) — solve exactly cog's problem: an agent acting on live, changing state it cannot see, which has to be handed to it as a tree. They were read from source, and they agree with each other on nearly everything. **This section is the most load-bearing in the file.**

Playwright MCP's implementation no longer lives in `microsoft/playwright-mcp` — the repo is a thin package, and `src/README.md` says so: "Playwright MCP source code is located in the Playwright monorepo." Everything below was counted and quoted from `packages/playwright-core/src/tools/` in `microsoft/playwright` at `af74c93`, cross-checked against the shipped `@playwright/mcp@0.0.80` README (which is generated from source by `update-readme.js`). Chrome's is `chrome-devtools-mcp@1.9.0`.

### 2.1 Both hand over an accessibility tree as indented text, and both say why in the description

The rationale is not buried in a design doc — it is in the description string the model reads on every call.

Playwright's two tools argue with each other on purpose:

> `browser_snapshot`: "Capture accessibility snapshot of the current page, **this is better than screenshot**"
> `browser_take_screenshot`: "Take a screenshot of the current page. **You can't perform actions based on the screenshot, use browser_snapshot for actions.**"

Chrome says the same and adds a staleness rule:

> `take_snapshot`: "Take a text snapshot of the target page based on the a11y tree. The snapshot lists page elements along with a unique identifier (uid). **Always use the latest snapshot. Prefer taking a snapshot over taking a screenshot.**"

And Playwright's README states the position as product framing: the server "enables LLMs to interact with web pages through structured accessibility snapshots, **bypassing the need for screenshots or visually-tuned models**", because it is "**Fast and lightweight**. Uses Playwright's accessibility tree, not pixel-based input", "**LLM-friendly**. No vision models needed", and "**Deterministic tool application**. Avoids ambiguity common with screenshot-based approaches."

Note what the stated reason actually *is*. Only one of those four bullets is about cost. The others are about **actionability and determinism**: a picture cannot be clicked, and a tree can, because every line carries a handle.

**The two implementations converged on the same line format independently.** Playwright emits a YAML-ish list; Chrome emits bare indented lines:

```
- button "Submit" [active] [ref=e2]          # Playwright
  uid=1_3 button "Submit"                    # Chrome DevTools
```

Both are: one line per node, indentation for depth, role, accessible name in quotes, bracketed state, and an opaque handle. Neither hands the model JSON. Chrome computes JSON as well — `SnapshotFormatter` has both `toString()` and `toJSON()` — and puts it in MCP `structuredContent` while the *text* the model reads stays the indented form. Playwright will emit `ariaSnapshotJSON` only when the caller passes an internal `_meta.json` flag, which the Playwright CLI uses and MCP clients do not.

### 2.2 Refs are generation-stamped, and going stale is a loud, actionable error

Chrome mints uids as `<snapshotId>_<counter>`, keeps only the latest snapshot's map on the page object, and fails a stale reference by name:

> `No snapshot found for page {id}. Use take_snapshot to capture one.`
> `Element uid "{uid}" not found on page {id}.`
> `Element with uid {uid} no longer exists on the page.`

Playwright resolves `target: 'e2'` through a dedicated `aria-ref=` selector engine, and its element parameter is documented as "Exact target element reference from the page snapshot, or a unique element selector" — so a handle and a real selector are interchangeable in the same argument.

For cog: a ref into a captured queue or element tree has to be scoped to the capture that minted it, and reusing an old one must fail by name and name the tool that refreshes it — not silently address the wrong op.

### 2.3 The size machinery, and the tree/log split

Between them the two servers ship five distinct size mechanisms, and they apply them to two different shapes of data. **The split is the finding.**

**Trees are pruned, rooted, depth-capped, searched, or dumped to a file — never paginated:**

| Mechanism | Where |
| --- | --- |
| Prune by semantics, by default | Chrome `verbose` defaults to `false`, which sets Puppeteer's `interestingOnly: !verbose`; Playwright's distiller (below) |
| Root the snapshot at a subtree | Playwright `browser_snapshot`'s `target` |
| Cap depth | Playwright `depth`: "Limit the depth of the snapshot tree" |
| Search instead of dumping | Playwright `browser_find` |
| Redirect to disk | Playwright `filename`, Chrome `filePath` — both documented as "instead of returning it in the response" / "instead of attaching it to the response" |

**Flat logs are paginated and drilled into by index — never pruned:**

Chrome's `list_network_requests` and `list_console_messages` take `pageSize` and `pageIdx` ("Maximum number of requests to return. When omitted, returns all requests." / "Page number to return (0-based)") plus a `resourceTypes` filter, with `get_network_request` for the detail. Playwright does it with a numbered list and states the follow-up in the description:

> `browser_network_requests`: "Returns a **numbered list** of network requests since loading the page. **Use browser_network_request with the number** to get full details."

Neither server paginates a tree. Neither prunes a log. That maps onto cog: **a canvas op queue is a flat log — paginate it and drill in by index; a ui element tree is a tree — prune it, root it, and hand back refs.** Two different capabilities, not one tool with a filter argument. (§3.3 supplies the caveat: pruning a tree needs a rule for which nodes are decorative, which an accessibility tree has and a scene graph does not. Where no such rule exists, page the tree — do not truncate it.)

`browser_find` deserves its own beat. It is `grep -C 3` over the snapshot text, coalescing overlapping windows, reconstructing each match's ancestor path, and marking elided siblings with `...`. Its description states the reason outright:

> "Search the accessibility snapshot of the current page for text or a regular expression. Returns matching snapshot nodes with a few lines of surrounding context (like search snippets), each shown under its path from the root of the tree, **which is cheaper than capturing the whole snapshot when you only need to locate an element and its ref**."

And note the second-order fact: **`browser_find` works because the snapshot is indented text.** Ancestor reconstruction is `indentOf(line)` arithmetic. On a JSON tree this tool would be a different, harder tool. That is a shipped, working argument for the text form over JSON, rather than an assertion about token counts.

### 2.4 Playwright distils the tree rather than truncating it — and keeps the refs

`packages/injected/src/ariaSnapshotDistiller.ts` is 263 lines whose only job is making the tree smaller for a model. It is a babel-style visitor pipeline with two presets: `normalizePlugins` for everyone, and an `ai` preset that runs only for `mode: 'ai'` — the mode the MCP server always requests. The header comment states the contract:

> "Distillation makes the snapshot **less verbose without losing information** … Plugins mutate the tree in place; `snapshot.info` and `snapshot.refs` are left intact, **so refs of removed nodes still resolve through the aria-ref selector engine**."

That second clause is the design insight. **The rendered tree is lossy; the ref namespace is not.** A node can vanish from the text the model reads and still be addressable by the handle it would have carried. Compressing the view does not shrink the action surface.

The five `ai`-only plugins are all semantic-redundancy elimination, not truncation:

- `removeNamelessImages` — a decorative `img` with no name and no content, unless it is the click target
- `removeRedundantNames` — an accessible name derived from content that is already rendered elsewhere in the output
- `inlineTextIntoGeneric` — "`generic: - generic: "text"` becomes `generic: "text"`"
- `removeNameRepeatingChild` — a wrapper whose only text repeats the parent's name "adds no information, so it removes itself"
- `unwrapSingleChildGenerics` — a generic enclosing exactly one ref-bearing child is replaced by that child

The unifying rule is **drop what the model can already read somewhere else in the same output** — not "drop past depth N", not "drop past byte N". Depth capping exists, but as a separate caller-supplied knob (`maxDepth`), not as the compression strategy. Chrome's `interestingOnly` is the same idea implemented more crudely.

This is a third answer to "how do you fit a big live tree into a context window", distinct from both truncation and pagination, and it is the one both browser vendors reached.

### 2.5 Screenshot delivery: Google publishes a hard threshold and the reason behind it

Chrome's `take_screenshot` handler is a three-way branch on size, with the number in source:

```ts
if (request.params.filePath)            { saveFile(...);          "Saved screenshot to X." }
else if (screenshot.length >= 2_000_000) { saveTemporaryFile(...); "Saved screenshot to <path>." }
else                                     { response.attachImage({ mimeType, data: base64 }); }
```

**2 MB is the documented cutover from the protocol's image channel to a path on disk.** It is the only concrete threshold found anywhere in this research.

Chrome's configuration docs then state the mechanism, and it is counter-intuitive enough to be worth quoting exactly:

> "JPEG and WebP are ~3-5x smaller than PNG, which reduces **transfer and storage size**."
> "**To reduce context size use `--screenshotMaxWidth` / `--screenshotMaxHeight`, since image tokens scale with dimensions rather than encoded bytes.**"

Re-encoding buys bandwidth, not context. Only downscaling buys context. For [the capture request](https://github.com/dvoyni/cog/issues/207) that means a resolution cap is the parameter that matters and a quality/format knob is not, and it disposes of the intuition that a smaller file is a cheaper file.

Playwright's rule is different and also principled: it **always** writes the file and returns `- [Screenshot of viewport](path)`, and *additionally* attaches the inline image only when the caller supplied no `filename`. An explicit filename is read as "I want the artifact", so the picture is not also spent on context. Above that sits `imageResponses`, documented as defaulting to "auto, which sends images if the client can display them" — delivery negotiated against client capability rather than fixed by the server.

Chrome's `--slim` mode drops the inline path entirely: its `screenshot` handler writes a temporary file and appends the `filepath`, nothing else.

Three official servers, three variants of the same conclusion, and none of them inlines an image unconditionally.

### 2.6 The read-and-act loop: state comes back with the action, unasked — but by reference

Neither server makes the agent snapshot again after acting. In Playwright every mutating tool calls `response.setIncludeSnapshot()` *before* doing the work — `browser_click`, `browser_drag`, `browser_hover`, `browser_select_option`, `browser_type`, `browser_fill_form`, `browser_navigate`, `browser_wait_for`. Chrome's `input.ts` calls `response.includeSnapshot()` eight times, once per input tool.

**And Playwright then writes that automatic snapshot to a file rather than inlining it.** In `Response._build()`:

```ts
const snapshotToFile = this._includeSnapshot !== 'explicit' || !!this._includeSnapshotFileName;
```

`setIncludeSnapshot()` — the path every action takes — sets the mode to `'full'`, never `'explicit'`, so the post-action branch always resolves to a file and the response carries `- [Snapshot](page-001.yml)`. Only an explicit `browser_snapshot` **with no filename** inlines the YAML. The repo's own test fixture confirms it by reading the snapshot back off disk through the returned path.

So the loop the agent actually sees after a click is: URL, title, HTTP status if not 2xx, `Console: 2 errors, 0 warnings`, the equivalent Playwright code, and a *link* to the new tree. **The state is always delivered; its bulk is not always spent.** That is the single most transferable pattern in this file for [queue capture](https://github.com/dvoyni/cog/issues/208) — the choice is not "return the queue or don't", it is *return it by value or by reference*, decided by who asked and how.

The envelope is a fixed set of named sections, the same skeleton for every tool: `### Error`, `### Result`, `### Ran Playwright code`, `### Open tabs`, `### Page`, `### Modal state`, `### Snapshot`, `### Events`, `### Paused`. Empty sections are omitted. `### Page` is the cheap always-on header — a summary line, never the log.

### 2.7 Counts, and the fact that neither default surface is the full registry

| Server | Defined in source | Default `tools/list` | Shrink mechanism |
| --- | --- | --- | --- |
| Playwright MCP (**official**) | **80** `defineTool`/`defineTabTool` in `tools/backend/*.ts` | **24** | `filteredTools` keeps only `capability` starting with `core`, and drops `skillOnly` |
| Chrome DevTools MCP 1.9.0 (**official**) | **61** `defineTool`/`definePageTool` in `src/tools/` | **57** (4 gated on `--devtools-comments`) | `--slim` replaces the whole set with **3** |

Playwright's 24 matches its generated README exactly (23 "Core automation" + 1 "Tab management"). The other 56 sit behind `--caps=vision,pdf,devtools,storage,network,testing,config`, or are `skillOnly` — defined for the Playwright CLI's skill, never advertised over MCP at all. Chrome's `--slim` is documented as "Exposes a 'slim' set of 3 tools covering navigation, script execution and screenshots only."

**Three official servers, three mechanisms, one shared premise: the full registry is not the default surface.** Epic hides it behind discovery meta-tools, Playwright gates it by capability, Chrome offers a three-tool mode. Section 1's "no convergent granularity norm" survives — but the *meta*-norm is real and unanimous.

And notice what slim mode *is*: navigate, screenshot, and arbitrary `evaluate`. When Google compresses its own surface to three tools, the a11y snapshot is the first casualty and an escape hatch is what replaces it — Roblox's shape, reached independently from the opposite direction.

### 2.8 The vendor recantation

The most uncomfortable finding here. Microsoft's own Playwright MCP README opens by steering coding agents *away* from it:

> "Modern **coding agents** increasingly favor CLI–based workflows exposed as SKILLs over MCP because CLI invocations are more token-efficient: they avoid **loading large tool schemas and verbose accessibility trees into the model context**, allowing agents to act through concise, purpose-built commands."
>
> "MCP remains relevant for specialized agentic loops that benefit from **persistent state, rich introspection, and iterative reasoning over page structure**, such as exploratory automation, self-healing tests, or long-running autonomous workflows where **maintaining continuous browser context outweighs token cost concerns**."

The party that popularised the accessibility-tree snapshot now describes it as a bill, and names the conditions under which paying it is right. Those conditions — persistent state, iterative reasoning over structure, a long-running loop — are a fair description of an agent iterating on a cog frame, which is the case *for* the map. But the argument only holds if the surface can say what the bill buys. It is a caution against a fat default snapshot, not against the design.

## 3. The engine servers: where structure handover goes wrong

Blender, Unity and Godot were the first pass's other named gap. Five servers were read from source. They matter less than the browser servers for tree design — because most of them barely attempt it — but three findings are load-bearing, and one is uncomfortable.

Canonicality, since the ticket asked. Blender is `ahujasid/blender-mcp` — 28.0k stars, twice the next server in this space and uncontested in its own. Unity is `CoplayDev/unity-mcp` at 14.1k for the community server (7× `CoderGamester/mcp-unity`'s 1.9k, though both are active, and CoderGamester is included below because it is the only narrow-granularity engine server here), **plus a first-party one that neither of them is**. Godot is `Coding-Solo/godot-mcp` at 5.6k — canonical by stars, the nearest open-source contender being around 182 — but last committed 2026-04-16, at version 0.1.1, and by far the thinnest server in this file.

### 3.1 Unity ships a first-party MCP server, and has deprecated it in favour of a CLI

`com.unity.ai.assistant` (module `Unity.AI.MCP.Editor`, Unity 6+) carries this notice, `[!include]`-ed into every one of its ten MCP documentation pages:

> "Unity MCP server is deprecated. Use the [Unity command-line interface (CLI)](https://docs.unity.com/en-us/unity-cli) instead. Unity CLI provides **faster iteration times, improved stability, and the ability to target runtime and the Editor**."

It is present at 2.18.0-pre.2 and 2.19.0-pre.2, absent at 2.10.0-pre.1, and **never mentioned in the CHANGELOG** — Unity deprecated its MCP server without a changelog entry.

**This is Microsoft's recantation (§2.8) a second time, from a second first party, for overlapping reasons.** Microsoft steers coding agents to a CLI plus a skill on token grounds; Unity steers to a CLI on iteration-speed and stability grounds. Two of the four first-party servers in this file now point away from MCP. That is a fact [the map](https://github.com/dvoyni/cog/issues/199) should hold consciously rather than discover later: neither party says MCP is wrong, both say it is the wrong default for an agent that is mostly writing code — and both name the same escape route, which is a command line the agent already knows how to drive.

### 3.2 Unity's official server ships one tool enabled by default, and it is the escape hatch

`McpToolAttribute.EnabledByDefault` is documented in source as marking "the curated default set", with "New or unvalidated tools should leave this as false." Grepping the 20 tools for `EnabledByDefault = true` returns exactly one hit: `Unity.RunCommand`. The vendor with the most to gain from a rich structured surface built 20 tools, defaulted 19 of them off, and left arbitrary C# as the surface.

But **it is a structured escape hatch, and that is a genuinely new idea** for §4. `RunCommand`'s description is a template with rules, and the template exists to make the agent's arbitrary code participate in the editor's own bookkeeping:

> "1. **Class Name is Mandatory**: The class MUST be named `CommandScript`…
> 3. **Use the `result` Object**:
>    - **Creation**: Use `result.RegisterObjectCreation(obj)` after creating objects.
>    - **Modification**: Use `result.RegisterObjectModification(obj)` BEFORE changing properties.
>    - **Deletion**: Use `result.DestroyObject(obj)` instead of `Object.DestroyImmediate`."

Recall Houdini's argument for narrow tools (§4): validation, structured errors, and single-step undo — "properties the wrapper *adds*". Unity's answer is to get undo and object tracking **without** the wrapper, by making the escape hatch's calling convention carry them. The tool declares a real output schema (`isCompilationSuccessful`, `isExecutionSuccessful`, `executionId`, `compilationLogs`, `executionLogs`, `localFixedCode`) and compiles before it runs. This does not change cog's position — arbitrary dispatch is out of scope — but it dissolves the §4 contradiction's premise, which was that you must choose between a hatch and the properties a wrapper adds.

### 3.3 Structure handover is where every engine server is weakest, and they fail differently

| Server | Shape of the scene handed over | Cap |
| --- | --- | --- |
| Unity `CoplayDev` | Paged one-level summary, cursor-driven | `pageSize` 50 (max 500), `maxNodes` 1000 (max 5000), with `truncated`, `next_cursor`, `total` |
| Blender | Flat JSON list, **no hierarchy at all** | **Hard 10 objects, silently** |
| Unity (**official**) | Recursive JSON tree, `children` nested to full depth | **None** — default depth `-1` |
| Unity `CoderGamester` | Whole scene, `JSON.stringify(hierarchy, null, 2)` | **None**; the tool's schema is `z.object({})` |
| Godot | `{scenes: 12, scripts: 30, assets: 88, other: 5}` | **No scene structure exists** |

**Blender's cap is the anti-pattern worth naming.** From the addon source:

```python
# Collect minimal object information (limit to first 10 objects)
for i, obj in enumerate(bpy.context.scene.objects):
    if i >= 10:  # Reduced from 20 to 10
        break
```

`object_count` reports the true total; the list silently stops at ten; there is **no cursor, no `truncated` flag, and no supported way to reach objects 11..N** except writing Python. The cap was tightened from 20 to 10 over time, so the pressure was real and the response was to truncate harder rather than to paginate. The most-used server in this space cannot show an agent a scene of thirty objects.

**Coplay's is the one to copy, and it independently matches Chrome's log pagination** (§2.3) — `cursor`, `next_cursor`, `truncated`, `total`, one level at a time, `// We do not inline children in summary mode`, `componentTypes` as type names only ("lightweight - no full serialization"), and **transform opt-in** via `include_transform` defaulting to false. Its `find_gameobjects` completes the pattern: "Returns instance IDs only (paginated). Then use `mcpforunity://scene/gameobject/{id}` resource for full data."

And note *how* Unity's own server tried to economise — by dropping a field, with the reason left in a comment beside the commented-out code:

```csharp
// Remove transform information since large scenes can have a lot of objects and this will
// be too much data
```

That is §7's finding again, arrived at by a third party independently: **when a team actually needs a tree to fit, the lever they reach for is omitting a field.** Unity dropped transform; Coplay made it opt-in; Playwright's distiller drops redundant names; Anthropic's measured 206→72 came from dropping identifiers. Four independent parties, one move.

### 3.4 Images: Blender is the only engine server that shows the agent the viewport

`get_viewport_screenshot` returns FastMCP's `Image` — a real `ImageContent` block, not a path — and the addon deliberately renders through `GPUOffScreen.draw_view3d` rather than grabbing the OS framebuffer, because the latter "is all-black whenever the Blender window is not composited in the foreground (the normal case when Blender is driven headless-style via MCP)." Worth remembering for cog: **a headless or backgrounded host is the normal case, and the naive capture path is the one that returns black.**

Coplay's `manage_camera` also returns real `ImageContent`, and two of its defaults corroborate §2.5 exactly: `include_image` is **false** by default (otherwise the screenshot is only written to `Assets/Screenshots`), and `max_resolution` defaults to **640** — a dimension cap, which is the parameter Chrome's docs say is the one that reduces context. Its multi-view mode (`batch='surround'`, six angles) returns several `ImageContent` blocks interleaved with `[Angle: ...]` labels, and strips `imageBase64` from the text payload so the bytes appear exactly once. Unity's own server has no viewport tool at all — its only pixels are an asset thumbnail as `previewBase64` inside a JSON field, which is Cinema 4D's anti-pattern (§5) in a different costume. Godot and CoderGamester return no images of any kind.

### 3.5 Blender's read-and-act loop is the worst in this file, and the author knows

`execute_blender_code` returns `f"Code executed successfully: {result.get('result', '')}"`, where `result` is *captured stdout only* — no new scene state, no object list, no diff. The compensation lives entirely in an MCP prompt:

> "- Always take a screenshot after completing a task to verify the visual result
> - Always call `get_scene_info()` after completing a task to verify the changes worked
> - When executing multiple operations, take intermediate screenshots to confirm each step"
>
> "- Use `get_viewport_screenshot()` **BEFORE** making changes to see the current state
> - Use `get_viewport_screenshot()` **AFTER** executing code or importing assets to verify the result"

So the loop is: mutate blind → screenshot → re-read a ten-object summary. Compare Playwright, where the new tree arrives attached to the action that changed it (§2.6). **Both engine servers that return state on mutation** — Unity official and Coplay both echo the serialized object back — **do it for the mutated object only**, so any wider change still needs a re-query. Nobody outside the browser servers auto-attaches the surrounding state.

### 3.6 Coplay publishes the only quantified argument for hiding tools

And it is the fourth mechanism for §2.7's meta-norm. `manage_tools` lets the *agent* enable and disable tool groups at runtime — an agent-mutable tool surface, which none of Epic, Playwright or Chrome offers. The stated reasoning:

> "MCP for Unity ships 47 tools, but exposing all of them to the LLM at once **balloons the prompt and dilutes routing decisions**."
> "**Prompt economy**: each visible tool adds tokens to every assistant call. Hiding what you're not using is real money saved at scale."
> "**Routing clarity**: when the LLM picks between 47 tools versus the 30 core tools, **the wrong-tool rate drops measurably**."
> "When a group's tools are confusing the assistant — e.g., `manage_shader` and `manage_material` both apply to materials in different ways — disabling the one you're not using keeps the assistant focused."

**Caveat it properly: "drops measurably" is asserted with no number, no method, and no published data.** It is the only claim about wrong-tool rate found anywhere in this research, and it is still a vendor assertion. What it is good for is naming the failure mode — *overlapping* tools confusing the router — which is a sharper concern than tool count, and which lands on cog if a cheap summary capability and a full capture capability ever answer the same question.

Coplay's in-repo `CLAUDE.md` adds two rules worth carrying: "Each MCP tool does one thing well. **Resist the urge to add 'convenient' parameters that bloat the API surface**", and "Use Resources for Reading … **Keep them smart and focused rather than 'read everything' type resources.**"

One last axis nobody else raised: **round trips.** Both Unity community servers ship a `batch_execute`, described as reducing "latency and token costs by **10-100x** compared to sequential tool calls", capped at 100 operations, with `stopOnError` and an `atomic` mode that "rolls back all operations if any fails (uses Unity Undo system)". The 10-100× is unsubstantiated, but the observation behind it is not: an agent building a scene issues many near-identical calls, and each round trip re-pays the response envelope. cog's ui and canvas capabilities will have the same shape.

## 4. The escape-hatch contradiction

Two mature DCC servers ship an identical arbitrary-Python tool and reach opposite conclusions, each stated in the description the model actually reads.

**Houdini — last resort:**

> "Execute arbitrary Python code in Houdini's environment. LAST RESORT: prefer the dedicated tools (connect_nodes, set_parameters, create_wrangle, get_geometry_info, ...) — they validate input, report structured errors and are undoable as a single step. Use this only for operations no dedicated tool covers."

**Cinema 4D — first resort:**

> "Execute a Python script in Cinema 4D's Python environment. **This is the most reliable tool for non-trivial operations — it gives full access to the c4d API and avoids wrapper/schema mismatches that can affect other tools.**"

The browser servers supply a **third stance: ship it, and put the risk in the description rather than an opinion about when to use it.** Playwright's is named for the danger and says nothing about preference:

> `browser_run_code_unsafe`: "Run a Playwright code snippet. **Unsafe: executes arbitrary JavaScript in the Playwright server process and is RCE-equivalent.**"

It also ships the narrower `browser_evaluate` ("Evaluate JavaScript expression on page or element"), so the escape hatch and a scoped version of it coexist without either claiming primacy. Chrome goes further still: `--no-javascript-evaluation` **removes** the escape hatch from the surface as a deployment choice, documented as disabling "evaluation tools (`evaluate_script` and slim `evaluate`), the `initScript` parameter in `navigate_page`, and navigating to `javascript:`, `data:`, or `vbscript:` URLs". That is the only server found that treats the escape hatch as an operator-controlled capability rather than a fixed part of the surface — which is a real option for cog if an app-plugin hatch ever opens.

The disagreement is not really about escape hatches; it is about **whether the narrow wrappers are trustworthy**. Houdini's argument for narrow tools is validation, structured errors and single-step undo — properties the wrapper *adds*. Cinema 4D's argument against them is schema drift — the wrapper falling out of sync with the thing it wraps.

For cog this cuts a specific way. The map has already ruled arbitrary command dispatch out of scope, so there is no escape hatch to argue about — which means **cog is buying Houdini's argument and taking on Houdini's obligation**: the typed capabilities have to validate, report structured errors, and not drift. The C4D failure mode is the one to design against, and cog's position is better than either, because a capability rendered from a Go type by reflection cannot drift from the type it was rendered from.

## 5. Visual return: the servers that thought about it decide per call

| Server | How a picture comes back |
| --- | --- |
| Chrome DevTools (**official**) | **Size-switched**: inline image under 2 MB, temp-file path at or above it, explicit path if asked |
| Playwright (**official**) | **Always a file path**, *plus* an inline image when the caller named no filename; `imageResponses` defaults to `auto` |
| Figma (official) | **Inline base64 PNG** via `get_screenshot`; *separately*, temporary **URLs** via `download_assets` |
| Unity `CoplayDev` | **Real MCP image content**, off by default, `max_resolution` 640, multi-angle contact sheet |
| Blender `ahujasid` | **Real MCP image content** — the only engine server that renders the *viewport* |
| Unreal `runreal` | **Real MCP image content** — `{ type: "image", data: base64, mimeType: "image/png" }` |
| Cinema 4D | **base64 data-URI inside a markdown string in a text block** — not the protocol's image channel |
| Unity (official) | **base64 in a JSON field** (`previewBase64`), asset thumbnails only, no viewport |
| Houdini | **A filesystem path as text**; the agent reads the file itself |
| Roblox (official), Maya, Godot, Unity `CoderGamester`, Unreal `chongdashu`, `flopperam`, `kvick` | **Nothing.** No capture of any kind |

The first pass concluded no strategy dominated. With the browser and engine servers in, one does: **nobody who thought about it inlines an image unconditionally.** Chrome decides on byte size, Playwright on whether the caller asked for a file, Coplay defaults images off entirely and caps resolution at 640 — and all three keep a path as the fallback. §2.5 has the numbers.

Three things worth carrying into [the capture request ticket](https://github.com/dvoyni/cog/issues/207):

**Image tokens scale with dimensions, not bytes** (Chrome's configuration docs, quoted in §2.5). This kills a plausible design instinct: a JPEG quality knob on a cog capture request would reduce disk and socket cost and buy nothing in context. A max-width/max-height cap is the parameter that earns its place.

**Figma deliberately splits seeing from fetching.** `get_screenshot` "returns a PNG and supports inline base64 so the agent can reason about it visually", while `download_assets` "returns temporary URLs that must be fetched". Two different delivery strategies in one server, chosen by purpose rather than by taste — and Figma's own note on the screenshot tool is "Recommended to keep on: only turn it off if you're concerned about token limits." That is the only vendor acknowledgement found that inline images cost context, and it is exactly the concern behind this map's choice of a path on disk.

**Houdini, having no image channel, tells the agent to verify numerically instead.** From `get_geometry_info`:

> "Summarize a node's geometry: point/primitive/vertex counts, bounding box, attribute listings per class, and group names. … **Use this to verify what a network actually produced instead of judging from a render.**"

That is independent support for the map's claim that a queue snapshot is not a consolation prize for a screenshot. A structured answer says *why*; a picture only says *what*. Maya is the counter-example — it imports `ImageContent`, never produces one, and ships no playblast or render tool, so its agent is simply blind.

Cinema 4D's data-URI-inside-text is worth naming as an **anti-pattern**: it works only because a particular client renders markdown, and it defeats any client that handles image content properly. Playwright's `imageResponses: 'auto'` is the correct version of the same instinct — ask the client what it can render, do not guess.

## 6. Descriptions, and who publishes guidance on writing them

The best descriptions found are not definitions. They carry operational knowledge into the one place the model always reads.

**Return shape inline.** Roblox's `run_script_in_play_mode` embeds the result schema, a preference ordering, a state-mutation warning and an error-recovery procedure in the description string:

> "Result format: { success: boolean, value: string, error: string, logs: {...}[], … }.
> - Prefer using start_stop_play tool instead run_script_in_play_mode …
> - After calling run_script_in_play_mode, the datamodel status will be reset to stop mode.
> - If It returns `StudioTestService: Previous call to start play session has not been completed`, call start_stop_play tool to stop play mode first then try it again."

**Worked example output.** Every `runreal` description embeds a literal `Example output:` line — for `editor_create_object`, a full JSON result with real values.

**Pointers to the follow-up call.** `flopperam`'s `add_node` documents 23 node types and, per enum value, names the *next* tool the agent will need:

> `"Switch" - Switch on byte/enum value with cases`
> `ℹ️ Creates 1 pin at creation; add more via set_node_property with action="add_pin"`
> `"Timeline" - Animation timeline playback with curve tracks`
> `⚠️ REQUIRES MANUAL IMPLEMENTATION: Animation curves must be added in editor`

**Preconditions and recovery.** Houdini's `set_parameters` tells the agent to call `get_parameter_schema` first if unsure, and that unknown names fail per-parameter with did-you-mean suggestions in a `"failed"` list. Its `create_wrangle` promises the node "is cooked immediately and the result includes a 'validation' report — check it for VEX compile errors before building on top. On invalid input the node is removed, never left half-configured."

**Steering between two tools.** The browser servers' descriptions carry no return shapes at all, and instead spend their words arbitrating: "this is better than screenshot", "You can't perform actions based on the screenshot, use browser_snapshot for actions", "Always use the latest snapshot", "cheaper than capturing the whole snapshot when you only need to locate an element", "Use browser_network_request with the number to get full details". Every one of those sentences exists to stop the model reaching for the wrong tool of a *pair*. That is a distinct job from documenting a contract, and it is the job that matters most when two tools answer nearly the same question — which is exactly the shape of a cheap-summary/full-snapshot pair.

**And the counter-example is official.** Chrome's own descriptions are one-liners: `click` is "Clicks on the provided element", `take_screenshot` is "Take a screenshot of the page or element." Chrome puts almost nothing in the description and instead invests in the *response* — the auto-attached snapshot, named errors that state the recovery tool, structured `annotations` — and in per-argument `.describe()` text. So "rich descriptions" is not a universal norm either. What is universal in the good servers is that **the operational knowledge lives somewhere the model reliably reads**; the description is one such place, and a well-shaped response envelope is another.

### The MCP project publishes no guidance on writing tool descriptions

This was an open question in the first pass. It has a plain answer, and it is a negative one worth stating flatly.

**The spec defines the field and never tells you how to fill it.** Across five shipped revisions (`2024-11-05` through `2026-07-28`) the normative text for `Tool.description` has not changed and reads, in full:

> "Human-readable description of functionality"

with the schema adding only:

> "This can be used by clients to improve the LLM's understanding of available tools. It can be thought of like a 'hint' to the model."

`Tool.name` gets mechanical constraints only — length, ASCII charset, case-sensitivity, uniqueness within a server. `title` is defined as a display name "optimized to be human-readable … even by those unfamiliar with domain-specific terminology", i.e. for people, not the model. The behavioural `annotations` (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) are defined and then explicitly disclaimed: "all properties in `ToolAnnotations` are **hints** … Clients should never make tool use decisions based on `ToolAnnotations` received from untrusted servers." Nowhere is there guidance on wording, length, granularity, disambiguating two similar tools, how many tools is too many, or what a surface costs in context.

**`tools/list` pagination exists and is justified on the wrong grounds.** The spec's pagination utility covers `tools/list` with opaque cursors, and the only motivation given is that pagination "is especially important when connecting to external services over the internet, but also useful for local integrations to avoid performance issues with large data sets." A transport concern. The spec nowhere bounds how large a tool list may be, and says nothing at all about result size or truncation.

**There is exactly one place the spec tells an author what to put in a description**, added in `2026-07-28` under "Stateful Tools", and it is flagged non-normative:

> "This section is non-normative guidance for tool design. **The protocol has no concept of a state handle; from the wire's perspective a handle is an ordinary string in a tool result and an ordinary argument to subsequent tool calls.**"
>
> "**Lifetime.** Because handles outlive any single connection, the server's retention policy should be stated in the creation tool's description (e.g., 'baskets expire after 24 hours of inactivity') so the model can see it when deciding to create state."

That is worth more to this map than its length suggests. Playwright's `ref=e2` and Chrome's `uid=1_3` are precisely this pattern, and the spec's position is that **the protocol owns nothing here — a handle is a string you invented and whose lifetime you must document in prose.** Whatever cog does with refs into a captured queue, no protocol machinery will help, and the staleness rule has to be written into the description of the tool that mints them.

**Where the docs site is asked the question directly, it points out of the org**, at `anthropics/claude-plugins-official`'s `mcp-server-dev` plugin. The eight "Best Practices" headings in `develop/build-server` are all about not writing to stdout.

**The one quantitative number the project publishes is client-side.** `client-best-practices` tells *hosts* — not server authors — to switch to progressive discovery once tool definitions occupy "a percentage of the context window … For example, 1%-5%", stated without derivation. The same page carries a caution almost nobody repeats: "Most providers cache the prompt prefix, including the `tools` array. Adding or removing tool definitions mid-conversation invalidates that cache, and the resulting miss **can cost more tokens than the definitions you removed.**" Any dynamic-tool-list scheme cog contemplates inherits that.

**Server-side progressive discovery is not in the protocol.** The roadmap says the project is "starting a dedicated effort around progressive discovery to define what an experimental server-side discovery mechanism would look like" — which is Epic's tool-search mode, invented independently because the protocol offered nothing.

The gap may be deliberate. The project's own design principles include "Capability over compensation": "Models improve faster than protocols evolve. We avoid adding permanent structure to work around limitations that are likely temporary." A defensible reading is that MCP treats description quality as a model-vendor concern and keeps it out of a wire protocol. But the project's own maintainers have written that "tool descriptions and other aspects of interface design for agents are still going to **make or break** how well LLMs can use your server" — so the omission is not for want of believing it matters.

**Consequence for this map: there is no protocol-level authority to defer to.** Whatever cog decides about description text, ref lifetimes, response size and tool count, it decides on its own evidence. That is an argument for [the provider contract](https://github.com/dvoyni/cog/issues/204) carrying an opinion, because nothing upstream carries one.

### First-party guidance that does exist

Three vendors publish it. None of them is the MCP project, and **two of them contradict each other.**

**Epic states the principle outright:**

> "Keep functions small and focused. One Tool, one responsibility."
> "Prefer descriptive function names and structured return types over free-form strings."
> "The description text and per-argument descriptions are reflected into the Tool's schema and **should be written with the same care as the public surface of any other API**."

**Chrome ships `docs/design-principles.md`**, seven bullets, and it is the most directly useful document found in this entire research. Reproduced nearly whole, because every line lands on a live ticket:

> - **Agent-Agnostic API**: Use standards like MCP. Don't lock in to one LLM. Interoperability is key.
> - **Token-Optimized**: Return semantic summaries. "LCP was 3.2s" is better than 50k lines of JSON. **Files are the right location for large amounts of data.**
> - **Small, Deterministic Blocks**: Give agents composable tools (Click, Screenshot), not magic buttons.
> - **Self-Healing Errors**: Return actionable errors that include context and potential fixes.
> - **Human-Agent Collaboration**: Output must be readable by machines (structured) AND humans (summaries).
> - **Progressive Complexity**: Tools should be simple by default (high-level actions) but offer advanced optional arguments for power users.
> - **Reference over Value**: for heavy assets (screenshots, traces, videos), **return a file path or resource URI, never the raw data stream**. Some MCP clients support a built-in handling of heavy assets e.g. directly displaying images. This _could_ be an exception.

Note that Chrome's own code is the worked example of each: "Reference over Value" *is* the 2 MB branch, with the stated exception spelled as a byte threshold. "Progressive Complexity" *is* `verbose: false` by default with an opt-in full tree. "Self-Healing Errors" *is* `No snapshot found for page {id}. Use take_snapshot to capture one.` A principles document whose principles are all traceable to source is worth more than one that is not, and this one passes.

Read against the map: "Files are the right location for large amounts of data" and "Reference over Value" are, between them, the strongest external support the map's capture design has.

**Anthropic, as the model vendor rather than the protocol steward, publishes the guidance MCP omits — and disagrees with Google about granularity.** Chrome says "Give agents composable tools (Click, Screenshot), **not magic buttons**." Anthropic's [writing effective tools for agents](https://www.anthropic.com/engineering/writing-tools-for-agents) says the opposite:

> "More tools don't always lead to better outcomes. A common error we've observed is tools that **merely wrap existing software functionality or API endpoints** — whether or not the tools are appropriate for agents."
>
> "Instead of implementing a `list_users`, `list_events`, and `create_event` tools, consider implementing a `schedule_event` tool which finds availability and schedules an event. Instead of implementing a `read_logs` tool, consider implementing a `search_logs` tool which only returns relevant log lines and some surrounding context."
>
> "**Too many tools or overlapping tools can also distract agents from pursuing efficient strategies.**"

Anthropic's platform docs go further: "Rather than creating a separate tool for every action (`create_pr`, `review_pr`, `merge_pr`), group them into a single tool with an `action` parameter."

**This is the section-3 contradiction again in a different costume.** Chrome argues from composability and determinism; Anthropic argues from selection ambiguity and context cost. Note where each is standing: Chrome is shipping a server whose agent must debug an unfamiliar live page, and cannot know in advance which composite it will need. Anthropic is looking at models choosing among many servers' tools at once. Both are right about their own problem, and **cog is standing where Chrome is standing** — a small number of capabilities against live state the agent cannot predict.

The two do agree on responses, and the agreement is the useful part:

> "tool implementations should take care to return only high signal information back to agents … and **eschew low-level technical identifiers** (for example: `uuid`, `256px_image_url`, `mime_type`)."
>
> "**For Claude Code, we restrict tool responses to 25,000 tokens by default.**"

That 25,000-token cap is the only published response-size budget from any vendor, and it is a useful order-of-magnitude anchor for what a cog queue snapshot may cost before it stops being a snapshot.

Two cautions on citing Anthropic here. Its advice to "return semantic, stable identifiers (for example, slugs or UUIDs)" in the platform docs sits badly against the engineering blog's claim that resolving UUIDs away "significantly improves Claude's precision" — the two pages are not aligned, and neither publishes the ablation. And on the question this map most wants answered, Anthropic explicitly declines:

> "**Even your tool response structure — for example XML, JSON, or Markdown — can have an impact on evaluation performance: there is no one-size-fits-all solution.** … The optimal response structure will vary widely by task and agent. We encourage you to select the best response structure based on your own evaluation."

The best-resourced party to answer "how should a tool hand a model a big structure" says: measure it yourself. Section 7 is why that is the honest answer, and why building no prototype costs this map something real.

Note also the mechanism Epic and Maya share: **the docstring *is* the description and the signature *is* the schema.** Maya's server has no tool decorators at all — it walks a directory, and derives each tool's schema by introspecting the function and its docstring. That is structurally the same move as the Go SDK's reflection over a request struct, and it is what makes "written with the same care as a public API" enforceable rather than aspirational.

The bad examples share one trait — they describe the *feature* rather than the *contract*. `flopperam`'s `construct_mansion` is "a magnificent mansion with … luxury features perfect for dramatic TikTok reveals", with no arguments documented and its legal `mansion_scale` values living only in a code comment that never reaches the schema. `chongdashu`'s `set_actor_property` offers "property_value: Value to set the property to" — no type, no enumeration, nothing the model can act on.

## 7. Serialising a big tree for a model

The ticket asked for evidence on JSON versus indented text versus summary-plus-drill-down. Both exist — convergent shipped practice *and* published measurement — and they largely agree, but the line between them is drawn sharply below because they are worth different amounts.

### What is demonstrated in shipped code

**Every server that thought hard about handing a model a live tree hands it indented plain text, one line per node, with a handle on the line.** Playwright and Chrome converged on the format independently (§2.1). Houdini's `get_geometry_info` returns counts and listings, not a graph dump.

The engine servers are the counter-example and they cut the other way: all four that emit structure emit **JSON**, and three of the four do it badly — Unity's official server recurses to unlimited depth, CoderGamester pretty-prints the whole scene with `JSON.stringify(hierarchy, null, 2)`, and Blender stops at ten objects without saying so (§3.3). The one good JSON implementation, Coplay's, wins by *not* emitting a tree at all: it returns one level of paged summary at a time. So the shipped evidence is less "text beats JSON" than **"a tree is the wrong thing to hand over whole, and the parties who serialised it as text were the ones who had already worked that out."**

Where JSON survives, it survives *beside* the text rather than instead of it. Chrome computes both — `SnapshotFormatter.toString()` for the model, `SnapshotFormatter.toJSON()` into MCP `structuredContent` — which is its "readable by machines (structured) AND humans (summaries)" principle made literal. Playwright emits its JSON tree only under an internal `_meta.json` flag its own CLI sets, never to an ordinary MCP client.

Two second-order consequences are demonstrated rather than argued:

- **The text form is what makes search cheap.** `browser_find` is `grep -C` plus indentation arithmetic (§2.3). The equivalent over a JSON tree is a different and harder tool. Choosing the serialisation chose the tooling that could sit on top of it.
- **Compression and addressing are separable.** Playwright's distiller drops nodes from the rendered text while leaving `snapshot.refs` intact, "so refs of removed nodes still resolve through the aria-ref selector engine" (§2.4). A big tree can be made small to read without being made small to act on. This is the most useful single idea in this section.

### Summary-plus-drill-down is universal for flat data, and contested for trees

Both browser servers pair a cheap list with an expensive fetch — Chrome's `list_network_requests` + `get_network_request`, Playwright's numbered list + `browser_network_request` — and Figma pairs `get_design_context` with the cheaper `get_metadata`. For flat sequences this is unanimous.

For trees the field splits. The browser servers prune, root and depth-cap but never paginate (§2.3); Coplay paginates one level at a time with cursors and gets a usable result (§3.3). Both work. What separates them is that the browser servers have a *semantic* pruning rule available — an accessibility tree knows which nodes are decorative — and a scene graph does not. **cog's ui element tree may or may not have such a rule; if it does, prune, and if it does not, page like Coplay rather than truncate like Blender.**

**No vendor in this file publishes a number for its own snapshot** — no token counts, no ablation between formats, nothing showing the shape they chose beats the one they didn't. The convergence above is craft.

The one place a vendor put money on the question, it hedged. Chrome ships `--experimentalDataFormat=toon|gcf`, swapping the built-in text formatter for [TOON](https://github.com/toon-format/toon) or GCF — both compact object encodings aimed at token efficiency, both **optional peer dependencies the user must install separately**, both behind a flag named `experimental`. The default remains the indented text. A team holding a number would not ship it as an experiment.

### What is measured, in the literature

There is more measured evidence than the discourse suggests, and it says something the discourse does not want to hear: **format choice is mostly a cost lever, not an accuracy lever, and the accuracy effects that do exist reverse between models.**

**Verbosity is not free and is not helpful.** KG-LLM-Bench ([arXiv:2504.07087](https://arxiv.org/abs/2504.07087), Markowitz et al.) textualises the same knowledge graphs five ways and reports tokens and accuracy together:

| Format | Mean input tokens | Accuracy |
| --- | --- | --- |
| List of edges (flat outline) | **2,645** | 0.412 |
| Structured YAML | 2,903 | 0.406 |
| Structured JSON | 4,505 | **0.419** |
| RDF Turtle | 8,171 | 0.349 |
| JSON-LD | 13,503 | 0.342 |

YAML costs 36% fewer tokens than JSON for identical data and loses 1.3 accuracy points. JSON-LD costs 3× JSON and loses 7.7. The self-describing formats are dominated — they cost more *and* score worse. The paper also reports "up to 17.5% absolute difference" from textualisation choice, and that Claude-3.5-Sonnet preferred RDF Turtle while most other models preferred list-of-edges or JSON.

**And the ranking flips between models.** [arXiv:2411.10541](https://arxiv.org/abs/2411.10541) (He et al., Microsoft) holds content fixed across plain text, Markdown, JSON and YAML: on MMLU, GPT-3.5-turbo scored JSON 59.7% / Markdown 50.0%; GPT-4 scored **Markdown 81.2% / JSON 73.9%** — the order inverted. Their conclusion is blunt: "There is no universally optimal format, even within the same generational lineage."

**On MCP tool surfaces specifically**, [arXiv:2605.29676](https://arxiv.org/abs/2605.29676) (Kutschka & Geiger) serialises tool schemas and invocations in TOON and TRON across BFCL, MCPToolBench++ and MCP-Universe: 2–27% input-token reduction, accuracy from +3 to −14 points, and one config (Mistral-Small-24B on BFCL) dropping 89% → 53%. Two failure modes it names are worth more than the headline: a **parse-failure cascade** in multi-turn use, where one bad tool call buys a whole extra reasoning iteration and erodes the saving, and a **header tax**, where the per-batch class header costs more than the body it deduplicates. An independent TOON-vs-JSON study ([arXiv:2603.03306](https://arxiv.org/abs/2603.03306)) adds the **prompt tax**: a novel notation needs a paragraph explaining itself, and in short contexts that overhead eats the savings. TOON's own README is franker than its advocates — compact JSON "often wins outright" for "deeply nested or non-uniform structures", which is precisely what a ui element tree is.

**Summary-plus-drill-down is scale-conditional, and stacking levels hurts.** The one real ablation, [arXiv:2607.17598](https://arxiv.org/abs/2607.17598) (He et al., UC Davis), compares raw navigation, flat progressive disclosure and hierarchical disclosure over ∞Bench at K=1 to K=20 books. At one book the gain is noise (+2.75%, +1.04%). At K=20 it is decisive: Codex on En.QA goes 0.257 → **0.462**, and cost goes 68.3M tokens → 32.5M. **Hierarchical disclosure never helps** and sometimes collapses (0.9126 → 0.6398). Their framing:

> "Progressive disclosure buys context, not intelligence: it is redundant while a strong agent can locate the right passages itself, and decisive once the corpus grows too large to navigate by reading."

Their recommendation on depth is one sentence: "one level is enough."

**And the biggest measured wins came from dropping fields, not changing syntax.** Anthropic's tool-writing guidance shows the same Slack result rendered two ways — **206 tokens versus 72** — with the saving coming entirely from omitting `thread_ts`, `channel_id` and `user_id`, not from a different encoding. The progressive-disclosure paper's raw→flat win is the same move at corpus scale.

**That is the same conclusion Playwright's distiller reaches in code** (§2.4). Drop what carries no information; keep the syntax boring. The measured evidence and the shipped practice agree with each other and disagree with the instinct to reach for a cleverer notation.

### Reading it against cog

- A cog capture is one tree or one queue from one live process — the K=1 end of the progressive-disclosure curve, not K=20. The measured evidence says a summary/drill-down pair is **not** automatically worth it there, and that if cog builds one it should be **one level deep**, never a hierarchy.
- Do not invent a notation. The token saving is real but small (2–27%), the accuracy risk is model-specific and can be double-digit, and both the prompt tax and the parse-failure cascade land hardest in exactly cog's case: a long multi-turn loop.
- Spend the effort on what to omit, not on how to encode. That is where every measured win in this section came from.
- Expect to have to measure. Both Anthropic and the strongest academic results independently land on "evaluate it on your own task", which is an honest answer rather than a dodge — and it is a real argument against the map's decision to build no prototype.

The only concrete numbers any vendor publishes are Chrome's, and both concern images: the 2 MB inline/path threshold, and "image tokens scale with dimensions rather than encoded bytes" (§2.5). The second is a claim about a mechanism, almost certainly right, and still an assertion in a config doc rather than a published measurement.

## 8. Stances this map can adopt

Each with the evidence behind it, for the tickets to accept or reject.

1. **Hand the agent a tree, and say in the tool's own description why it is not a picture.** Evidence: two official browser servers converged on an indented a11y tree, and both put the argument in the description string — "this is better than screenshot", "You can't perform actions based on the screenshot, use browser_snapshot for actions", "Prefer taking a snapshot over taking a screenshot" (§2.1). Note the reason they give is **actionability**, not cost: a tree line carries a handle and a pixel does not. Bears on [queue capture](https://github.com/dvoyni/cog/issues/208) and on [the capture request](https://github.com/dvoyni/cog/issues/207) — the two are not competitors, and the surface should say so where the model reads it.
2. **Serialise as indented text with a handle per line. Do not invent a notation.** Evidence: Playwright and Chrome reached the same line format independently (§2.1); Chrome computes JSON too and puts it in `structuredContent` rather than in front of the model; `browser_find` only works *because* the form is text (§2.3). Measured backing: verbose self-describing formats are dominated on both cost and accuracy, format rankings reverse between models, and compact notations buy 2–27% at a risk of double-digit accuracy plus a prompt tax and a parse-failure cascade in multi-turn loops (§7).
3. **Return state by reference, not by value, when the agent did not ask for it.** Evidence: Playwright auto-attaches a snapshot after every action and **writes it to a file**, returning a link plus a cheap header (URL, title, `Console: 2 errors, 0 warnings`); only an explicit `browser_snapshot` inlines it (§2.6). Chrome's design principles state it as a rule — "Files are the right location for large amounts of data", "Reference over Value … never the raw data stream". This reframes [queue capture](https://github.com/dvoyni/cog/issues/208): the question is not whether to return the queue, but by value or by reference, decided by who asked.
4. **Prune trees; paginate logs. They are different problems.** Evidence: no browser server paginates a tree and none prunes a log (§2.3). Trees get semantic pruning, a subtree root, a depth cap and a search tool; flat sequences get `pageSize`/`pageIdx` and fetch-by-index. Coplay is the engine-side exception that proves the value of paginating a tree *when* you have no way to prune one semantically — and it does it properly, one level at a time with `truncated`, `next_cursor` and `total` (§3.3). cog has one of each shape — a ui element tree and a canvas op queue — and they want different capabilities, not one tool with a filter argument. This replaces the first pass's weaker "cheap summary tool beside the full snapshot".
5. **Never truncate silently.** Evidence: Blender's `get_scene_info` stops at ten objects with the true count beside it, no cursor and no flag, and the cap was tightened from twenty rather than replaced with paging (§3.3). The most-used server in this space cannot show an agent a thirty-object scene, and the agent cannot tell. Whatever cog's capture omits, it must say it omitted it and offer a way to reach the rest.
6. **Compress the view without shrinking the action surface.** Evidence: Playwright's distiller drops nodes from the rendered text while "`snapshot.info` and `snapshot.refs` are left intact, so refs of removed nodes still resolve" (§2.4). Corroborated by measurement: the biggest wins came from dropping fields, not changing syntax (§7), and by three more parties reaching for omission when they needed a tree to fit — Unity's commented-out transform with "this will be too much data" beside it, Coplay's `include_transform` defaulting false, Anthropic's 206→72 (§3.3). For cog: a collapsed or elided op must still be addressable by the ref it would have had, and the first lever to pull is which fields ship by default.
7. **Refs are generation-stamped, and staleness is a named, actionable error.** Evidence: Chrome's `uid` is `<snapshotId>_<counter>`, only the latest map is kept, and a stale one fails with "Element uid X not found on page" while a missing snapshot fails with "No snapshot found for page N. **Use take_snapshot to capture one.**" (§2.2). The MCP spec confirms nothing helps here — "the protocol has no concept of a state handle" and a handle's lifetime "should be stated in the creation tool's description" (§6). Bears on [queue capture](https://github.com/dvoyni/cog/issues/208).
8. **On images: cap resolution, not quality, and switch to a path on size.** Evidence: "image tokens scale with dimensions rather than encoded bytes", so JPEG/WebP buy transfer and storage and *not* context; Chrome's 2 MB inline/path threshold; Playwright's rule that an explicit filename means the picture need not also be spent; `imageResponses: auto` negotiating against client capability (§2.5, §5). Confirms the map's settled choice for [the capture request](https://github.com/dvoyni/cog/issues/207) and adds the parameter that actually earns its place. Do **not** copy Cinema 4D's data-URI-in-text.
9. **The full registry is not the default surface, and the risk is overlap rather than count.** Evidence: Epic's discovery meta-tools, Playwright's capability gating (80 defined, 24 advertised), Chrome's `--slim` (61 → 3), Coplay's runtime-togglable groups and agent-mutable `manage_tools` (48 defined, 30 default) — four servers, four mechanisms, one premise (§2.7, §3.6). Coplay names the failure mode precisely: overlapping tools that "both apply to materials in different ways" confuse the router, and "the wrong-tool rate drops measurably" when one is hidden — **asserted with no number, so cite it as a hypothesis, not a result.** cog's v1 flat list is fine on count; the thing to watch is any pair of capabilities that answer the same question, such as a cheap summary beside a full capture. The MCP spec offers no help: server-side progressive discovery is a roadmap item, not a protocol feature (§6).
10. **Write descriptions as API surface — and spend the words on steering between similar tools.** Evidence: Epic's "written with the same care as the public surface of any other API"; Roblox's and Houdini's embedded recovery procedures; and the browser servers spending almost every description sentence on arbitration between a pair of tools (§6). Note the honest counterweight: Chrome's descriptions are one-liners and its server works, because the knowledge lives in the response envelope and the errors instead. The invariant is that operational knowledge lands *somewhere the model reliably reads*. Bears on [the provider contract](https://github.com/dvoyni/cog/issues/204).
11. **Having no escape hatch obliges the narrow tools to be trustworthy — but the obligation is narrower than the first pass thought.** Evidence: the Houdini/C4D contradiction, Playwright's third stance of shipping one labelled RCE-equivalent, Chrome's fourth of making it operator-removable via `--no-javascript-evaluation` (§4), and Unity's fifth (§3.2): a *structured* hatch whose mandated calling convention — `result.RegisterObjectCreation`, `result.RegisterObjectModification`, `result.DestroyObject` — delivers undo and object tracking without a wrapper. Houdini's case for narrow tools rested on validation, structured errors and single-step undo being things the wrapper *adds*; Unity shows two of the three can be had otherwise. cog has ruled out arbitrary dispatch regardless, so validation and non-drift remain requirements — and reflection over a Go type is what makes non-drift structural.
12. **Batch the round trips before optimising the payload.** Evidence: both Unity community servers ship a `batch_execute` and justify it as "reduces latency and token costs by 10-100x compared to sequential tool calls", capped at 100 operations, with an atomic mode that rolls back through the editor's undo stack (§3.6). **The 10–100× is unsubstantiated**, but the observation under it is not: an agent building a scene issues many near-identical calls and re-pays the whole response envelope on each. cog's ui and canvas capabilities have exactly that shape, and this is an axis the first pass did not have at all.
13. **Nothing upstream will decide any of this for cog.** Evidence: the MCP project defines `Tool.description` in one clause and has never advised how to write one; its docs site delegates the question to an Anthropic plugin; its only published number is a client-side 1–5% context threshold; and Anthropic, asked directly about response structure, answers "there is no one-size-fits-all solution … select the best response structure based on your own evaluation" (§6, §7). This is an argument for [the provider contract](https://github.com/dvoyni/cog/issues/204) carrying an opinion — and, uncomfortably, an argument that **the map's decision to build no prototype is a real cost**, because the two best-resourced parties in the field both say the answer is task-specific and measured.

And one the evidence raises rather than settles, for [the map](https://github.com/dvoyni/cog/issues/199) rather than a provider ticket: **two first parties have now deprecated their own MCP server in favour of a CLI** — Microsoft on token grounds, Unity on "faster iteration times, improved stability, and the ability to target runtime and the Editor" (§2.8, §3.1). Neither says MCP is wrong. Microsoft names the conditions under which it is right — "persistent state, rich introspection, and iterative reasoning over page structure … long-running autonomous workflows" — and those conditions describe a cog agent iterating on a frame better than they describe a coding agent editing files. The map should be able to state that argument in one sentence, because two vendors have now made the opposite call in public.

Superseded from the first pass: "reserve judgement on tool count" now reads as stance 9, which is sharper — the count is not the axis, the *default surface* and tool overlap are. "A structured answer beats a picture" is folded into stance 1 with much stronger evidence behind it.

## 9. What this still does not cover

Named plainly. Four of the first pass's five gaps are now closed — the browser servers (§2), Unity/Godot/Blender (§3), the MCP project's guidance or lack of it (§6), and the serialisation question (§7). What remains:

**The real hole, and it is a different one than before.**

- **Nobody has watched an agent use any of this.** The first pass said there was no measured evidence on which surfaces agents use well, and that is still true of every shipped server here: no vendor publishes tool-selection failure rates, and none of the granularity choices in §1 is backed by anything measured. Section 7 changes the picture only slightly — the literature measures *formats* and *disclosure strategies* on benchmarks, not tool surfaces on real work. And the two best-resourced parties both answer the central question with "evaluate it on your own task". **That is now the sharpest finding against the map's no-prototype decision**, and it should be recorded as such rather than left as a footnote here.
- **Nothing here observes a failure.** Every quote in this file is a vendor describing what it does. No post-mortem, no issue thread where an agent demonstrably picked the wrong tool, no before-and-after from a server that changed its surface and measured the result. `flopperam`'s "44 to 21 tools" reduction is the closest thing, and its stated motive was "reduce complexity" with no outcome reported.

**Smaller, and honestly labelled.**

- **The Playwright figures are from `main` (`af74c93`), not from a tagged release.** The key description strings and the 24-tool default were cross-checked against the shipped `@playwright/mcp@0.0.80` generated README and match. The `snapshotToFile` behaviour in §2.6 was verified by reading `Response._build()` and confirmed by the repo's own test fixture reading snapshots back off disk — but it was **not** observed on a live server, and `browser_install` exists in the 0.0.80 README while being absent from `main`, so some drift between the two is real.
- **The arXiv results in §7, and the engine-server source readings in §3, were gathered by subagents against primary sources but not independently re-read by this pass.** The counting commands, file paths and quotes are reported as found. Treat them as directionally reliable and re-verify any single figure before it lands in a spec. The browser-server material in §2 was read directly and is the most solid thing in this file.
- **No author rationale exists for Blender's or Godot's granularity.** Searched for and not found in blogs, issues or repos; the Blender author's one recorded interview is about motivation, not tool surface. Their designs are inferable but not explained, so §3 reads their code and does not put words in their mouths.
- **The eight exact tool-name matches between Unity's first-party server and Coplay's** (`manage_gameobject`, `script_apply_edits`, `manage_script_capabilities` and five more) are an observation. No stated lineage was found in either repo, and none is claimed here.
- **Coplay's "the wrong-tool rate drops measurably" has no number, method or data behind it**, and it is the only claim about tool-selection failure anywhere in this research. It names a plausible failure mode; it does not establish one.
- **TOON's own benchmark is run by the format's author** and should be read as such; the independent replications cited beside it are less favourable, which is the reason both are in §7.
- **No account of how a client renders any of this.** Whether a given host actually shows an inline image, follows a file link, or truncates a long text block is unexamined, and Playwright's `imageResponses: 'auto'` implies clients differ enough to matter.
- **Figma's read-table row count is 17 or 18** — the names are verified, the total was not byte-checked. A config parameter named `enableBase64Response` appeared in search summaries but **could not be confirmed in any Figma source; do not cite it.**
- **Epic's ~1,091 tools across 52 toolsets is third-party**, machine-generated by calling `describe_toolset` against UE 5.8.0. Epic publishes no count. The tool-search quotes themselves are from Epic's documentation and are solid.
- **Three servers' own documentation disagrees with their source**, which is why nothing in this file trusts a README for a count: `chongdashu` advertises viewport control the source shows commented out as buggy; Coplay's docs say 47 tools where source has 48; and CoderGamester's README documents 33, omitting `get_scenes_hierarchy` — its most important read tool. Every count here comes from registration source with the command recorded.
