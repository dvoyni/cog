package input

import (
	"github.com/dvoyni/cog/bundles/input/internal"
	"github.com/dvoyni/cog/kernel"
)

// KeyChange builds a key/button up-or-down change.
func KeyChange(k Key, mods Mods, down bool) Change { return internal.KeyChange(k, mods, down) }

// PointerChange builds a pointer-move change.
func PointerChange(p Pos) Change { return internal.PointerChange(p) }

// ScrollChange builds a scroll-delta change.
func ScrollChange(dx, dy float64) Change { return internal.ScrollChange(dx, dy) }

// TextChange builds a text-input change for one rune.
func TextChange(r rune) Change { return internal.TextChange(r) }

// ClipboardPasteChange builds a change for text pasted from the clipboard.
func ClipboardPasteChange(text string) Change { return internal.ClipboardPasteChange(text) }

// GamepadConnectChange builds a change for a gamepad of type padType
// connecting in slot pad, calling itself name. A driver sends it before any
// button or axis change for that slot; State ignores those for a slot that is
// not connected.
func GamepadConnectChange(pad Gamepad, name string, padType GamepadType) Change {
	return internal.GamepadConnectChange(pad, name, padType)
}

// GamepadDisconnectChange builds a change for the gamepad in slot pad
// disconnecting. The buttons it held are released with it, and each release is
// an edge for the tick. A driver that wants each release published as a
// GamepadButtonEvent sends the button changes first.
func GamepadDisconnectChange(pad Gamepad) Change { return internal.GamepadDisconnectChange(pad) }

// GamepadButtonChange builds a gamepad button up-or-down change.
func GamepadButtonChange(pad Gamepad, button GamepadButton, down bool) Change {
	return internal.GamepadButtonChange(pad, button, down)
}

// GamepadAxisChange builds a change moving a gamepad axis to value.
func GamepadAxisChange(pad Gamepad, axis GamepadAxis, value float64) Change {
	return internal.GamepadAxisChange(pad, axis, value)
}

// ParseKey resolves a key name, or the "#<n>" printed form of an unnamed key.
// It is exported because a name for a key is something config files and debug
// tools want as much as an agent does.
func ParseKey(s string) (Key, error) { return internal.ParseKey(s) }

// Play validates, splits and dispatches a synthetic input sequence, waiting
// between batches. It holds no locks; each batch is one SynthesizeCmd.
//
// The split cannot happen inside the command: a handler that slept would sleep
// under the State write lock, stalling every tick for the delay's duration. So
// splitting, validation and waiting happen here, and an agent-facing capability
// over this is an adapter and nothing more.
//
// A sequence has exactly two outcomes — refused with nothing applied, or
// applied whole — which is why the response carries no count of steps applied.
// Every check runs before the first batch is dispatched, and a refusal is an
// mcp.Unavailable naming the step at fault. The one exception is stated rather
// than papered over: an engine shutting down mid-sequence fails the next
// dispatch, which can leave a key down in a game that is exiting.
//
// A sequence may hold up to 256 steps and delay for up to ten seconds in total.
// Delays are wall clock, not ticks: a delay shorter than a frame may not
// separate ticks, and while the engine is paused no delay separates anything.
// The wait selects on k's context, so a caller that hangs up stops the sequence
// at the next delay.
func Play(k kernel.Executioner, actions []Action) (StateResponse, error) {
	return internal.Play(k, actions)
}
