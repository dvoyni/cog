package internal

// KeyEvent is published for each applied key change.
type KeyEvent struct {
	Key  Key
	Mods Mods
	Down bool
}

// PointerEvent is published for each applied pointer change.
type PointerEvent struct{ Pos Pos }

// ScrollEvent is published for each applied scroll change.
type ScrollEvent struct{ Dx, Dy float64 }

// TextEvent is published for each applied text change.
type TextEvent struct{ Rune rune }

// ClipboardPasteEvent is published for each applied clipboard paste.
type ClipboardPasteEvent struct{ Text string }

// GamepadConnectionEvent is published for each gamepad connecting or
// disconnecting. Name is what the gamepad calls itself; it is set on a
// disconnection too, when the change carried it, and is otherwise "".
type GamepadConnectionEvent struct {
	Gamepad   Gamepad
	Name      string
	Connected bool
}

// GamepadButtonEvent is published for each applied gamepad button change.
// Axes publish no event: a stick moves every frame it is held, and a game reads
// it by polling State.
type GamepadButtonEvent struct {
	Gamepad Gamepad
	Button  GamepadButton
	Down    bool
}
