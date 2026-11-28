# What v1's UI takes from v1, and what v2 offers or lacks

Research for [cog#977](https://github.com/dvoyni/cog/issues/977), on the map
[cog#975](https://github.com/dvoyni/cog/issues/975). Gathered 2026-11-28.

The question: what does v1's UI (`_legacy/bundles/ui`) take from v1's kernel,
canvas, app, input, assets and MCP provider, and what does v2 offer in place of
each? Where does v2 offer nothing? Which promises of v1's guide rest on v1's
kernel or canvas and must be restated?

It records facts and decides nothing. Findings name the ticket of the map they
feed: [#978](https://github.com/dvoyni/cog/issues/978) declaring,
[#979](https://github.com/dvoyni/cog/issues/979) gamepads,
[#980](https://github.com/dvoyni/cog/issues/980) sizing,
[#981](https://github.com/dvoyni/cog/issues/981) passes,
[#982](https://github.com/dvoyni/cog/issues/982) containers,
[#983](https://github.com/dvoyni/cog/issues/983) the visual,
[#984](https://github.com/dvoyni/cog/issues/984) ui on the kernel,
[#985](https://github.com/dvoyni/cog/issues/985) pointer input,
[#987](https://github.com/dvoyni/cog/issues/987) focus,
[#988](https://github.com/dvoyni/cog/issues/988) text fields,
[#989](https://github.com/dvoyni/cog/issues/989) layout inspection and
[#990](https://github.com/dvoyni/cog/issues/990) testing.

**Sources.** v1's code and docs under `_legacy/`, and v2's code and docs in the
working tree at `28a06b9`. A path under `_legacy/` is v1, and every other path
is v2, from the repository root. `L` is a line in the file named. `ui/` stands
for `_legacy/bundles/ui/`.

**Run or read.** Throwaway programs outside the repository ran canvas's `font`
package and canvas under `canvastest`. A finding they produced says *(ran)*.
Everything else was read from code. Nothing under `_legacy` was built.

## Answer in short

| v1 gives ui | v2 counterpart | Status |
|---|---|---|
| Resources, a subscription that locks and runs, ordering by subscription type | Resources, tasks, `Handlers` with `OrderSubscriptions`, the `in` dependency | Reshaped |
| A command (`ArmLayoutCmd`) and an MCP provider | Nothing. canvas's inspection is the precedent | **Gap** |
| `app.UpdateEvent`: lay out, hit-test and draw once per tick | A metronome beat; entries only inside `canvas.FrameEvent` | Reshaped, forced choice |
| `app.TimeCmd`: pause and step | Nothing | **Gap** |
| `input.State`: pointer, button edges per tick | `platform.InputState` and input events, per window | Reshaped |
| `gfx.Viewport`: logical size, window size, framebuffer size | `WindowManager`, `canvas.Camera`, `canvas.Targets` | **Partial** |
| Text size, wrapped size, font metrics | `FontLibrary.Layout` and `Metrics` | Direct, with differences |
| Sprite size from the header | `Assets.ImageSize(image)` | Direct |
| `MaterialSet`: one material for every family under a node | One `Material` per entry; text needs its own | **Partial** |
| Tint ramp | Plain multiply; v1's ramp is the same multiply with another neutral | Direct |
| Sprite transform, nine-slice, tiling | `Sprite`, `DrawMode`, `ImageOptions`, `Crop` | Reshaped, one **gap** |
| `SetClip` around a visual | `Clip` on every entry | Reshaped |
| `canvas.Layer` and a transform per layer | `Layer`, `Order`, cameras with layer ranges | Reshaped |
| `storage.FileSystem`, `libs/assets` behind canvas's lookup | `platform.FS` inside canvas's parameters | Direct |

### Gaps: v1 had it and v2 offers nothing

1. **A material that serves images and text alike.** A custom material on a
   `Text` samples the glyph's distance field, not its coverage (section 5.3).
   *v1 got:* one `Element.Material` put a shader on every visual under it,
   glyphs and fills included. #983.
2. **Tiling along one axis.** `Tiled` repeats both. *v1 got:* a tiled
   nine-slice whose edges tile along their side and stretch across it
   (section 5.4). #983.
3. **An icon named by path inside a string.** *v1 got:* `${path}` in any UI
   string, with nothing registered first (section 5.1). #983.
4. **A language for part of a string.** `canvas.Span` has none. *v1 got:*
   `$[ja]{...}` in any UI string, such as a player's name in another script
   (section 5.1). #983.
5. **Reading an image's slice insets back.** Insets moved into the interned
   `Image`, which is opaque. *v1 got:* a nine-slice's natural size from the
   insets in its params (section 5.2). #983.
6. **One place that says how large the UI's screen is** before anything is
   drawn, and which window is the main one. *v1 got:* the root's rect, the
   pointer's conversion and the pixel scale from `gfx.Viewport` (section 4).
   #984.
7. **Pause and single step.** *v1 got:* `ui_layout` answering while the game was
   paused, in a step shared with the other snapshots (section 2). #989.
8. **Commands and an MCP provider.** *v1 got:* the `ui_layout` tool, paired by
   tick with `canvas_draws` and `gfx_frame` (section 7). #989.
9. **A type's printable name.** v2's kernel has no `TypeName`. *v1 got:* the
   name of each element's visual in the snapshot (section 7). #989.
10. **An error the default logger survives.** *v1 got:* a missing sprite or
    font reported once and drawn as nothing, the game carrying on. Under v2's
    `StdLogger` every `LogErr` panics (section 6). No ticket holds it.

### Needed by the map, offered by neither v1 nor v2

- **A text's narrowest width.** No call answers it (section 5.1). #980, #981.
- **Snapping to framebuffer pixels in canvas.** v1's UI snapped its own rects.
  canvas leaves non-pixel-art snapping out on purpose (section 5.7). #982.
- **Text positions for a field** beyond `ClusterPosition`: no cluster count, no
  point-to-cluster, no line of a cluster (section 5.1). #988.
- **Gamepads.** v1's input had them, v1's UI never read them, and v2's platform
  has none (section 3). #979, #987.
- **A pointer cancel.** gogpu reports nothing for one (section 3). #985.

### What v2 would trip a design over

- **A run wrapped at the width canvas itself measured can break onto two
  lines** *(ran)*. At an em of 15, up to 320 of 3,000 runs did. The map's
  acceptance case needs "longest item 200: each on one line" (section 5.1).
- **`canvas.Shape` with a zero `Tint` draws nothing.** v1's zero colour drew
  white (section 5.3).
- **A zero `Clip` means no clip,** so an empty clip at the origin clips nothing
  (section 5.5).
- **A frame that sets any camera loses the default camera,** and a camera with
  no layer ranges draws every layer (section 5.8).
- **The layout cache ages by canvas's frames,** so measuring on a slower beat
  shapes again each time (section 5.1).
- **The documented wiring gives no dependency from input to drawing:**
  `canvas.FrameEvent` handlers run before the game's handler has scheduled the
  pump (section 2).
- **`InputState`'s getters given the zero `Window` panic** unless exactly one
  window is known, none included (section 3).

---

## 1. v1's kernel

### 1.1 What ui takes

| Use | Where |
|---|---|
| Name, `Dependencies` (app, input, gfx, canvas), `Register`, `Stop` | `ui/internal/plugin.go` L36-69 |
| `InitResource` of `*Frame`, `*Interactions`, private `*processor` | L44-46 |
| `Subscribe[ProcessOnUpdate]`, `After[input.AdvanceOnUpdate]`, `Before[canvas.FlushOnUpdate]` | L48-50 |
| `Subscribe(...).First()` and `After[ProcessOnUpdate]` for the snapshot | L51-53 |
| One handler that locks eight resources and runs | L102-157 |
| `HandleCommand[ArmLayoutCmd]`, `ExecuteCommand[app.TimeCmd]` | `commandsimpl.go`; `mcpprovider.go` L135, L191-211 |
| `ProvideAdapter[McpProvider]` | `plugin.go` L54 |
| `kernel.TypeName` | `snapshot.go` |

ui has no `Config` and publishes no event. The exported subscription type
`ProcessOnUpdate` is how a producer orders before it and a reader after it
(`ui/internal/id.go` L11-18).

### 1.2 What v2 offers

- **Plugin.** `NewPlugin()` in the plugin's package, an optional `*Config`,
  `Init`, `Start`, `Stop`, `Destroy`, and `Dependencies` naming the plugins
  whose resources `Init` needs (root `AGENTS.md`; `kernel/docs/design.md`
  *Plugins*, L120-158).
- **Handlers lock nothing.** A handler schedules tasks
  (`kernel/docs/design.md` L441-442). A task declares locks through
  `kernel.Read` and `kernel.Write` in a plain function of up to eight
  parameters (`RegisterTask8`, `kernel/configurator_gen.go` L184), or through a
  constructor with `RegisterTask`. Bodies are named functions in `tasks.go`.
- **Ordering.** `OrderSubscriptions(before, after)` orders two handlers of one
  event (`kernel/configurator.go` L205). Where nothing decides, plugin order
  does (design L417-424). A plugin publishes its `Sub`s in a `Handlers`
  resource (`kernel/AGENTS.md`). There is no `First` or `Last`.
- **A job waits only for what it is given.** Jobs whose locks conflict run *"in
  no guaranteed order"*, the one promise being that a waiting writer is not
  overtaken (design L246-250). So v1's *"a producer runs before ui"* and *"a
  reader runs after ui"* need handler order **and** each side joining its job
  into its out dependency. Several producers writing one frame resource have no
  order among themselves without it, and root order decides draw and hit order
  within a layer. canvas avoids this with a command list per recording task,
  merged in registration order (`plugins/canvas/commandlist.go` L18-31). #984.
- **Task args, results and events are pointer-free** (design L261-282). An
  `Element` tree cannot be a task's arg or result, so a `Measure` over one is a
  function or a custom `Parameter`, not a task. An `Interaction`, with a string
  ID and `userData any`, cannot be an event, so results stay in a resource.
  #984, #985.
- **Resources are not kept** outside the function that reached them (root
  `AGENTS.md`). v1's frame holds the app's child slices from the producer's
  handler to ui's (`ui/docs/README.md` L63-66). A v2 resource can hold the same
  slices, but the memory they point at is under no lock: a job of the producer
  can change it while ui's job reads it. #978, #984.
- **A task offered to others** is a field of the plugin's `Tasks` resource, as
  `platform.Tasks{ProcessEvents}` (`plugins/platform/resources.go` L9-18) and
  mux's `Tasks.Step`, which takes the delta as its arg.
- **A panicking job is recovered** and logged, and answers the zero result
  (design L251-253).

---

## 2. `slots/app`, metronome and the frame

### 2.1 What ui takes

- **`app.UpdateEvent`** (`Dt`, `Last`, `Tick`;
  `_legacy/slots/app/internal/events.go` L10). All of ui's work hangs off it,
  on every tick, catch-up ticks included (`ui/docs/README.md` L80-83). In that
  one handler ui measures, arranges, snaps, resolves clips and order, hit-tests
  the pointer, draws, publishes interactions and clears the frame
  (`ui/internal/layout.go` L38-49). Hover therefore shows in the frame it
  happens. `Tick` names the snapshot's moment. `Dt` is what a game hands
  `HoverTracker.Update` (`hovertracker.go` L43).
- **`app.TimeCmd`**, used by `ui_layout` alone, to read paused-ness and to step
  a paused game once (`mcpprovider.go` L135-153, L191-211).

### 2.2 What v2 offers

- **Beats.** A metronome track publishes an event embedding `metronome.Beat`:
  `Tick`, `Delta` as a `time.Duration`, `Lost`, `Alpha`, `InterpolatedTick`
  (`plugins/metronome/beat.go` L7-42). The application declares tracks.
  metronome ships `UpdateEvent` only.
- **Drawing has its own window in time.** `canvas.FrameEvent` is published
  inside `render.FrameEvent`, inside gfx's draw event, on the render beat
  (`plugins/canvas/events.go` L10-26). Its handlers record after `Ready` and
  join their jobs into the out dependency. *"An entry recorded by a task not
  joined so, which runs once canvas's drawing has begun, is refused."* An entry
  recorded on an update track is refused too (`plugins/canvas/docs/design.md`
  L286-294). So v1's one pass per update tick has no place: whatever ui draws,
  it records on the render beat. #984.
- **Pacing precedents.** A plugin ships its own track event (gfx's
  `RenderEvent`); offers a task the application schedules (platform, mux); or
  publishes an event of its own inside another (render, canvas).
- **Handler order on `canvas.FrameEvent` is plugin order.** ui would be listed
  after canvas, and a game that reads ui's resources after ui, so if ui
  processed on that event its handler would run first unless an order were
  declared. A game declaring its tree in the same event would have to be
  ordered before ui, or declare somewhere earlier. #984.
- **The pump and the frame.** The canvas guide schedules
  `platform.Tasks.ProcessEvents` from the game's `gfx.RenderEvent` handler
  (`plugins/canvas/docs/README.md` L183-187). The game is listed after gfx, so
  gfx's handler has already run, and with it `DrawEvent`, `render.FrameEvent`
  and `canvas.FrameEvent`, nested. No dependency makes a job scheduled from a
  `canvas.FrameEvent` handler wait for the pump. Only the lock reservation
  orders them. A dependency exists if the pumping handler is ordered before
  `gfx.Handlers.Draw`, since gfx hands its `in` to the nested events
  (`plugins/gfx/framepool.go` L377-379). #984.
- **No pause.** metronome has no pause, time scale or single step
  (`plugins/metronome/docs/design.md` L24, L294). A test uses a `ManualClock`,
  which is chosen at `Spawn`. **Gap** for a running game (7). #989.

---

## 3. `bundles/input` and platform

### 3.1 What ui takes

(`ui/internal/plugin.go` L106, L124-184.)

- `State.Pointer()`, one position, the latest, in window units.
- `State.JustPressed` and `JustReleased` for five mouse keys, edges promoted
  once per tick, so every handler after `input.AdvanceOnUpdate` sees the same
  ones (`_legacy/bundles/input/internal/state.go` L146).
- Nothing else. Both edges are placed at the current pointer position, and a
  tick's downs are processed before its ups.

ui reads no keyboard key, text, scroll or gamepad, though v1's input has all
four.

### 3.2 What v2 offers

| v1 | v2 |
|---|---|
| `Pointer()` | `InputState.PointerPosition(window)` and `IsPointerInside(window)`, in the window's points (`plugins/platform/inputstate.go` L63-66); `PointerMoveEvent`, one per window per pump |
| `JustPressed`, `JustReleased` of mouse keys | The same on `InputState`, `KeyMouseLeft` to `KeyMouse5`; or `KeyEvent`, which carries the pointer's position at the click and `Repeat` (`events.go` L36-52) |
| `Scroll()` (unused) | `ScrollEvent` only, in lines or points (`events.go` L88) |
| `Text()` (unused) | `TextEvent` only, one committed character (L57) |
| Gamepads (unused) | **None.** *"Gamepads and the clipboard: later"* (`plugins/platform/docs/design.md` L483) |

Facts a design meets:

- **Edges hold only in the update that ran the pump.** *"A job of another
  track, or of a later beat that ran no pump, sees the last run's edges again,
  and every step of a fixed-step catch-up sees the same ones. Such a reader
  folds `KeyEvent`s in its own handler instead"* (`plugins/platform/docs/README.md`,
  *Input*; `inputstate.go` L23 and the comment above it). #984.
- **Several windows.** Every pointer and key event names its `Window`, and each
  window keeps its own pointer. The zero `Window` reads the only window known
  and **panics unless exactly one is** (`inputstate.go` L157), so also before
  the first pump. #984, #985.
- **Focus loss releases every held key,** mouse buttons at the pointer's last
  position, before the `FocusEvent` (`plugins/platform/eventprocessor.go`
  L210-229). The release looks like any other, so v1's rule would report a
  `Click` if the pointer is still over the captured element. #985.
- **A captured drag** reports positions outside the window.
- **A cancelled pointer reports nothing** (`plugins/gogpu/docs/design.md`
  L224). A press it cancels gets no release. #985.
- **Committed IME text, the emoji picker and dictation** arrive on Windows only
  (gogpu design L221). #988.
- **Cursor shapes** exist, `CursorShapePointer` and `CursorShapeText` among
  them, set under the `WindowManager`'s write lock
  (`windowmanager.go` L111). v1's ui set none. #985.
- **No shipped scripted input.** A test binds its own
  `InputSourceSocket`; the only fake is in platform's own tests
  (`plugins/platform/platform_test.go` L126), and `platform/headless` has
  windows alone. v1's tests of ui handed the engine a `PointerState` directly.
  #990.

---

## 4. `slots/gfx`

### 4.1 What ui takes

- **`gfx.Viewport`** (`Width`, `Height`, `WindowWidth`, `WindowHeight`,
  `FramebufferWidth`, `FramebufferHeight`): the screen rect that roots are laid
  out in, that `StayOnScreen` clamps to and that `IgnoreClip` resets to
  (`ui/internal/plugin.go` L144-153); the pointer's conversion from window
  units (L168-176); the pixel scale for snapping (L161-166). The logical size
  is the game's policy, resolved by gfx: fixed width, fixed height, fit or
  cover (`_legacy/slots/gfx/internal/commandsimpl.go` L107).
- `gfx.FilterMode` in sprite params; `gfx.ShaderParameterColor` to pass tint;
  `gfx.SnapshotView` in the snapshot.

### 4.2 What v2 offers

ui reaches no gfx: *"A plugin that draws in 2D uses canvas, and render for what
canvas does not cover, and never reaches gfx"*
(`plugins/canvas/docs/design.md` L44-45).

- `FilterMode` is `canvas.Filter` (zero is `Config.DefaultFilter`); tint is a
  field of the entry.
- **The logical size is a camera's:** `Camera.Size`, with a zero axis following
  the viewport, and `Fit` (expand, inscribe, overlap, stretch)
  (`plugins/canvas/camera.go` L113-120, L287-306). v1's four policies map onto
  these, but a camera is a value set each frame, not a resource.
- **Window sizes:** `WindowManager.Size`, `FramebufferSize`, `Scale`
  (`plugins/platform/windowmanager.go` L157-167), known once the window has
  opened. `Size` is integer points.
- **As drawn:** `canvas.Targets.Window(window)` answers size and scale as last
  drawn, and nothing before the first draw (`plugins/canvas/resources.go`
  L40-57). `Camera.ScreenToWorld`, `WorldToScreen` and `VisibleRect` read it
  (`camera.go` L149-187).
- **The main window has no accessor.** `WindowManager` lists no windows.
  canvas learns the main one from `WindowOpenedEvent` and
  `WindowClosedEvent` and keeps it private; `Targets.Window(zero)` answers its
  size, not its handle. A plugin that must match pointer events to "the main
  window" follows the events itself. **Gap** (6). #984.

---

## 5. `bundles/canvas`

v1's ui uses: `canvas.LookupAccess` (`SpriteSize`, `MeasureTextSize`,
`MeasureWrappedTextSize`, `FontMetrics`), `canvas.OpQueue` (`SetLayerTransform`,
`SetClip`, `RemoveClip`, `Sprite`, `FillRect`, `Text`), `canvas.Layer`,
`canvas.MaterialSet`, `canvas.SpriteTransform` and `SpriteFrame`, `canvas.TextDraw`
and `ShapeDraw`, `canvas.TintSlot`, `DefaultTint` and the default font path
(`ui/internal/layout.go` L1127-1148, `elements.go`, `element.go` L86-110).

### 5.1 Measuring text

| v1 | v2 |
|---|---|
| `MeasureTextSize(path, size, text)` | `FontLibrary.Layout(canvas.Text{String, Font, Size}).Size()` |
| `MeasureWrappedTextSize(..., width)` | `Layout` with `WrapWidth`. The widest line is the maximum of `LineWidth(i)` |
| `FontMetrics(path, size)` | `FontLibrary.Metrics(set, size, language)`: `Ascent`, `Descent`, `LineGap`, `LineHeight`, `XHeight`, `CapHeight` |
| Baseline computed by ui (`elements.go` L478-485) | `TextLayout.Baseline()` |
| Narrowest width: none | **None** |

(`plugins/canvas/resources.go` L68-101; `textlayout.go` L33-53;
`fontlibrary.go` L94-107.)

**What differs.**

- **`Size().X` is the wrap width** when wrapping, not the widest line
  (`font/shaper.go` L517-522) *(ran)*. v1 answered the widest line
  (`_legacy/bundles/canvas/internal/lookup.go` L327).
- **Height for a width** is one `Layout` call with `WrapWidth`. Zero or less
  means no wrapping, so a width of zero cannot be asked about.
- **No narrowest width.** A very small `WrapWidth` does not give it: a word too
  long for its line breaks between grapheme clusters, so `"hello world"`
  wrapped at 1 unit is eleven lines of one cluster each *(ran)*. The shaper
  passes go-text no break policy (`font/shaper.go` L392), and `font.Input` has
  no field for one.
- **Wrapping at the measured width is not stable** *(ran)*. The shaper turns
  `WrapWidth` into 26.6 fixed point by truncating a float division
  (`font/shaper.go` L204-205). Lay a run out unwrapped, then lay it out again
  with `WrapWidth` set to the `Size().X` it answered:

  | em | 10, 12, 14, 16, 20, 24, 32 | 11 | 13 | 15 | 18 |
  |---|---|---|---|---|---|
  | JetBrains Mono, of 3,000 runs on two lines | 0 | 2 | 4 | 69 | 0 |
  | Noto Sans JP, of 3,000 | 0 | 141 | 255 | 320 | 39 |

  Over 20,000 runs of mixed sizes, rewrapping at the widest line of an already
  wrapped layout changed the lines in 4% to 5%. The next `float32` above the
  measured width never broke an unwrapped run. A wrapped one still came out
  differently in 5 (JetBrains Mono) and 54 (Noto Sans JP) cases; those looked
  at had a width narrower than a word, where a space can take an empty line of
  its own. canvas's design says *"Measuring and drawing read the same layout,
  so they never disagree"* (L792); they agree only for equal inputs. v1 met a
  cousin of this and wraps one unit wider than the rect
  (`ui/internal/elements.go` L491-505). #981, and canvas.
- **Trailing spaces.** The docs say `LineWidth` leaves them out
  (`textlayout.go` L43-44). Only the last one is: `"hi "` measures as `"hi"`,
  `"hi  "` as three characters *(ran)*. #988.
- **Break rules** are Unicode's (UAX #14) and grapheme clusters, with bidi, in
  place of v1's words and runes.
- **Line height** is ascent, descent and line gap times `Text.LineHeight`, the
  gap split above and below, so `Baseline()` sits half the gap below the
  ascent. v1's guide says *"A label's baseline is its font's ascent"*
  (`ui/docs/README.md` L184-185). #982.
- **Fonts.** v1's `ui.Font{Path, Size int}` (`elements.go` L93-96) becomes a
  `FontSet` handle and a `float32` `Size`. A set is made on purpose under the
  library's write lock (`resources.go` L103-124); drawing never loads one. A
  zero set is the default set. A zero `Size` is `Config.TextSize`, 16 by
  default, where v1 drew nothing.
- **Alignment** follows the paragraph: `TextAlignStart` is the right edge of a
  right-to-left paragraph (`text.go` L150-157). v1's `TextAlignStart` was always
  the left (`ui/internal/elements.go` L511-519). The map says left and right
  keep their names. #983.
- **Icons** are registered by name (`WriteFontLibrary.RegisterIcon`,
  `resources.go` L145) and placed by `\i:name` in a run with `Markup` set, or by
  `Span.Icon`. `${path}` is plain text in v2. **Gap** (3).
- **Language** is per run (`Text.Language`, `text.go` L50) or the library's
  current one. `Span` has `Text`, `Tint` and `Icon` only, and markup has
  `\i:` and `\c:` only. **Gap** (4).
- **For a text field** (v1 has none), `TextLayout` answers `ClusterPosition(i)`,
  `LineCount` and `LineWidth`. It answers no cluster count (an index past the
  end panics), no cluster at a point and no line of a cluster. #988.

**Locks, cost and allocation.**

- Measuring takes `kernel.Read[canvas.FontLibrary]`. v1's ui held write locks
  on `canvas.Lookup` and `OpQueue` while it laid out. `CommandList` declares the
  library itself but does not hand it out, so a task that records and measures
  declares both.
- The layout cache is keyed by the string, set, size, `WrapWidth`, `Align`,
  `LineHeight`, `Tracking` and language (`layoutcache.go` L35-45). Each width a
  pass tries is a separate shaping and a separate entry; shaping is not reused
  across widths. `Align` is in the key too, so measuring with one and drawing
  with another shapes twice. #981.
- On a warm cache `Layout` (plain, wrapped and with spans), `Metrics`,
  `ImageSize` and `ImageFromFile` allocated nothing *(ran)*. canvas's
  allocation gate records text with `DrawText` but measures with none of
  `Layout`, `Metrics` and `ImageSize` (`allocationgate_test.go`,
  `gaterecording_test.go`). A miss allocates. #990.
- An entry unused for `Config.LayoutIdleFrames` frames (2 by default) is freed,
  **counted in canvas's drawing jobs** (`layoutcache.go` L90-100, `tasks.go`
  L140). A run measured on a 60 Hz update track under a 240 Hz render track
  would be shaped again each update. #984.

### 5.2 Sprite sizes

- v1: `lookup.SpriteSize(path)`, from the file's header, zero and reported once
  when it fails (`_legacy/bundles/canvas/internal/lookup.go` L265).
- v2: `canvas.Assets.ImageSize(image) (m.Vec2, bool)`, from the header, of the
  image's `SourceRect` or one sheet frame
  (`plugins/canvas/assets.go` L120-140). The zero `Image` is 1 by 1, the white
  texel. `Assets` is a task parameter that reads canvas's draw state,
  `platform.FS` and render's frame and inventory (L22-37).
- An `Image` is interned from a path and `ImageOptions` and is never released
  (`imageoptions.go` L129; `interntable.go` L5-7). `ImageFromFile`'s doc says
  *"a game makes an Image once, as it names its art"*: naming one by path each
  frame takes a lock and hashes the path. v1's sprite params name a path. #983.
- `Image` has no method and canvas exports nothing that answers its options. A
  visual handed an `Image` can learn its frame's size, not its slice insets or
  hollow centre. v1's nine-slice measured itself as the sum of its insets
  (`ui/internal/elements.go` L272-278). **Gap** (5).
- A tiled nine-slice's insets default to the corner images' sizes
  (`elements.go` L367-392), which `ImageSize` covers.

### 5.3 Materials and tint

**v1.** `canvas.MaterialSet{Sprite, Triangles, Texture *Material, Params}`.
ui inherits a set down the tree, seeded by `Frame.SetMaterial`, and every
built-in passes the `Sprite` slot, since *"a sprite, a nine-slice, a glyph run
and a fill are all sprite draws"* (`ui/docs/README.md` L123-156). A custom
visual reads the set, params included, from `State.Materials`
(`ui/internal/element.go` L170-186).

**v2.**

- A `canvas.Material` is a handle made at run time by `Assets.MakeMaterial`
  from a shader path, a blend and an argument type, and given back with
  `ReleaseMaterial` (`assets.go` L154-170; `materialdescriptor.go` L10).
- An entry names **one** material. There is no set, and no layer-wide or
  frame-wide material. The zero is the default material, or `TextMaterial` for
  a run.
- A material's parameters are set per material (`SetMaterialTexture`,
  `SetMaterialUniform`) and *"stand when canvas draws"*, so one material holds
  one value per frame. Two subtrees fading by different amounts need two
  materials or a per-entry argument.
- A per-entry argument is typed: `DrawSpriteWith[T]` and its siblings
  (`commandlist.go` L72-82), whose `T` must be the material's declared type
  (L531). There is no call taking bytes or `any`, so a visual that does not
  know `T` cannot pass an inherited argument.
- **A material on text draws glyphs only if it was written for them.** A run's
  batch binds the glyph atlas as `canvas_atlas` (`commandlist.go` L549-556), an
  `R8Unorm` distance field (design L695). `canvas_sample` returns the raw texel
  (`assets/canvas.wgsl` L351), so a material written for images would draw each
  glyph's quad as the field's value in red. `TextMaterial`'s coverage, outline
  and small-text pen snapping live in `assets/text.wgsl` L43-124, not in the
  include. `Text.Material` says each glyph is a quad of its field
  (`text.go` L78-84), while the design says *"a fragment-only material serves
  sprites, glyphs and meshes alike"* (L591). Read, not run. **Gap** (1). #983.

**Tint.**

- v2's `Tint` is a linear `m.Color`, straight alpha, multiplying the image
  (`assets/default.wgsl`). Zero means white for sprites, meshes, triangles and
  text (`instancedata.go` L141-144).
- **For a `Shape` zero is transparent** (L105-108, passed through as it is).
  v1's `ShapeDraw` read a zero colour as opaque white
  (`_legacy/bundles/canvas/docs/README.md` L214), so v1's `ColorPanel` with no
  colour drew white.
- v1's ramp is `sampled.rgb * tint.rgb / 0.1529` with plain alpha
  (`_legacy/bundles/canvas/internal/builtin/canvas/tint.wgsl` L29): a multiply
  whose neutral is `#6D6D6D`. A v1 tint reaches the same product in v2 divided
  by that number, values above 1 included. v1's `defaultTint` for a zero tint
  (`ui/internal/elements.go` L563-568) is v2's zero.
- v2 blends in encoded space on a window, v1 in linear
  (`plugins/canvas/docs/design.md`, *Blending in the target's space*), so
  translucent UI does not look the same.
- v1's guide says 0.5 leaves art as painted (`ui/docs/README.md` L160-163). Its
  code says `#6D6D6D`. The guide is stale.

### 5.4 Sprite draw modes

| v1 `SpriteTransform`, as ui sets it | v2 |
|---|---|
| `Position`, `Size`, `Origin`, `Rotation` | `Transform m.Mat2x3` and `Size`; the pivot is the image's, centre by default (`sprite.go` L8-57) |
| `FlipX` | A negative scale in `Transform` |
| `Filter` | `Sprite.Filter` |
| `Frame` (pixel sub-rect, per draw) | `ImageOptions.SourceRect` or `Sheet` with `Sprite.Frame`; part of the interned image (`imageoptions.go` L19-55) |
| `Frame` as `SpriteCover`'s crop, computed from the rect (`elements.go` L588-605) | `Sprite.Crop`, fractions per draw, over a larger `Size`. A `SourceRect` per size would intern without end |
| `NineSlice`, `NineSliceNoCenter` (per draw) | `ImageOptions.Slice`, `SliceHollow` (per image), `DrawMode: Sliced` |
| `NineSliceScale`; `Scale` of a tile | `TexelScale` |
| `TileX`, `TileY` | `DrawMode: Tiled`, **both axes** (`sprite.go` L122-140; `instancedata.go` L168-170) |

v1's tiled nine-slice draws its top and bottom with `TileX` alone and its sides
with `TileY` alone (`ui/internal/elements.go` L358-362), so each stretches
across (`tiledRepeat`, `_legacy/bundles/canvas/internal/plugin.go` L823). v2 differs
whenever an edge image's cross size, times the texel scale, is not the inset it
is drawn into, or the rect is too small for the corners. **Gap** (2).

`SpriteContain` is a rect computation that needs only the image's size. A
sliced sprite in v2 is one instance, and corners larger than the sprite shrink.
`DrawMode` is one of three, as v1 also refused tiling with a nine-slice.

### 5.5 Clipping

**v1.** ui calls `queue.SetClip(node.clip)` before each visual and `RemoveClip`
after (`ui/internal/layout.go` L1141-1145). canvas tests the clip in the
fragment shader, so a custom material that skips the test draws outside it.

**v2.**

- Every entry has `Clip m.Rect`, in world units; there is no ambient clip
  (`shape.go` L41, and the same field on `Sprite`, `Text`, `Mesh` and
  `DrawTrianglesOptions`). A custom visual that leaves it out is not clipped.
- It is render's scissor, so no material can ignore it. A change of clip breaks
  a batch. Under a rotated camera it is the clip's bounding box. canvas culls an
  entry whose bounds miss it (design L999-1010).
- **Zero means no clip.** ui's `intersect` answers a rect of zero size at the
  larger of the two corners (`ui/internal/layout.go` L1464-1470). At the
  origin, where a UI camera's top left is, that is `m.Rect{}`: the children of
  an empty element there would be unclipped. #982.
- `IgnoreClip`'s *"the screen"* needs no rect in v2: zero is the camera's
  viewport.

### 5.6 Layers and order

**v1.** A `canvas.Layer` is a `gfx.Order`, one pass per layer. ui calls
`SetLayerTransform(layer, screen, AspectStretch)` on each layer it draws into
(L1136-1140), sorts its nodes by layer then declaration order, and records in
that order; hit testing walks it backwards (L979-1003, L1098-1121).

**v2.**

- An entry's sort key is `Layer int32`, then `Order int32`, then the command
  list's order (task registration: plugin order, then registration order), then
  recording order (design L319-337). A game listed after ui therefore draws
  over ui at an equal layer and order.
- *"Equal keys keep their call order"* unless the layer is in
  `Config.RegroupedLayers`, which canvas's docs suggest for a UI whose order
  does not matter (design L337; `plugin.go` L24). v1's guide promises
  declaration order on a layer (`ui/docs/README.md` L117-121).
- **Layers have no transform and no target.** A camera does: it draws up to
  four half-open layer ranges (`camera.go` L35-71). A UI's base layers and
  offsets, such as a tooltip at `Layer(500)`, must fall inside its camera's
  ranges.
- A camera has a `render.Order`. Another plugin draws between 2D entries only
  between camera orders, not between two layers as in v1.
- Sprites and shapes batch together; text binds another atlas and material, and
  every change of material, atlas, filter or clip in sort order is a new draw
  (design L1028).

### 5.7 Pixel snapping

**v1.** Not in the guide, but in the code. ui rounds every rect's edges to
device pixels with `PixelScale = FramebufferHeight / Height`
(`ui/internal/layout.go` L55-61, L561-582), *"a solid fill and a sprite round a
half pixel differently"*, and wraps text one unit wider than the snapped rect
(`elements.go` L491-505).

**v2.**

- `Camera.PixelPerfect` is for pixel art: it floors the scale to a whole number
  and snaps every entry to the world-unit grid (`camera.go` L121-133). It is not
  edge snapping at a fractional DPI.
- canvas lists *"snapping a non-pixel-art UI to framebuffer pixels at a
  fractional DPI"* among what it left out (design L1232).
- `TextMaterial` moves the pen of a small, unturned glyph to a whole pixel
  (design L711-714). Nothing else snaps.
- A UI can snap what it records, as v1 did. No `Camera` method answers pixels
  per world unit; it follows from `WorldToScreen` of two points times the scale
  from `Targets`, or is `WindowManager.Scale` for a camera at one unit to a
  point. An inspection reports it (`InspectedCamera.Scale`). #982.

### 5.8 Cameras and windows

**v1** had none for ui: one window, one viewport, a transform per layer.

**v2.**

- A `Camera` is a value set for one frame with `CommandList.SetCamera`:
  `Position`, `Rotation`, `Zoom`, `Pivot`, `Layers`, `Window` or `Texture`,
  `Viewport`, `Clear`, `SampleCount`, `Order`, `Size`, `Fit`, `PixelPerfect`
  (`camera.go` L51-133). `Pivot{-0.5, -0.5}` with a zero `Size` makes world
  units a window's points from its top left, which is the pointer's space, so
  no conversion is needed when the viewport is the whole window.
- **The default camera applies only to a frame that sets none**
  (`drawstate.go` L111-113). If ui sets a camera, a game that relied on the
  default is seen only through cameras its layers fall in. A camera with four
  empty ranges, the zero value, draws every layer: the game's would draw ui's
  entries in world space, and ui's would draw the game's in UI space. Who sets
  the UI camera and how layers are divided must be said. #984.
- A pointer converts through `Camera.ScreenToWorld(point, targets)`, which
  needs the camera value and `kernel.Read[canvas.Targets]`. A click is routed by
  trying cameras from the highest `Order` down (design, *Conversions and
  Targets*).
- A camera may draw into a render texture. Pointer events are window space
  only, so a UI drawn into a texture gets no pointer by itself.

### 5.9 Custom visuals

v1's contract is `DefaultSize(lookup, params)` and
`Draw(lookup, queue, state, params)`, with an optional `Baseline`
(`ui/internal/element.go` L86-110). The plugin sets the layer's transform and
the clip around each `Draw`, and *"a custom visual appends operations ... it
does not reset the queue or change clipping"* (`ui/docs/README.md` L319-321).

In v2:

- Recording goes through `canvas.CommandList`, a task parameter that is *"valid
  only while the task's job runs"* (`commandlist.go` L18-47). A visual drawing
  for ui records into the list of ui's task.
- Layer, order, clip and material are on every entry. Nothing is set around a
  visual.
- What `LookupAccess` was is split: `Assets` (image sizes, materials) and
  `FontLibrary` (layout, metrics).
- A visual is given no locks of its own: one that needs a game resource while
  drawing has it only through its params, as in v1.
- `SpriteTexture` and `DrawTexture` are an `Image` from `ImageFromTexture`;
  `DrawTriangles` takes `Triangles[V]` with its bounds required.

---

## 6. Storage, assets and reports

- **`storage.FileSystem`.** ui reads it only to build `LookupAccess`
  (`ui/internal/plugin.go` L110, L127). In v2 canvas's parameters declare
  `platform.FS` themselves; ui needs it only if it makes font sets
  (`ElevateFontLibrary`).
- **`_legacy/libs/assets`.** ui imports it nowhere. Through canvas it gets lazy
  loading, a failure reported once and terminal, and unloading. v2's canvas loads
  an image at its first draw, offers `Preload`, `Unload` and `ReloadChanged`,
  draws a missing image magenta, and answers `ImageSize` false for it.
- **Reports.** v1's canvas reports and carries on
  (`_legacy/bundles/canvas/docs/README.md`, *Lookup API*, L759). In v2 render,
  which loads images, logs through `LogErr` (`plugins/render/reporter.go` L23)
  and canvas through `LogErrOnce` (`plugins/canvas/reporter.go` L23), and the
  default `StdLogger` panics on every `LogErr` (`kernel/logger.go` L132-135). A
  missing sprite or an unregistered icon in a UI ends a game under the default
  logger. **Gap** (10). The sound study met the same
  (`plugins/sfx/docs/research/v1-port.md` section 1.3).
- **`libs/m`.** `Maybe`, `Rect`, `Vec2` and `Color` are in v2's `libs/m`.

---

## 7. The MCP provider and the layout snapshot

**v1.** ui offers one capability, `ui_layout`
(`ui/internal/mcpprovider.go` L98-110; `ui/docs/specs/mcp.md`):

- one tick's tree, flat, in pre-order: index and parent, id, active, rect,
  content rect, clip, layer, draw order, visual state by name, the visual's Go
  type, `userData` marshalled under a `recover`, and **what was declared**
  beside what was resolved (`snapshot.go`, `ElementView`, `DeclaredView`);
- filters by subtree and depth, saying what they dropped;
- the pixel, window and logical sizes, and the tick;
- a request that waits for a tick that began after it, one at a time, abandoned
  on `Stop`; under pause it steps once;
- an optional file.

It is built from `ArmLayoutCmd`, two subscriptions around ui's own, and a slot
with its own lock, because the tree's elements live in borrowed storage that is
released when ui's handler returns.

**v2.**

- **No MCP plugin and no commands** (`kernel/docs/design.md` L5-6).
  **Gap** (8). canvas leaves an agent tool to *"a later MCP plugin's, cog-wide"*.
- **The precedent is canvas's and render's inspection:** a task asks with
  `Assets.RequestInspection`, an `InspectedEvent` follows, and
  `ReadInspection` answers only while that event's jobs run
  (`plugins/canvas/assets.go` L227-246; `events.go` L28-35). `String()` writes it
  as text. A frame not inspected pays one check.
- Code outside the kernel waits with a `kernel.Signal`.
- canvas's `InspectedEntry` carries the entry, its layer, order and command-list
  place, and nothing saying which element recorded it
  (`plugins/canvas/inspection.go` L214-229). v1 paired `ui_layout` with
  `canvas_draws` by tick and geometry too.
- v2's kernel has no `TypeName`. **Gap** (9).
- `canvastest.Harness` runs canvas in a real kernel, records, steps and answers
  frames in canvas's terms, reports expected by kind
  (`plugins/canvas/canvastest/harness.go`). #990.

---

## 8. Promises of v1's guide to restate

Lines are of `ui/docs/README.md`. Promises about containers, grids,
interaction states and hit-test rules that rest on ui's own engine are not
listed; the map redesigns sizing apart from this study.

| Lines | The promise | Rests on | What v2 changes | Ticket |
|---|---|---|---|---|
| L3-6, L80-83 | A tree every update tick; processed on every tick, catch-up ones included | `app.UpdateEvent` | Entries only on the render beat; input edges only in the pump's update (2, 3) | #984 |
| L10-45, L68-78 | Packages, name, constructor, dependencies, no config, MCP provider, `PluginStopper`, registration order | v1 kernel | The v2 plugin rules (1.2) | #984 |
| L48-66 | The root is copied; descendant slices may be borrowed and must stay unchanged until processing ends | One handler after another | The borrowed memory is under no lock (1.2) | #978, #984 |
| L85-115 | `*ui.Frame` is written by producers ordered before processing; processing consumes it; an empty frame is still processed | Subscription ordering | Order and dependency both; no order among producers without them (1.2) | #984 |
| L110-121 | `Add(layer, root)`; a child inherits; `Layer(offset)` is from the base; layer then declaration order, shared by drawing and hit testing | `canvas.Layer` | Camera ranges, `RegroupedLayers`, task registration order (5.6) | #982, #984 |
| L123-156 | One material modifier reaches every visual under it; `Frame.SetMaterial` per tick; params carried to custom visuals; no change to batching | `MaterialSet` | One material per entry, none for text and images alike (5.3) | #983 |
| L158-167 | Tint is a ramp; zero draws as painted; tint is per instance and breaks no batch | `canvas.TintSlot` | A multiply; zero white, but transparent on a shape; still per entry (5.3) | #983 |
| L178-187 | `AlignBaseline`; a label's baseline is its font's ascent | `FontMetrics` | `TextLayout.Baseline()`, half a line gap lower (5.1) | #982 |
| L238-244 | `SpriteParams.Scale` affects intrinsic size; `Measure` has no lookup, so sprites and text measure zero | `LookupAccess` | Two read handles would be needed: `Assets`, `FontLibrary` | #983, #984 |
| L246-272 | Edges and pivots; `StayOnScreen` slides inside the viewport; `IgnoreClip` resets to the screen; clips intersect | `gfx.Viewport`, `SetClip` | The screen is a camera's view; zero clip is no clip (4, 5.5) | #982, #984 |
| L275-293 | Built-ins; intrinsic size from the lookup; sprite params; framed nine-slice; `Font{Path, Size}`; `${path}`; `WordWrapping` at the arranged width, `WrapWidth` for measuring | canvas | Image handles, font sets, registered icons (5.1, 5.2, 5.4) | #983 |
| L295-321 | The custom-visual contract; layer, transform and clip set around it | `OpQueue` | `CommandList`; every entry carries them (5.9) | #983 |
| L323-338 | Visual state picks sprite path, tint, colour | Paths in params | An `Image` per state | #983 |
| L367-382 | `Interactions` holds the latest completed update; a reader before ui sees the last tick's, after it this tick's; the sequence is borrowed until the next update | Subscription ordering | A lock orders nothing by itself (1.2) | #984, #985 |
| L384-387 | Buttons 0 to 4; both edges at the pointer's current position; down before up | `input.State` | `KeyEvent` has the click's own position; focus loss releases (3) | #985 |
| L391-440 | `HoverTracker` once per tick with the frame delta; it trails the pointer by a frame | `app.UpdateEvent` | `Beat.Delta` is a `time.Duration`; the beat is open | #985 |
| L442-472 | A fallback tooltip is placed in layout coordinates; its dwell runs only while the position holds still from frame to frame | Pointer space | Per window and camera | #985 |
| L474-487 | Give a wrapped column `MinWidth` equal to `WrapWidth`; measuring wraps at `WrapWidth`, drawing at the box | v1 measure and draw | `Size().X` is the wrap width; the measured width can wrap again (5.1) | #980, #981 |
| L497-558 | `ui_layout` | MCP, `app.TimeCmd`, `gfx.SnapshotView` | None of the three (7) | #989 |
| Not in the guide | Rects snap to device pixels; text wraps a unit wider (`layout.go` L561-582, `elements.go` L491-505) | `gfx.Viewport` | 5.7 | #982 |
| Not in the guide | A zero colour draws white | v1 `ShapeDraw` | Transparent (5.3) | #983 |

Two more statements are tied to v1:

- `_legacy/CONTEXT.md`, *UI Declarations*: **Pixel value** is *"an absolute
  distance in screen pixels"*. ui's units are the logical viewport's, and v2's
  would be a camera's world units. **UI Frame** is *"produced for one update
  tick"*.
- `ui/docs/README.md` L287 says `SpriteParams` controls an origin. It has none
  (`ui/internal/elements.go` L17-28).

---

## 9. For the map: what the open tickets may not hold

1. **Wrapping at the measured width** (5.1) is canvas's behaviour, not ui's
   design. No ticket holds it, and the map's *What canvas must add* names only a
   narrowest width and pixel snapping.
2. **A material on text** (5.3). #983 lists *"one 2D material per entry ... and
   no material set"*. The sharper fact is that a material written for images
   cannot be put on a label at all. Its *"tint as a plain multiply"* is not a
   loss: v1's ramp is a multiply too.
3. **Icons and language spans in strings** (5.1). No ticket names them.
4. **Whose camera, and the layers it shares with the game** (5.8). #984 says
   *"windows and cameras"*; the loss of the default camera is the trap in it.
5. **The error level of a missing asset** (6). No ticket holds it, here or in
   the sound map.
6. **Releases the user did not make, and cancels never reported** (3), for
   #985.
7. **Measuring and the layout cache's age** (5.1) ties #981's passes to #984's
   beat: where ui measures decides whether a steady frame allocates.
