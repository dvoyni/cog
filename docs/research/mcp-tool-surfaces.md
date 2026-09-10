# What makes an agent-facing tool surface usable

Research for [research: what makes an agent-facing tool surface usable](https://github.com/dvoyni/cog/issues/202), a ticket on the map [mcp: an agent-facing extension point, and the broker that serves it](https://github.com/dvoyni/cog/issues/199).

**Gathered 2026-09-10.** This exists because the map builds **no prototype**: nothing in the effort ever watches an agent try to use these tools, so prior art is the only evidence available before the specs are written.

Counts and quotes below come from reading registration source files and official vendor documentation directly. Where a number is a README claim rather than something counted from source, it says so. **Section 6 lists what was not reached** — the gaps are load-bearing and should not be papered over.

## The short version

Nine servers examined across five 3D and design tools, two of them first-party. What the evidence supports:

- **There is no convergent granularity norm, and the two first-party servers sit at opposite extremes.** Figma ships 34 tools; Roblox ships 6 with the entire scene API behind one `run_code`. Anyone claiming "MCP servers should have N tools" is generalising from one example.
- **Epic is the only party that solved the many-tools problem architecturally**, and said why: three discovery meta-tools instead of a flat list, so `tools/list` stays small at ~1,091 tools.
- **Two mature servers ship the same arbitrary-code escape hatch and give opposite advice about it, in the description text the model reads.** That contradiction is the single most useful finding here.
- **Visual return is unsolved in this space.** Four servers return no image at all; one smuggles base64 inside a text string; one returns a filesystem path; only two use the protocol's image channel.
- **The best descriptions carry operational knowledge, not definitions**: return shapes, worked examples, preconditions, and explicit pointers to the follow-up tool call.

## 1. Granularity: no norm, and first parties disagree

| Server | Tools | Shape | Escape hatch |
| --- | --- | --- | --- |
| Epic / Unreal 5.8 (**official**) | ~1,091 in 52 toolsets | narrow, one per operation | none needed |
| Figma (**official**) | 34 | narrow reads, **one mega write tool** | — |
| Roblox Studio (**official**) | 6 | everything behind `run_code` | *is* the escape hatch |
| Houdini `capoomgit` | 25 | narrow, one per graph op | `execute_houdini_code` |
| Cinema 4D `ttiimmaacc` | 25 | narrow, noun-shaped | `execute_python_script` |
| Maya `PatrickPalmer` | 18 | hybrid; `mesh_operations` is an 8-way action enum | none |
| Unreal `flopperam` | 43 | hybrid + task-level macros | none |
| Unreal `chongdashu` | 30 | narrow | **none — hard capability ceiling** |
| Unreal `runreal` | 20 | coarse | `editor_run_python`, `editor_console_command` |
| Unreal `kvick-games` | 8 | very broad | `execute_python` |

The inverse correlation is the pattern: **the fewer tools a server has, the more likely it ships an arbitrary-code escape hatch**, and the servers with the most tools need none. `chongdashu` is the cautionary case — 30 narrow tools and no escape hatch means anything not on the list is simply impossible, and its README advertises viewport control that the source shows commented out as buggy.

**Epic's tool-search mode** is the one architectural answer anybody has published. From Epic's own documentation:

> "By default, the plugin runs in tool-search mode (`bEnableToolSearch = true`). In this mode, `tools/list` returns three discovery meta-tools rather than every advertised Tool"

The three are `list_toolsets`, `describe_toolset` and `call_tool`, and the stated rationale is that the agent "walks this discovery path on demand, **which keeps `tools/list` responses small even when the registry exposes hundreds of Tools**." Epic also ships *zero* tools in the core plugin — toolsets are a separate plugin — which is the same broker-and-providers split this map is designing.

**Figma is the instructive hybrid.** Reads are many narrow tools organised around a hub, with an explicitly cheaper fallback for size:

> "`get_design_context` is the default entry point: the other tools in this group are either inputs to it, or fallbacks when a selection is too large or you need a specific asset format."

`get_metadata` is that fallback, documented as "Useful for very large designs where `get_design_context` produces output with a large context size." Writes, by contrast, are consolidated behind a single `use_figma` covering Design files, FigJam boards and Slides decks.

**Consolidation happens, and does not always stick.** Figma deprecated four per-kind shader tools (`list_shader_effects`, `list_shader_fills`, `get_shader_effect`, `get_shader_fill`) into two, and renamed `get_code` to `get_design_context`. `flopperam` documents a reduction "from 44 to 21 tools" — but the stated motive is "reduce complexity", **not** context limits, and the count has since drifted back to 43 with the removed tools re-added.

## 2. The escape-hatch contradiction

Two mature DCC servers ship an identical arbitrary-Python tool and reach opposite conclusions, each stated in the description the model actually reads.

**Houdini — last resort:**

> "Execute arbitrary Python code in Houdini's environment. LAST RESORT: prefer the dedicated tools (connect_nodes, set_parameters, create_wrangle, get_geometry_info, ...) — they validate input, report structured errors and are undoable as a single step. Use this only for operations no dedicated tool covers."

**Cinema 4D — first resort:**

> "Execute a Python script in Cinema 4D's Python environment. **This is the most reliable tool for non-trivial operations — it gives full access to the c4d API and avoids wrapper/schema mismatches that can affect other tools.**"

The disagreement is not really about escape hatches; it is about **whether the narrow wrappers are trustworthy**. Houdini's argument for narrow tools is validation, structured errors and single-step undo — properties the wrapper *adds*. Cinema 4D's argument against them is schema drift — the wrapper falling out of sync with the thing it wraps.

For cog this cuts a specific way. The map has already ruled arbitrary command dispatch out of scope, so there is no escape hatch to argue about — which means **cog is buying Houdini's argument and taking on Houdini's obligation**: the typed capabilities have to validate, report structured errors, and not drift. The C4D failure mode is the one to design against, and cog's position is better than either, because a capability rendered from a Go type by reflection cannot drift from the type it was rendered from.

## 3. Visual return: four strategies, none dominant

| Server | How a picture comes back |
| --- | --- |
| Figma (official) | **Inline base64 PNG** via `get_screenshot`; *separately*, temporary **URLs** via `download_assets` |
| Unreal `runreal` | **Real MCP image content** — `{ type: "image", data: base64, mimeType: "image/png" }` |
| Cinema 4D | **base64 data-URI inside a markdown string in a text block** — not the protocol's image channel |
| Houdini | **A filesystem path as text**; the agent reads the file itself |
| Roblox (official), Maya, Unreal `chongdashu`, `flopperam`, `kvick` | **Nothing.** No capture of any kind |

Two things worth carrying into [the capture request ticket](https://github.com/dvoyni/cog/issues/207):

**Figma deliberately splits seeing from fetching.** `get_screenshot` "returns a PNG and supports inline base64 so the agent can reason about it visually", while `download_assets` "returns temporary URLs that must be fetched". Two different delivery strategies in one server, chosen by purpose rather than by taste — and Figma's own note on the screenshot tool is "Recommended to keep on: only turn it off if you're concerned about token limits." That is the only vendor acknowledgement found that inline images cost context, and it is exactly the concern behind this map's choice of a path on disk.

**Houdini, having no image channel, tells the agent to verify numerically instead.** From `get_geometry_info`:

> "Summarize a node's geometry: point/primitive/vertex counts, bounding box, attribute listings per class, and group names. … **Use this to verify what a network actually produced instead of judging from a render.**"

That is independent support for the map's claim that a queue snapshot is not a consolation prize for a screenshot. A structured answer says *why*; a picture only says *what*. Maya is the counter-example — it imports `ImageContent`, never produces one, and ships no playblast or render tool, so its agent is simply blind.

Cinema 4D's data-URI-inside-text is worth naming as an **anti-pattern**: it works only because a particular client renders markdown, and it defeats any client that handles image content properly.

## 4. What good descriptions actually do

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

**And Epic states the principle outright**, which is as close to first-party guidance as this research found:

> "Keep functions small and focused. One Tool, one responsibility."
> "Prefer descriptive function names and structured return types over free-form strings."
> "The description text and per-argument descriptions are reflected into the Tool's schema and **should be written with the same care as the public surface of any other API**."

Note the mechanism Epic and Maya share: **the docstring *is* the description and the signature *is* the schema.** Maya's server has no tool decorators at all — it walks a directory, and derives each tool's schema by introspecting the function and its docstring. That is structurally the same move as the Go SDK's reflection over a request struct, and it is what makes "written with the same care as a public API" enforceable rather than aspirational.

The bad examples share one trait — they describe the *feature* rather than the *contract*. `flopperam`'s `construct_mansion` is "a magnificent mansion with … luxury features perfect for dramatic TikTok reveals", with no arguments documented and its legal `mansion_scale` values living only in a code comment that never reaches the schema. `chongdashu`'s `set_actor_property` offers "property_value: Value to set the property to" — no type, no enumeration, nothing the model can act on.

## 5. Stances this map can adopt

Each with the evidence behind it, for the tickets to accept or reject:

1. **Write descriptions as API surface, with return shape and failure modes inline.** Evidence: Epic's stated guidance; Roblox's and Houdini's descriptions carrying recovery procedures; the bad examples failing precisely by describing features. Bears on every provider ticket, and on [the provider contract](https://github.com/dvoyni/cog/issues/204), because it is an argument for the broker owning description text rather than each package.
2. **A structured answer beats a picture for "why", and should not be positioned as a fallback.** Evidence: Houdini's `get_geometry_info` instructing the agent to verify numerically rather than from a render; Maya's blindness being survivable. Bears on [queue capture](https://github.com/dvoyni/cog/issues/208).
3. **Size is a first-class design axis, not an afterthought.** Evidence: Figma's hub-plus-cheap-fallback pair driven explicitly by "large context size"; Epic's tool-search mode existing solely to keep `tools/list` small. Bears on [queue capture](https://github.com/dvoyni/cog/issues/208), where a busy frame is thousands of ops, and suggests a cheap summary tool beside the full snapshot rather than one tool with a filter argument.
4. **A path on disk is defensible, and Figma is the only vendor to acknowledge the cost inline images impose.** Evidence: Figma's "only turn it off if you're concerned about token limits", plus Houdini shipping paths only. Confirms rather than challenges the map's settled choice for [the capture request](https://github.com/dvoyni/cog/issues/207). Do **not** copy Cinema 4D's data-URI-in-text.
5. **Having no escape hatch obliges the narrow tools to be trustworthy.** Evidence: the Houdini/C4D contradiction. cog has already ruled out arbitrary dispatch, so validation and non-drift are now requirements, not niceties — and reflection over a Go type is what makes non-drift structural.
6. **Reserve judgement on tool count.** Evidence: 6 to 1,091 among first-party servers. cog's v1 is a handful of capabilities, so the flat list is fine; Epic's discovery pattern is what to reach for *if* the app-plugin escape hatch (currently fog on the map) ever opens the count up.

## 6. What this does not cover

Named plainly. A gap-filling pass should extend this file rather than restate it.

- **Unity, Godot and Blender were not reached.** They are the three most-cited engine MCP servers and their absence is the largest hole here — particularly Blender, whose server is the most widely used of any in this space.
- **Browser devtools and Playwright-style servers were not reached**, and they are the *closest structural analogue to this map*: an agent inspecting live, changing state it cannot see, handing over a page's structure as a snapshot. Their answer to "how do you serialize a large live tree for a model" is the single most relevant missing datapoint, and [queue capture](https://github.com/dvoyni/cog/issues/208) should not be resolved without it.
- **The MCP project's own guidance on writing tool descriptions was not read** — Epic's is the only first-party guidance quoted here, and it is Epic's, not the protocol's.
- **No measured evidence on which surfaces agents actually use well.** Everything in section 4 is craft observed in shipped servers, not outcomes. No server publishes tool-selection failure rates, and none of the granularity choices here is backed by anything measured.
- **Figma's read-table row count is 17 or 18** — the names are verified, the total was not byte-checked. A config parameter named `enableBase64Response` appeared in search summaries but **could not be confirmed in any Figma source; do not cite it.**
- **Epic's ~1,091 tools across 52 toolsets is third-party**, machine-generated by calling `describe_toolset` against UE 5.8.0. Epic publishes no count. The tool-search quotes themselves are from Epic's documentation and are solid.
- **`chongdashu`'s README contradicts its source** on viewport control, which is a reminder that README claims were not trusted anywhere in this file; counts come from registration source.
