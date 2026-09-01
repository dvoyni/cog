// Package input is the driver-agnostic input contract: a unified Key space
// (keyboard keys AND mouse buttons), a polled State resource, discrete input
// events, and the Apply command a driver uses to feed input changes. Gameplay
// depends only on this package, never on a specific driver (e.g. wgpu).
//
// All state synchronization goes through the kernel's resource locks — there are
// no mutexes. A driver batches raw input into []Change and runs Apply; the Apply
// handler folds the batch into the State resource (under its write lock) and
// publishes the discrete events. A tick-boundary subscription on app.UpdateEvent
// rolls the per-tick edges (JustPressed/JustReleased).
package input

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// Key identifies a physical input, unifying keyboard keys and mouse buttons so a
// single Pressed/JustPressed/JustReleased path covers both. Keyboard key values
// mirror gogpu/gpucontext.Key (same order/codes) so a driver maps them with a
// plain cast; mouse buttons occupy the negative range so they never collide with
// the non-negative keyboard keys.
type Key int

const KeyUnknown Key = 0

// Letters [1..31].
const (
	KeyA Key = iota + 1
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
)

// Numbers [33..47].
const (
	Key0 Key = iota + 33
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
)

// Function keys [49..80].
const (
	KeyF1 Key = iota + 49
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Navigation [81..112].
const (
	KeyEscape Key = iota + 81
	KeyTab
	KeyBackspace
	KeyEnter
	KeySpace
	KeyInsert
	KeyDelete
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
)

// Modifiers [113..128] (as keys, not the Mods bitmask).
const (
	KeyLeftShift Key = iota + 113
	KeyRightShift
	KeyLeftControl
	KeyRightControl
	KeyLeftAlt
	KeyRightAlt
	KeyLeftSuper
	KeyRightSuper
)

// Punctuation [129..160].
const (
	KeyMinus Key = iota + 129
	KeyEqual
	KeyLeftBracket
	KeyRightBracket
	KeyBackslash
	KeySemicolon
	KeyApostrophe
	KeyGrave
	KeyComma
	KeyPeriod
	KeySlash
)

// Numpad [161..192].
const (
	KeyNumpad0 Key = iota + 161
	KeyNumpad1
	KeyNumpad2
	KeyNumpad3
	KeyNumpad4
	KeyNumpad5
	KeyNumpad6
	KeyNumpad7
	KeyNumpad8
	KeyNumpad9
	KeyNumpadDecimal
	KeyNumpadDivide
	KeyNumpadMultiply
	KeyNumpadSubtract
	KeyNumpadAdd
	KeyNumpadEnter
)

// Lock and other keys [193..208].
const (
	KeyCapsLock Key = iota + 193
	KeyScrollLock
	KeyNumLock
	KeyPrintScreen
	KeyPause
)

// Mouse buttons live in the negative Key range so they never collide with the
// (non-negative) keyboard keys. Values mirror gpucontext.MouseButton negated and
// shifted: MouseButton b maps to Key(-1 - b).
const (
	KeyMouseLeft   Key = -1
	KeyMouseRight  Key = -2
	KeyMouseMiddle Key = -3
	KeyMouse4      Key = -4
	KeyMouse5      Key = -5
)

// Mods is a bitmask of modifier keys held during an input event. Bit positions
// mirror gogpu/gpucontext.Modifiers so a driver maps them with a plain cast.
type Mods uint8

const (
	ModShift Mods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
	ModCapsLock
	ModNumLock
)

// Has reports whether all of x's bits are set in m.
func (m Mods) Has(x Mods) bool { return m&x == x }

// Pos is a pointer position in logical window coordinates (DIP).
type Pos struct{ X, Y float64 }

// changeKind tags the variant of a Change.
type changeKind uint8

const (
	changeKey     changeKind = iota // key/button up or down
	changePointer                   // pointer moved
	changeScroll                    // scroll delta
	changeText                      // text input (one rune)
)

// Change is a single input delta. Build it with the KeyChange/PointerChange/
// ScrollChange/TextChange constructors and pass it to ApplyCmd; its fields are
// unexported because drivers construct changes, they don't inspect them.
type Change struct {
	kind   changeKind
	key    Key
	mods   Mods
	down   bool
	pos    Pos
	dx, dy float64
	r      rune
}

// KeyChange builds a key/button up-or-down change.
func KeyChange(k Key, mods Mods, down bool) Change {
	return Change{kind: changeKey, key: k, mods: mods, down: down}
}

// PointerChange builds a pointer-move change.
func PointerChange(p Pos) Change { return Change{kind: changePointer, pos: p} }

// ScrollChange builds a scroll-delta change.
func ScrollChange(dx, dy float64) Change { return Change{kind: changeScroll, dx: dx, dy: dy} }

// TextChange builds a text-input change for one rune.
func TextChange(r rune) Change { return Change{kind: changeText, r: r} }

// UpdateEventHandler identifies the input plugin's per-tick subscription.
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]
