package internal

import "slices"

// State is the polled input state. It is registered as *State; gameplay reads
// it under a read lock and queries it. The "just pressed/released", scroll, and
// text values are per-tick: a driver's Apply folds changes into pending
// accumulators, and the tick-boundary subscription promotes them to the current
// tick's view via advance. The pressed set and pointer are live (always current).
type State struct {
	down map[Key]struct{}

	// current tick view (read by gameplay)
	justPressed  map[Key]struct{}
	justReleased map[Key]struct{}
	scrollDx     float64
	scrollDy     float64
	text         []rune
	paste        string
	pasted       bool

	// pending accumulation since the last tick (written by Apply)
	pendPressed  map[Key]struct{}
	pendReleased map[Key]struct{}
	pendScrollDx float64
	pendScrollDy float64
	pendText     []rune
	pendPaste    string
	pendPasted   bool

	pointer Pos

	// gamepads is every connected gamepad, and gamepadOrder their slots
	// sorted: the slice Gamepads hands out, rebuilt on connect and disconnect
	// so reading it allocates nothing.
	gamepads     map[Gamepad]*padState
	gamepadOrder []Gamepad

	// per-tick gamepad button edges, current and pending, as for keys
	padPressed      map[padButton]struct{}
	padReleased     map[padButton]struct{}
	pendPadPressed  map[padButton]struct{}
	pendPadReleased map[padButton]struct{}
}

func NewState() *State {
	return &State{
		down:         map[Key]struct{}{},
		justPressed:  map[Key]struct{}{},
		justReleased: map[Key]struct{}{},
		pendPressed:  map[Key]struct{}{},
		pendReleased: map[Key]struct{}{},

		gamepads:        map[Gamepad]*padState{},
		padPressed:      map[padButton]struct{}{},
		padReleased:     map[padButton]struct{}{},
		pendPadPressed:  map[padButton]struct{}{},
		pendPadReleased: map[padButton]struct{}{},
	}
}

// apply folds one change into the live state and the pending per-tick accumulators.
func (s *State) apply(c Change) {
	switch c.kind {
	case ChangeKindKey:
		if c.down {
			if _, ok := s.down[c.key]; !ok {
				s.down[c.key] = struct{}{}
				s.pendPressed[c.key] = struct{}{}
			}
		} else {
			if _, ok := s.down[c.key]; ok {
				delete(s.down, c.key)
				s.pendReleased[c.key] = struct{}{}
			}
		}
	case ChangeKindPointer:
		s.pointer = c.pos
	case ChangeKindScroll:
		s.pendScrollDx += c.dx
		s.pendScrollDy += c.dy
	case ChangeKindText:
		s.pendText = append(s.pendText, c.r)
	case ChangeKindClipboardPaste:
		// Two pastes in one tick keep the later: a paste is the clipboard's
		// content, and the later one is what the clipboard holds.
		s.pendPaste, s.pendPasted = c.text, true
	case ChangeKindGamepadConnect:
		s.connectGamepad(c.pad, c.text, c.padType)
	case ChangeKindGamepadDisconnect:
		s.disconnectGamepad(c.pad)
	case ChangeKindGamepadButton:
		s.setGamepadButton(c.pad, c.button, c.down)
	case ChangeKindGamepadAxis:
		if pad := s.gamepads[c.pad]; pad != nil && int(c.axis) < gamepadAxisCount {
			pad.axes[c.axis] = c.value
		}
	}
}

// connectGamepad adds a gamepad, or renames and retypes one already in the
// slot.
func (s *State) connectGamepad(id Gamepad, name string, padType GamepadType) {
	if pad := s.gamepads[id]; pad != nil {
		pad.name, pad.kind = name, padType
		return
	}
	s.gamepads[id] = &padState{name: name, kind: padType}
	at, _ := slices.BinarySearch(s.gamepadOrder, id)
	s.gamepadOrder = slices.Insert(s.gamepadOrder, at, id)
}

// disconnectGamepad releases what the gamepad held, with edges, and forgets
// it.
func (s *State) disconnectGamepad(id Gamepad) {
	pad := s.gamepads[id]
	if pad == nil {
		return
	}
	for b := range gamepadButtonCount {
		s.setGamepadButton(id, GamepadButton(b), false)
	}
	delete(s.gamepads, id)
	if at, found := slices.BinarySearch(s.gamepadOrder, id); found {
		s.gamepadOrder = slices.Delete(s.gamepadOrder, at, at+1)
	}
}

// setGamepadButton folds one button edge of a connected gamepad. Like a key,
// pressing a held button or releasing a released one is a no-op.
func (s *State) setGamepadButton(id Gamepad, button GamepadButton, down bool) {
	pad := s.gamepads[id]
	if pad == nil || int(button) >= gamepadButtonCount || pad.down[button] == down {
		return
	}
	pad.down[button] = down
	if down {
		s.pendPadPressed[padButton{id, button}] = struct{}{}
	} else {
		s.pendPadReleased[padButton{id, button}] = struct{}{}
	}
}

// advance promotes the pending accumulators to the current-tick view and resets
// them. Called once per tick, before gameplay reads the state.
func (s *State) advance() {
	s.justPressed = s.pendPressed
	s.justReleased = s.pendReleased
	s.scrollDx, s.scrollDy = s.pendScrollDx, s.pendScrollDy
	s.text = s.pendText
	s.paste, s.pasted = s.pendPaste, s.pendPasted
	s.padPressed, s.padReleased = s.pendPadPressed, s.pendPadReleased

	s.pendPressed = map[Key]struct{}{}
	s.pendReleased = map[Key]struct{}{}
	s.pendScrollDx, s.pendScrollDy = 0, 0
	s.pendText = nil
	s.pendPaste, s.pendPasted = "", false
	s.pendPadPressed = map[padButton]struct{}{}
	s.pendPadReleased = map[padButton]struct{}{}
}

// Pressed reports whether k is currently held down.
func (s *State) Pressed(k Key) bool { _, ok := s.down[k]; return ok }

// JustPressed reports whether k transitioned to down during the current tick.
func (s *State) JustPressed(k Key) bool { _, ok := s.justPressed[k]; return ok }

// JustReleased reports whether k transitioned to up during the current tick.
func (s *State) JustReleased(k Key) bool { _, ok := s.justReleased[k]; return ok }

// Pointer returns the current pointer position (live).
func (s *State) Pointer() Pos { return s.pointer }

// Scroll returns the scroll delta accumulated for the current tick.
func (s *State) Scroll() (dx, dy float64) { return s.scrollDx, s.scrollDy }

// Text returns the runes typed during the current tick.
func (s *State) Text() []rune { return s.text }

// ClipboardPaste returns the text pasted from the clipboard during the current
// tick, and whether anything was.
func (s *State) ClipboardPaste() (string, bool) { return s.paste, s.pasted }

// Gamepads returns the slots of the connected gamepads, in order. The slice is
// the State's own: read it under the lock and do not keep or change it.
func (s *State) Gamepads() []Gamepad { return s.gamepadOrder }

// GamepadConnected reports whether a gamepad is connected in slot pad, or, for
// AnyGamepad, whether any is.
func (s *State) GamepadConnected(pad Gamepad) bool {
	if pad == AnyGamepad {
		return len(s.gamepadOrder) > 0
	}
	return s.gamepads[pad] != nil
}

// GamepadName returns what the gamepad in slot pad calls itself, such as
// "Xbox Controller", or "" when none is connected there.
func (s *State) GamepadName(pad Gamepad) string {
	if p := s.gamepads[pad]; p != nil {
		return p.name
	}
	return ""
}

// GamepadType returns the family of the gamepad in slot pad, for a game that
// draws button glyphs, or GamepadTypeUnknown when none is connected there or
// the driver could not place it.
func (s *State) GamepadType(pad Gamepad) GamepadType {
	if p := s.gamepads[pad]; p != nil {
		return p.kind
	}
	return GamepadTypeUnknown
}

// GamepadPressed reports whether button is held on the gamepad in slot pad, or,
// for AnyGamepad, on any connected gamepad.
func (s *State) GamepadPressed(pad Gamepad, button GamepadButton) bool {
	if int(button) >= gamepadButtonCount {
		return false
	}
	if pad != AnyGamepad {
		p := s.gamepads[pad]
		return p != nil && p.down[button]
	}
	for _, p := range s.gamepads {
		if p.down[button] {
			return true
		}
	}
	return false
}

// GamepadJustPressed reports whether button went down during the current tick
// on the gamepad in slot pad, or, for AnyGamepad, on any gamepad.
func (s *State) GamepadJustPressed(pad Gamepad, button GamepadButton) bool {
	return hasPadButton(s.padPressed, pad, button)
}

// GamepadJustReleased reports whether button went up during the current tick
// on the gamepad in slot pad, or, for AnyGamepad, on any gamepad. A gamepad
// disconnecting releases what it held, and those releases count.
func (s *State) GamepadJustReleased(pad Gamepad, button GamepadButton) bool {
	return hasPadButton(s.padReleased, pad, button)
}

// GamepadAxis returns where axis is on the gamepad in slot pad, 0 when none is
// connected there. For AnyGamepad it is the value furthest from rest across
// the connected gamepads. The value is raw: a stick at rest rarely reads
// exactly 0, and the dead zone is the game's to choose.
func (s *State) GamepadAxis(pad Gamepad, axis GamepadAxis) float64 {
	if int(axis) >= gamepadAxisCount {
		return 0
	}
	if pad != AnyGamepad {
		if p := s.gamepads[pad]; p != nil {
			return p.axes[axis]
		}
		return 0
	}
	var furthest float64
	for _, p := range s.gamepads {
		if v := p.axes[axis]; abs(v) > abs(furthest) {
			furthest = v
		}
	}
	return furthest
}

func hasPadButton(set map[padButton]struct{}, pad Gamepad, button GamepadButton) bool {
	if pad != AnyGamepad {
		_, ok := set[padButton{pad, button}]
		return ok
	}
	for edge := range set {
		if edge.button == button {
			return true
		}
	}
	return false
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// gamepadSnapshots is the gamepad half of the picture of the seam: each
// connected gamepad, in slot order, with what it holds. Always non-nil.
func (s *State) gamepadSnapshots() []GamepadSnapshot {
	snapshots := make([]GamepadSnapshot, 0, len(s.gamepadOrder))
	for _, id := range s.gamepadOrder {
		pad := s.gamepads[id]
		snapshot := GamepadSnapshot{
			Gamepad: id,
			Name:    pad.name,
			Type:    pad.kind,
			Down:    []GamepadButton{},
			Axes:    []GamepadAxisValue{},
		}
		for b, down := range pad.down {
			if down {
				snapshot.Down = append(snapshot.Down, GamepadButton(b))
			}
		}
		for a, v := range pad.axes {
			if v != 0 {
				snapshot.Axes = append(snapshot.Axes, GamepadAxisValue{Axis: GamepadAxis(a), Value: v})
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

// held is the down-set of the picture of the seam every input capability
// answers with (input.StateResponse), which pairs it with the live pointer:
// the live down-set, sorted because map iteration is not. The slice is always
// non-nil, so "nothing is held" reads as an empty list rather than as a missing
// answer.
func (s *State) held() []Key {
	down := make([]Key, 0, len(s.down))
	for key := range s.down {
		down = append(down, key)
	}
	slices.Sort(down)
	return down
}

// modifiers derives the modifier bitmask from the live down-set. A driver
// carries Mods from the platform and the state does not track them, so this is
// the only place a chord assembled from separate presses can report the
// modifier that is actually held instead of zero.
//
// ModCapsLock and ModNumLock are never derived: they report a lock being
// active, not a key being held, and nothing in the state knows which locks are
// on. Reporting them from a held key would be a lie in the one direction that
// matters — a caller comparing Pressed against Mods.
func (s *State) modifiers() Mods {
	var mods Mods
	for key, mod := range modifierKeys {
		if _, held := s.down[key]; held {
			mods |= mod
		}
	}
	return mods
}

// modifierKeys maps each modifier key onto the bit it contributes. Left and
// right contribute the same bit, as they do on every platform cog maps from.
var modifierKeys = map[Key]Mods{
	KeyLeftShift:    ModShift,
	KeyRightShift:   ModShift,
	KeyLeftControl:  ModCtrl,
	KeyRightControl: ModCtrl,
	KeyLeftAlt:      ModAlt,
	KeyRightAlt:     ModAlt,
	KeyLeftSuper:    ModSuper,
	KeyRightSuper:   ModSuper,
}
