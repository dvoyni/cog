// Package gamepad reads physical gamepads on Windows, macOS, Linux and the
// browser, and reports each one in the standard layout: the W3C Gamepad
// remapping, which is SDL's game controller layout under other names.
//
// It is a Library: no plugin, no Kernel. A platform driver polls it once per
// frame on its main thread and turns what changed into input's Changes; game
// code reads gamepads through input, never through here.
//
// The device layer under internal/ is Ebitengine's gamepad and gamepaddb
// packages (Apache-2.0), vendored because they are internal to Ebitengine. It
// is pure Go and cgo-free: XInput and DirectInput on Windows, the
// GameController framework with an IOKit fallback on macOS, evdev on Linux, and
// the Gamepad API in the browser. Mappings for devices the platform does not lay
// out itself come from the embedded SDL_GameControllerDB. See
// internal/README.md for what was taken and what was changed.
package gamepad

import (
	"time"

	"github.com/dvoyni/cog/libs/gamepad/internal/gamepad"
	"github.com/dvoyni/cog/libs/gamepad/internal/gamepaddb"
)

// ID identifies a connected gamepad. It is the gamepad's slot: it stays the
// same while the gamepad is connected, and a slot freed by a disconnection is
// the first one a later connection takes.
type ID int

// Button is a button of the standard layout. Faces are named by position, not
// by label, because the labels differ between pad families: South is A on an
// Xbox pad, Cross on a PlayStation pad and B on a Nintendo pad.
type Button int

// The order is the W3C standard layout's button order.
const (
	ButtonSouth Button = iota
	ButtonEast
	ButtonWest
	ButtonNorth
	ButtonLeftShoulder
	ButtonRightShoulder
	ButtonLeftTrigger
	ButtonRightTrigger
	ButtonBack
	ButtonStart
	ButtonLeftStick
	ButtonRightStick
	ButtonDpadUp
	ButtonDpadDown
	ButtonDpadLeft
	ButtonDpadRight
	ButtonGuide

	// ButtonCount is the number of standard buttons.
	ButtonCount = int(ButtonGuide) + 1
)

// Axis is an analog input of the standard layout. Sticks run from -1 to 1,
// with left and up negative; triggers run from 0 (released) to 1 (fully
// pulled).
type Axis int

const (
	AxisLeftX Axis = iota
	AxisLeftY
	AxisRightX
	AxisRightY
	AxisLeftTrigger
	AxisRightTrigger

	// AxisCount is the number of standard axes.
	AxisCount = int(AxisRightTrigger) + 1
)

// Pad is one gamepad's state at the moment it was polled.
type Pad struct {
	ID ID
	// Name is what the device or its mapping calls itself, such as "Xbox
	// Controller" or "PS5 Controller". It is the only hint of the pad family.
	Name    string
	Buttons [ButtonCount]bool
	Axes    [AxisCount]float64
}

// Poll reads every gamepad and appends the connected ones that have a standard
// layout to pads, in ID order. A gamepad with no standard layout — a device
// neither the platform nor the mapping database knows how to lay out — is
// connected but not reported.
//
// Call it once per frame from one thread, the platform's main thread: the
// macOS backend needs the main run loop, and hot-plug is detected while
// polling. nativeWindow is the platform window handle the Windows backend
// listens to for hot-plug (an HWND); on every other platform it is ignored and
// 0 will do. Before Windows has a window, Poll reports nothing and no error.
//
// An error means the platform layer failed and will keep failing; the caller
// should stop polling.
func Poll(nativeWindow uintptr, pads []Pad) ([]Pad, error) {
	if nativeWindowRequired && nativeWindow == 0 {
		return pads, nil
	}
	if err := gamepad.Update(nativeWindow, nil); err != nil {
		return pads, err
	}
	var ids [8]gamepad.ID
	for _, id := range gamepad.AppendGamepadIDs(ids[:0]) {
		g := gamepad.Get(id)
		if g == nil || !g.IsStandardLayoutAvailable() {
			continue
		}
		pad := Pad{ID: ID(id), Name: g.Name()}
		for b := range ButtonCount {
			pad.Buttons[b] = g.IsStandardButtonPressed(gamepaddb.StandardButton(b))
		}
		for a := AxisLeftX; a <= AxisRightY; a++ {
			pad.Axes[a] = g.StandardAxisValue(gamepaddb.StandardAxis(a))
		}
		pad.Axes[AxisLeftTrigger] = g.StandardButtonValue(gamepaddb.StandardButtonFrontBottomLeft)
		pad.Axes[AxisRightTrigger] = g.StandardButtonValue(gamepaddb.StandardButtonFrontBottomRight)
		pads = append(pads, pad)
	}
	return pads, nil
}

// Vibrate runs the gamepad's rumble motors for duration, the strong
// (low-frequency) one at strong and the weak (high-frequency) one at weak, each
// from 0 to 1. A gamepad that is gone or has no motors ignores it.
func Vibrate(id ID, duration time.Duration, strong, weak float64) {
	if g := gamepad.Get(gamepad.ID(id)); g != nil {
		g.Vibrate(duration, strong, weak)
	}
}
