package internal

import "fmt"

// ErrUnknownKey reports a key name that is neither in the name table nor the
// "#<n>" printed form of an unnamed key. It carries the whole hint, because the
// place it usually surfaces is a JSON decode whose own message says only that a
// string did not fit.
type ErrUnknownKey struct{ Name string }

func (e ErrUnknownKey) Error() string {
	return fmt.Sprintf("input: %q is not a key; use a name such as w, escape or mouse_left, or "+
		"#<n> with cog's own key code — which is not ASCII and not a browser keyCode", e.Name)
}

// ErrUnknownGamepadInput reports a name that is neither a gamepad button's nor
// a gamepad axis's.
type ErrUnknownGamepadInput struct{ Name string }

func (e ErrUnknownGamepadInput) Error() string {
	return fmt.Sprintf("input: %q is not a gamepad button or axis; buttons are south, east, west, "+
		"north, left_shoulder, right_shoulder, left_trigger, right_trigger, back, start, guide, "+
		"left_stick, right_stick and dpad_up/down/left/right; axes are left_x, left_y, right_x, "+
		"right_y, left_trigger and right_trigger", e.Name)
}
