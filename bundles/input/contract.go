// Package input is the driver-agnostic input contract: a unified Key space
// (keyboard keys AND mouse buttons), a polled State resource, discrete input
// events, and the Apply command a driver uses to feed input changes. Gameplay
// depends only on this package, never on a specific driver (e.g. wgpu).
//
// input is a Bundle. This package is its contract root and declares no plugin;
// the plugin and its handlers are in inputimpl, which only composition roots and
// tests import, and the code the two share is in internal.
//
// All state synchronization goes through the kernel's resource locks — there are
// no mutexes. A driver batches raw input into []Change and runs Apply; the Apply
// handler folds the batch into the State resource (under its write lock) and
// publishes the discrete events. A tick-boundary subscription on app.UpdateEvent,
// AdvanceOnUpdate, rolls the per-tick edges (JustPressed/JustReleased).
//
// The package also plays a scripted sequence of input into the engine through
// that same seam — Action, SynthesizeCmd and Play — carrying no mark that
// distinguishes it from a driver's own and no lifetime of its own. Tests,
// replays and demos are first-class callers; the two capabilities an agent
// reaches it through are an adapter over Play.
package input

import "github.com/dvoyni/cog/bundles/input/internal"

// Mods is a bitmask of modifier keys held during an input event. Bit positions
// mirror gogpu/gpucontext.Modifiers so a driver maps them with a plain cast.
// Mods.Has reports whether all of a mask's bits are set.
type Mods = internal.Mods

const (
	ModShift    = internal.ModShift
	ModCtrl     = internal.ModCtrl
	ModAlt      = internal.ModAlt
	ModSuper    = internal.ModSuper
	ModCapsLock = internal.ModCapsLock
	ModNumLock  = internal.ModNumLock
)

// Pos is a pointer position in logical window coordinates (DIP).
type Pos = internal.Pos

// Change is a single input delta. Build it with the KeyChange/PointerChange/
// ScrollChange/TextChange constructors and pass it to ApplyCmd; its fields are
// unexported because drivers construct changes, they don't inspect them.
type Change = internal.Change

// KeyChange builds a key/button up-or-down change.
func KeyChange(k Key, mods Mods, down bool) Change { return internal.KeyChange(k, mods, down) }

// PointerChange builds a pointer-move change.
func PointerChange(p Pos) Change { return internal.PointerChange(p) }

// ScrollChange builds a scroll-delta change.
func ScrollChange(dx, dy float64) Change { return internal.ScrollChange(dx, dy) }

// TextChange builds a text-input change for one rune.
func TextChange(r rune) Change { return internal.TextChange(r) }
