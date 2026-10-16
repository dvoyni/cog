package internal

import (
	"encoding/json"
	"slices"
	"testing"
)

// A gamepad button has the same edges as a key: held at once, just pressed for
// the tick after advance, just released for the tick after its release.
func TestStateGamepadButtonEdges(t *testing.T) {
	s := NewState()
	s.apply(GamepadConnectChange(0, "pad"))

	s.apply(GamepadButtonChange(0, GamepadButtonSouth, true))
	if !s.GamepadPressed(0, GamepadButtonSouth) {
		t.Fatal("GamepadPressed should be true immediately")
	}
	if s.GamepadJustPressed(0, GamepadButtonSouth) {
		t.Fatal("GamepadJustPressed should be false before advance")
	}
	s.advance()
	if !s.GamepadJustPressed(0, GamepadButtonSouth) {
		t.Fatal("GamepadJustPressed should be true this tick")
	}
	s.advance()
	if s.GamepadJustPressed(0, GamepadButtonSouth) || !s.GamepadPressed(0, GamepadButtonSouth) {
		t.Fatal("the edge should clear next tick while the button stays held")
	}

	s.apply(GamepadButtonChange(0, GamepadButtonSouth, false))
	s.advance()
	if !s.GamepadJustReleased(0, GamepadButtonSouth) || s.GamepadPressed(0, GamepadButtonSouth) {
		t.Fatal("GamepadJustReleased should be true this tick and the button up")
	}
}

// AnyGamepad answers for every connected pad; a slot answers only for itself.
func TestStateAnyGamepad(t *testing.T) {
	s := NewState()
	s.apply(GamepadConnectChange(0, "first"))
	s.apply(GamepadConnectChange(1, "second"))
	s.apply(GamepadButtonChange(1, GamepadButtonEast, true))
	s.apply(GamepadAxisChange(0, GamepadAxisLeftX, 0.25))
	s.apply(GamepadAxisChange(1, GamepadAxisLeftX, -0.75))
	s.advance()

	if !s.GamepadPressed(AnyGamepad, GamepadButtonEast) || !s.GamepadJustPressed(AnyGamepad, GamepadButtonEast) {
		t.Error("AnyGamepad should see the second pad's press")
	}
	if s.GamepadPressed(0, GamepadButtonEast) || s.GamepadJustPressed(0, GamepadButtonEast) {
		t.Error("slot 0 should not see slot 1's press")
	}
	if v := s.GamepadAxis(AnyGamepad, GamepadAxisLeftX); v != -0.75 {
		t.Errorf("AnyGamepad axis = %v, want the value furthest from rest, -0.75", v)
	}
	if v := s.GamepadAxis(0, GamepadAxisLeftX); v != 0.25 {
		t.Errorf("slot 0 axis = %v, want 0.25", v)
	}
	if got := s.Gamepads(); !slices.Equal(got, []Gamepad{0, 1}) {
		t.Errorf("Gamepads = %v, want [0 1]", got)
	}
}

// A disconnection releases what the pad held, as edges, and forgets the pad.
func TestStateGamepadDisconnectReleases(t *testing.T) {
	s := NewState()
	s.apply(GamepadConnectChange(0, "pad"))
	s.apply(GamepadButtonChange(0, GamepadButtonNorth, true))
	s.apply(GamepadAxisChange(0, GamepadAxisRightY, 1))
	s.advance()

	s.apply(GamepadDisconnectChange(0))
	s.advance()
	if !s.GamepadJustReleased(0, GamepadButtonNorth) {
		t.Error("the held button should be just released")
	}
	if s.GamepadPressed(AnyGamepad, GamepadButtonNorth) || s.GamepadConnected(0) ||
		s.GamepadAxis(0, GamepadAxisRightY) != 0 || len(s.Gamepads()) != 0 {
		t.Error("the pad should be gone with nothing held")
	}
}

// Changes for a slot with no gamepad are ignored rather than connecting one:
// connecting is the driver's statement, not a side effect of a press.
func TestStateGamepadIgnoresUnconnectedSlot(t *testing.T) {
	s := NewState()
	s.apply(GamepadButtonChange(2, GamepadButtonSouth, true))
	s.apply(GamepadAxisChange(2, GamepadAxisLeftX, 1))
	s.advance()
	if s.GamepadConnected(AnyGamepad) || s.GamepadPressed(AnyGamepad, GamepadButtonSouth) ||
		s.GamepadJustPressed(AnyGamepad, GamepadButtonSouth) {
		t.Error("an unconnected slot should change nothing")
	}
}

// Buttons and axes cross the wire as names, and unknown names are refused.
func TestGamepadNamesCrossAsJSONStrings(t *testing.T) {
	encoded, err := json.Marshal(Action{Do: ActionGamepadAxis, Gamepad: 1, Axis: GamepadAxisRightTrigger, Value: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"do":"gamepad_axis","gamepad":1,"axis":"right_trigger","value":0.5}` {
		t.Errorf("encoded as %s", encoded)
	}

	var step Action
	if err := json.Unmarshal([]byte(`{"do":"gamepad_down","button":"dpad_left"}`), &step); err != nil {
		t.Fatal(err)
	}
	if step.Button != GamepadButtonDpadLeft || step.Gamepad != 0 {
		t.Errorf("decoded %+v, want dpad_left on slot 0", step)
	}
	if err := json.Unmarshal([]byte(`{"do":"gamepad_down","button":"a"}`), &step); err == nil {
		t.Error("a label rather than a position was accepted as a button")
	}
}

// A scripted press on an empty slot connects a synthetic pad first, so the
// press lands and the seam reports the pad holding it.
func TestPlay_AGamepadStepConnectsASyntheticPad(t *testing.T) {
	h := newPlayHarness(t)
	seam, err := Play(h.k, []Action{
		{Do: ActionGamepadDown, Button: GamepadButtonSouth},
		{Do: ActionGamepadAxis, Axis: GamepadAxisLeftY, Value: -1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seam.Gamepads) != 1 {
		t.Fatalf("seam gamepads = %+v, want one", seam.Gamepads)
	}
	pad := seam.Gamepads[0]
	if pad.Gamepad != 0 || pad.Name != syntheticGamepadName ||
		!slices.Equal(pad.Down, []GamepadButton{GamepadButtonSouth}) ||
		!slices.Equal(pad.Axes, []GamepadAxisValue{{Axis: GamepadAxisLeftY, Value: -1}}) {
		t.Errorf("seam gamepad = %+v", pad)
	}
}
