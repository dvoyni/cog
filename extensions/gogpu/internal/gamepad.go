package internal

import (
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/gamepad"
)

// gamepadFailureKey names the one condition the gamepad poll reports once per
// engine: the platform layer failed, and polling stopped.
type gamepadFailureKey struct{}

// gamepads is the driver's memory of the gamepads, so a frame's poll turns into
// the changes since the last one. Main-thread-only, like pending.
type gamepads struct {
	// last is every gamepad the previous poll reported, by slot.
	last map[gamepad.ID]gamepad.Pad
	// polled is the poll's buffer, kept between frames so polling allocates
	// nothing once warm.
	polled []gamepad.Pad
	// window is the native window the platform layer listens to for hot-plug,
	// found once; 0 until then.
	window uintptr
	// failed stops polling for good once the platform layer has failed.
	failed bool
}

// pollGamepads reads the gamepads and appends what changed since the last
// frame to the pending batch: connections first, then each pad's button and
// axis changes, then the releases and disconnections of pads that left. A pad
// that leaves releases its buttons one change at a time before it disconnects,
// so every release is published as an event. Called at the start of onUpdate,
// on the main thread, which is where the platform layer must be polled.
func (p *plugin) pollGamepads(k kernel.Executioner) {
	g := &p.gamepads
	if g.failed {
		return
	}
	if g.window == 0 {
		g.window = findGamepadWindow()
	}
	polled, err := gamepad.Poll(g.window, g.polled[:0])
	g.polled = polled
	if err != nil {
		g.failed = true
		k.ReportErrorOnce(gamepadFailureKey{}, ErrGamepads{Err: err})
		return
	}
	if g.last == nil {
		g.last = map[gamepad.ID]gamepad.Pad{}
	}

	for _, now := range polled {
		slot := input.Gamepad(now.ID)
		before, known := g.last[now.ID]
		if !known {
			p.pending = append(p.pending, input.GamepadConnectChange(slot, now.Name))
		}
		for b := range gamepad.ButtonCount {
			if now.Buttons[b] != before.Buttons[b] {
				p.pending = append(p.pending, input.GamepadButtonChange(slot, input.GamepadButton(b), now.Buttons[b]))
			}
		}
		for a := range gamepad.AxisCount {
			if now.Axes[a] != before.Axes[a] {
				p.pending = append(p.pending, input.GamepadAxisChange(slot, input.GamepadAxis(a), now.Axes[a]))
			}
		}
		g.last[now.ID] = now
	}

	for id, before := range g.last {
		if gamepadPolled(polled, id) {
			continue
		}
		slot := input.Gamepad(id)
		for b := range gamepad.ButtonCount {
			if before.Buttons[b] {
				p.pending = append(p.pending, input.GamepadButtonChange(slot, input.GamepadButton(b), false))
			}
		}
		p.pending = append(p.pending, input.GamepadDisconnectChange(slot))
		delete(g.last, id)
	}
}

// gamepadPolled reports whether the poll reported the gamepad in slot id.
func gamepadPolled(polled []gamepad.Pad, id gamepad.ID) bool {
	for _, pad := range polled {
		if pad.ID == id {
			return true
		}
	}
	return false
}

// The gamepad library lays its buttons and axes out in the order input does,
// the W3C standard layout's, so a cast maps one onto the other. These fail to
// compile if either side reorders.
var (
	_ = [1]struct{}{}[int(input.GamepadButtonGuide)-int(gamepad.ButtonGuide)]
	_ = [1]struct{}{}[int(input.GamepadButtonDpadUp)-int(gamepad.ButtonDpadUp)]
	_ = [1]struct{}{}[int(input.GamepadButtonLeftTrigger)-int(gamepad.ButtonLeftTrigger)]
	_ = [1]struct{}{}[int(input.GamepadAxisRightTrigger)-int(gamepad.AxisRightTrigger)]
	_ = [1]struct{}{}[int(input.GamepadAxisLeftTrigger)-int(gamepad.AxisLeftTrigger)]
)
