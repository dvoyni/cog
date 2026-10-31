package internal

// ActionKind tags the variant of an Action. The ten kinds are the whole
// vocabulary of a synthetic input sequence: there is no compound click and no
// compound press, because two spellings of a click would make a caller choose
// between them.
type ActionKind string

const (
	// ActionKeyDown presses Key. Pressing a key already down is a no-op.
	ActionKeyDown ActionKind = "key_down"
	// ActionKeyUp releases Key. Releasing a key that is not down is a silent
	// no-op, exactly as the state itself already treats it.
	ActionKeyUp ActionKind = "key_up"
	// ActionMove puts the pointer at X, Y in window units.
	ActionMove ActionKind = "move"
	// ActionMoveBy moves the pointer by Dx, Dy from wherever it is. The
	// resolution happens under the write lock, which is the one thing the
	// caller's own arithmetic over a returned position cannot do.
	ActionMoveBy ActionKind = "move_by"
	// ActionScroll reports a scroll delta of Dx, Dy. cog has no unit to
	// promise here: the value is whatever a driver would have passed through.
	ActionScroll ActionKind = "scroll"
	// ActionText enters Text one rune at a time. It emits text changes only
	// and presses no keys, because a faithful rendering is a keyboard-layout
	// problem with no consumer in cog; a game that reads keys wants
	// ActionKeyDown and ActionKeyUp.
	ActionText ActionKind = "text"
	// ActionDelay waits Ms milliseconds of wall clock. It is the only step
	// that splits a sequence: everything between two delays lands in one tick.
	ActionDelay ActionKind = "delay"
	// ActionGamepadDown presses Button on the gamepad in slot Gamepad. A slot
	// with no gamepad gets a synthetic one first, connected like a driver's.
	ActionGamepadDown ActionKind = "gamepad_down"
	// ActionGamepadUp releases Button on the gamepad in slot Gamepad.
	ActionGamepadUp ActionKind = "gamepad_up"
	// ActionGamepadAxis moves Axis on the gamepad in slot Gamepad to Value.
	// An axis stays where it was put until something moves it back to 0.
	ActionGamepadAxis ActionKind = "gamepad_axis"
)

// actionKinds is the set of steps that exist. It is what makes an unknown
// step kind a refusal before the sequence runs rather than a step silently
// skipped halfway through one.
var actionKinds = map[ActionKind]bool{
	ActionKeyDown: true,
	ActionKeyUp:   true,
	ActionMove:    true,
	ActionMoveBy:  true,
	ActionScroll:  true,
	ActionText:    true,
	ActionDelay:   true,

	ActionGamepadDown: true,
	ActionGamepadUp:   true,
	ActionGamepadAxis: true,
}

// Action is one step of a synthetic input sequence.
//
// Its fields are exported, unlike Change's — a deliberate divergence inside one
// package. A Change is a driver's internal delta, constructed and never
// inspected; an Action is a script, and the thing writing it is usually outside
// the process. The omitempty fields are the tagged union flattened, which keeps
// the JSON schema one object rather than a oneOf.
type Action struct {
	// Do selects the step kind. An unknown kind refuses the whole sequence.
	Do ActionKind `json:"do" jsonschema:"one of key_down, key_up, move, move_by, scroll, text, delay, gamepad_down, gamepad_up or gamepad_axis"`
	// X and Y are the absolute pointer position for move, in window units.
	X float64 `json:"x,omitempty" jsonschema:"move: pointer x in window units"`
	Y float64 `json:"y,omitempty" jsonschema:"move: pointer y in window units"`
	// Dx and Dy are the delta for move_by and scroll.
	Dx float64 `json:"dx,omitempty" jsonschema:"move_by or scroll: delta along x"`
	Dy float64 `json:"dy,omitempty" jsonschema:"move_by or scroll: delta along y"`
	// Key is the key or mouse button for key_down and key_up.
	Key Key `json:"key,omitempty" jsonschema:"A key name such as w, escape or mouse_left. #<number> also works and is how an unnamed key is reported back, but the number is cog's own key code and is not ASCII or a browser keyCode — #13 is the letter M, and Enter is enter (#84). Use names."`
	// Text is what text enters, one rune per change.
	Text string `json:"text,omitempty" jsonschema:"text: the characters to enter"`
	// Ms is how long delay waits, in milliseconds of wall clock.
	Ms int `json:"ms,omitempty" jsonschema:"delay: milliseconds to wait before the next steps"`
	// Gamepad is the slot gamepad_down, gamepad_up and gamepad_axis act on.
	Gamepad Gamepad `json:"gamepad,omitempty" jsonschema:"gamepad_down, gamepad_up or gamepad_axis: the gamepad's slot, 0 for the first"`
	// Button is the standard-layout button for gamepad_down and gamepad_up.
	Button GamepadButton `json:"button,omitempty" jsonschema:"gamepad_down or gamepad_up: a standard-layout button, named by position: south is A on an Xbox pad"`
	// Axis and Value are the axis gamepad_axis moves and where to.
	Axis  GamepadAxis `json:"axis,omitempty" jsonschema:"gamepad_axis: the axis to move"`
	Value float64     `json:"value,omitempty" jsonschema:"gamepad_axis: -1 to 1 for a stick, left and up negative; 0 to 1 for a trigger"`
}
