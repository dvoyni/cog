# input

`github.com/cog-engine/input` is the driver-neutral input plugin. Drivers feed
raw changes through one command; gameplay can poll the `State` resource or
subscribe to discrete events without depending on a windowing implementation.

## Plugin

- Name: `input.Name` (`"input"`)
- Constructor: `input.New() *input.Plugin`
- Plugin dependencies: none
- Go package dependencies: `app`, `kernel`, `mcp`
- Configuration: none

`Plugin.Register` registers `*State`, implements `ApplyCmd`, `SynthesizeCmd`
and `StateCmd`, and subscribes first to `app.UpdateEvent`.

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`, and
`Init`, and `mcp.Provider`.

## Commands Implemented

### `ApplyCmd`

`ApplyRequest{Changes []Change}` folds an ordered batch into `*State` under a
write lock. Its empty response is `ApplyResponse`. For each change, the handler
also asynchronously publishes the matching input event.

Drivers construct changes with:

- `KeyChange(key, mods, down)`
- `PointerChange(pos)`
- `ScrollChange(dx, dy)`
- `TextChange(rune)`

`Change` intentionally has no exported fields; consumers do not inspect driver
input batches.

### `SynthesizeCmd`

`SynthesizeRequest{Actions []Action}` folds one batch of a scripted sequence
into `*State` under one write lock and publishes the same events `ApplyCmd`
does. It answers with `StateResponse`. Use `Play` rather than dispatching it
directly unless you want exactly one tick's worth of steps: a `delay` step in
the request is ignored here, because a handler that slept would sleep under the
write lock.

It does three things a caller building `[]Change` and dispatching `ApplyCmd`
cannot: it derives `Mods` from the live down-set **after** the change is folded,
resolves `move_by` against the pointer under the lock, and reads the resulting
state out of the lock it already holds.

### `StateCmd`

`StateRequest{}` reads the seam under the read lock and answers with the same
`StateResponse`. It changes nothing.

## Scripted Input

A sequence of input steps can be played into the engine through the same seam a
driver's input arrives on, carrying no mark that distinguishes it and no
lifetime of its own. **Tests, replays and demos are first-class callers here** —
this is package API, not agent-facing machinery with a public door.

```go
seam, err := input.Play(k, []input.Action{
	{Do: input.ActionMove, X: 40, Y: 30},
	{Do: input.ActionKeyDown, Key: input.KeyMouseLeft},
	{Do: input.ActionKeyUp, Key: input.KeyMouseLeft},
})
```

`Play(k kernel.Executioner, actions []Action) (StateResponse, error)` validates,
splits and dispatches. It holds no locks; each batch is one `SynthesizeCmd`.

One rule produces every idiom: **consecutive steps with no `delay` between them
fold into one dispatch**, and a `delay` starts another with the wait between
them.

| sequence | what the game sees |
| --- | --- |
| `move, key_down, key_up` | a complete click in one tick |
| `key_down, delay 500, key_up` | a key held across ~30 ticks |
| `move, key_down, delay 100, move, key_up` | a drag |

The seven step kinds are `key_down`, `key_up`, `move`, `move_by`, `scroll`,
`text` and `delay`. Coordinates are window units, the same as `Pos`. `text`
emits text changes only and presses no keys — a game that reads keys wants
`key_down`/`key_up`.

What to expect at the edges:

- **A sequence is refused whole or applied whole.** Every check — unknown step
  kind, negative delay, the caps — runs before the first batch, and the
  response carries no count of steps applied because there is nothing partial
  to report. Releasing a key that is not down stays a silent no-op.
- **One batch folds under one lock hold**, so another source's pointer move
  cannot land between a `move` and the `key_down` after it.
- **The wait is outside every lock**, and it is a select on `k.Context()`: a
  caller that hangs up stops the sequence at the next delay rather than running
  it out.
- **A delay shorter than a frame may not separate ticks**, and while the engine
  is paused no delay separates anything at all — the per-tick edges roll on a
  tick and nothing else. `key_down`, step 1, `key_up`, step 1 against a paused
  engine holds a key for exactly one tick, which no running engine can offer.
- **The caps are 10s of total delay and 256 steps**, both refused up front.
  They bound the window in which a sequence can be orphaned mid-hold: nothing
  unwinds a sequence, so a key pressed and never released stays down, and the
  down-set on every response is how it is found.

## Events Published

- `KeyEvent{Key, Mods, Down}` for key and mouse-button transitions.
- `PointerEvent{Pos}` for pointer movement.
- `ScrollEvent{Dx, Dy}` for scroll deltas.
- `TextEvent{Rune}` for each text-input rune.

The package declares and publishes all four event types. It does not subscribe
to them itself.

## Event Subscribed

`UpdateEventHandler` handles `app.UpdateEvent`. It is registered with `First()`
and writes `*State`, promoting pending presses, releases, scroll, and text into
the current tick before gameplay runs.

## State Resource

`State` is a public alias for the plugin's private resource implementation.
Subscribers should bind `access.GetRead[*input.State]()` and query:

- `Pressed(Key) bool`: whether the key or button is currently held.
- `JustPressed(Key) bool`: transitioned down during this tick.
- `JustReleased(Key) bool`: transitioned up during this tick.
- `Pointer() Pos`: current logical-window pointer position.
- `Scroll() (dx, dy float64)`: accumulated scroll for this tick.
- `Text() []rune`: text entered during this tick.

Pressed state and pointer position are live. Edge, scroll, and text values are
tick-scoped.

## Keys And Modifiers

`Key` is a unified integer key space. Keyboard values mirror
`gpucontext.Key`; mouse buttons use negative values.

- Letters: `KeyA` through `KeyZ`.
- Digits: `Key0` through `Key9`.
- Functions: `KeyF1` through `KeyF12`.
- Navigation/editing: `KeyEscape`, `KeyTab`, `KeyBackspace`, `KeyEnter`,
  `KeySpace`, `KeyInsert`, `KeyDelete`, `KeyHome`, `KeyEnd`, `KeyPageUp`,
  `KeyPageDown`, `KeyLeft`, `KeyRight`, `KeyUp`, `KeyDown`.
- Modifier keys: `KeyLeftShift`, `KeyRightShift`, `KeyLeftControl`,
  `KeyRightControl`, `KeyLeftAlt`, `KeyRightAlt`, `KeyLeftSuper`,
  `KeyRightSuper`.
- Punctuation: `KeyMinus`, `KeyEqual`, `KeyLeftBracket`, `KeyRightBracket`,
  `KeyBackslash`, `KeySemicolon`, `KeyApostrophe`, `KeyGrave`, `KeyComma`,
  `KeyPeriod`, `KeySlash`.
- Numpad: `KeyNumpad0` through `KeyNumpad9`, `KeyNumpadDecimal`,
  `KeyNumpadDivide`, `KeyNumpadMultiply`, `KeyNumpadSubtract`, `KeyNumpadAdd`,
  `KeyNumpadEnter`.
- Other: `KeyUnknown`, `KeyCapsLock`, `KeyScrollLock`, `KeyNumLock`,
  `KeyPrintScreen`, `KeyPause`.
- Mouse: `KeyMouseLeft`, `KeyMouseRight`, `KeyMouseMiddle`, `KeyMouse4`,
  `KeyMouse5`.

`Mods` is a bitmask with `ModShift`, `ModCtrl`, `ModAlt`, `ModSuper`,
`ModCapsLock`, and `ModNumLock`. `Mods.Has(x)` reports whether all bits in `x`
are set. `Pos{X, Y float64}` uses logical window coordinates.

A driver carries `Mods` from the platform. `SynthesizeCmd` has no platform to
carry them from, so it derives them from the live down-set after the change is
folded: `key_down left_control, key_down s` publishes `ModCtrl` on the `s`, and
the held state and the reported modifiers never disagree. `ModCapsLock` and
`ModNumLock` are never derived, because they report a lock being active rather
than a key being held and nothing here tracks lock state.

### Key Names

`Key` has a name and a parse, exported because config files and debug tools
want them as much as a script does:

```go
func (k Key) String() string                // "w", "escape", "mouse_left", "#204"
func (k Key) MarshalText() ([]byte, error)
func (k *Key) UnmarshalText(b []byte) error
func ParseKey(s string) (Key, error)
```

Names are lowercase and `snake_case`: `w`, `0`, `f1`, `escape`, `enter`,
`space`, `left_control`, `numpad_add`, `caps_lock`, `mouse_left`. `Key`
implements `encoding.TextMarshaler`, so it is a JSON **string** everywhere it
appears — in an `Action`, and in a response's down-set.

The table has deliberate gaps and a driver's cast can produce a `Key` no name
covers, so `#<n>` is printed for one and accepted back, negative values
included.

> **The number is a cog `input.Key` and nothing else.** Letters are `iota + 1`,
> so **`#13` is the letter `M`** — while ASCII 13, a browser `keyCode` 13 and
> USB HID usage 0x28 all say Enter, and `KeyEnter` is **84**. Use names.

## Offered To An Agent

`input` implements `mcp.Provider` and offers two capabilities, rendered as the
tools `input_send` and `input_state`.

- **`input_send`** is an `mcp.Func` over `Play` — it cannot be an `mcp.Command`,
  because the wait between batches must happen outside every lock. It is
  **not** `mcp.ReadOnly()`: changing the game is its purpose.
- **`input_state`** is an `mcp.Command` over `StateCmd` with no glue, and it
  **is** `mcp.ReadOnly()`. MCP annotates a tool rather than an argument, which
  is why looking and pressing are two capabilities: *is `w` actually down?* is
  worth asking cheaply and often.

Both answer with `StateResponse`, and neither binds to a frame — a capture
binds to a tick that began after its own request, so press-then-look is correct
without either side building a wait. Coordinates are window units, and the
agent converts from a capture's pixels itself using the two sizes the capture
response carries.

The description prose the agent reads is reproduced in full in
[`docs/specs/mcp.md`](docs/specs/mcp.md), so it is reviewed as prompt text. The
feature half is in [`docs/specs/input.md`](docs/specs/input.md).
