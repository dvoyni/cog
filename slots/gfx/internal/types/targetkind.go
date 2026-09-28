package types

import "strconv"

// TargetKind says what a TargetDescr renders into.
type TargetKind uint8

// The screen is the zero value: a recorder that defaults a pass leaves Target
// alone, and the screen is what it means by that. A colourless pass has to say
// so explicitly, because "no colour attachment" is a deliberate choice a
// depth-only prepass makes, never an omission.
const (
	TargetScreen TargetKind = iota
	TargetNone
	TargetTexture
)

// String spells the target kind for a debug document.
func (kind TargetKind) String() string {
	switch kind {
	case TargetScreen:
		return "screen"
	case TargetNone:
		return "none"
	case TargetTexture:
		return "texture"
	}
	return "unknown(" + strconv.Itoa(int(kind)) + ")"
}
