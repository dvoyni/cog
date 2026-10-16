package internal

import (
	"fmt"
	"slices"
)

// Gamepad identifies a connected gamepad by its slot. A slot stays the same
// while its gamepad is connected, and a slot freed by a disconnection is the
// first one a later connection takes, so a single player's pad is 0.
type Gamepad int

// AnyGamepad, passed where a query takes a Gamepad, asks about every connected
// gamepad at once. It is how a game with one player reads "the pad" without
// caring which slot it landed in.
const AnyGamepad Gamepad = -1

// GamepadButton is a button of the standard layout: the W3C Gamepad remapping,
// which is SDL's game controller layout under other names. Faces are named by
// position, not by label, because labels differ between pad families: South is
// A on an Xbox pad, Cross on a PlayStation pad and B on a Nintendo pad.
//
// The order is the W3C standard layout's, so a driver over a library that
// follows it maps with a plain cast.
type GamepadButton uint8

const (
	GamepadButtonSouth GamepadButton = iota
	GamepadButtonEast
	GamepadButtonWest
	GamepadButtonNorth
	GamepadButtonLeftShoulder
	GamepadButtonRightShoulder
	GamepadButtonLeftTrigger
	GamepadButtonRightTrigger
	GamepadButtonBack
	GamepadButtonStart
	GamepadButtonLeftStick
	GamepadButtonRightStick
	GamepadButtonDpadUp
	GamepadButtonDpadDown
	GamepadButtonDpadLeft
	GamepadButtonDpadRight
	GamepadButtonGuide
)

// gamepadButtonCount is the number of standard buttons.
const gamepadButtonCount = int(GamepadButtonGuide) + 1

// GamepadAxis is an analog input of the standard layout. Sticks run from -1 to
// 1, with left and up negative; triggers run from 0, released, to 1, fully
// pulled. A trigger is also a GamepadButton, pressed past the platform's or the
// mapping's threshold.
type GamepadAxis uint8

const (
	GamepadAxisLeftX GamepadAxis = iota
	GamepadAxisLeftY
	GamepadAxisRightX
	GamepadAxisRightY
	GamepadAxisLeftTrigger
	GamepadAxisRightTrigger
)

// gamepadAxisCount is the number of standard axes.
const gamepadAxisCount = int(GamepadAxisRightTrigger) + 1

// GamepadType is the family a gamepad belongs to, which decides the labels on
// its buttons, so a game can draw the glyphs the player sees on the pad. The
// driver tells it at connection, from what it knows of the device; Unknown is
// a pad it could not place.
type GamepadType uint8

const (
	GamepadTypeUnknown GamepadType = iota
	GamepadTypeXbox
	GamepadTypePlayStation
	GamepadTypeSwitch
	GamepadTypeSteamDeck
)

// gamepadTypeCount is the number of gamepad types.
const gamepadTypeCount = int(GamepadTypeSteamDeck) + 1

var gamepadButtonNames = [gamepadButtonCount]string{
	"south", "east", "west", "north",
	"left_shoulder", "right_shoulder", "left_trigger", "right_trigger",
	"back", "start", "left_stick", "right_stick",
	"dpad_up", "dpad_down", "dpad_left", "dpad_right",
	"guide",
}

var gamepadAxisNames = [gamepadAxisCount]string{
	"left_x", "left_y", "right_x", "right_y", "left_trigger", "right_trigger",
}

var gamepadTypeNames = [gamepadTypeCount]string{
	"unknown", "xbox", "playstation", "switch", "steam_deck",
}

// String reports the button's name, such as "south" or "dpad_up".
func (b GamepadButton) String() string {
	if int(b) < gamepadButtonCount {
		return gamepadButtonNames[b]
	}
	return fmt.Sprintf("#%d", int(b))
}

// MarshalText writes the button's name, so a GamepadButton is a JSON string.
func (b GamepadButton) MarshalText() ([]byte, error) { return []byte(b.String()), nil }

// UnmarshalText reads a button name.
func (b *GamepadButton) UnmarshalText(text []byte) error {
	i := slices.Index(gamepadButtonNames[:], string(text))
	if i < 0 {
		return ErrUnknownGamepadInput{Name: string(text)}
	}
	*b = GamepadButton(i)
	return nil
}

// TextValues reports the button names, so a tool schema lists them.
func (GamepadButton) TextValues() (values []string, other string) {
	return slices.Clone(gamepadButtonNames[:]), ""
}

// String reports the axis's name, such as "left_x" or "right_trigger".
func (a GamepadAxis) String() string {
	if int(a) < gamepadAxisCount {
		return gamepadAxisNames[a]
	}
	return fmt.Sprintf("#%d", int(a))
}

// MarshalText writes the axis's name, so a GamepadAxis is a JSON string.
func (a GamepadAxis) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

// UnmarshalText reads an axis name.
func (a *GamepadAxis) UnmarshalText(text []byte) error {
	i := slices.Index(gamepadAxisNames[:], string(text))
	if i < 0 {
		return ErrUnknownGamepadInput{Name: string(text)}
	}
	*a = GamepadAxis(i)
	return nil
}

// TextValues reports the axis names, so a tool schema lists them.
func (GamepadAxis) TextValues() (values []string, other string) {
	return slices.Clone(gamepadAxisNames[:]), ""
}

// String reports the type's name, such as "xbox" or "steam_deck".
func (t GamepadType) String() string {
	if int(t) < gamepadTypeCount {
		return gamepadTypeNames[t]
	}
	return fmt.Sprintf("#%d", int(t))
}

// MarshalText writes the type's name, so a GamepadType is a JSON string.
func (t GamepadType) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// UnmarshalText reads a type name.
func (t *GamepadType) UnmarshalText(text []byte) error {
	i := slices.Index(gamepadTypeNames[:], string(text))
	if i < 0 {
		return ErrUnknownGamepadInput{Name: string(text)}
	}
	*t = GamepadType(i)
	return nil
}

// TextValues reports the type names, so a tool schema lists them.
func (GamepadType) TextValues() (values []string, other string) {
	return slices.Clone(gamepadTypeNames[:]), ""
}

// padState is what State knows about one connected gamepad.
type padState struct {
	name string
	kind GamepadType
	down [gamepadButtonCount]bool
	axes [gamepadAxisCount]float64
}

// padButton is one button of one gamepad: the key of the per-tick edge sets.
type padButton struct {
	pad    Gamepad
	button GamepadButton
}

// GamepadSnapshot is one connected gamepad in the picture of the seam: its
// name, the buttons held and the axes away from rest.
type GamepadSnapshot struct {
	Gamepad Gamepad     `json:"gamepad" jsonschema:"the gamepad's slot"`
	Name    string      `json:"name" jsonschema:"what the device calls itself"`
	Type    GamepadType `json:"type" jsonschema:"the family the gamepad belongs to, which decides its button labels"`
	// Down is every button held, in layout order. Always non-nil.
	Down []GamepadButton `json:"down" jsonschema:"every button held"`
	// Axes is every axis not at rest, in layout order. Always non-nil.
	Axes []GamepadAxisValue `json:"axes" jsonschema:"every axis not at rest"`
}

// GamepadAxisValue is one axis and where it is.
type GamepadAxisValue struct {
	Axis  GamepadAxis `json:"axis"`
	Value float64     `json:"value"`
}
