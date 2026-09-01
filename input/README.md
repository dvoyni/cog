# input

`github.com/cog-engine/input` is the driver-neutral input plugin. Drivers feed
raw changes through one command; gameplay can poll the `State` resource or
subscribe to discrete events without depending on a windowing implementation.

## Plugin

- Name: `input.Name` (`"input"`)
- Constructor: `input.New() *input.Plugin`
- Plugin dependencies: none
- Go package dependencies: `app`, `kernel`
- Configuration: none

`Plugin.Register` registers `*State`, implements `ApplyCmd`, and subscribes first to
`app.UpdateEvent`.

`Plugin` implements the kernel lifecycle methods `Name`, `Dependencies`, and
`Init`.

## Command Implemented

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
