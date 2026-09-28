package types

import "strconv"

// FrontFace selects the winding that counts as the front face. glTF requires
// the reversed winding on nodes whose transform has a negative determinant.
type FrontFace uint8

const (
	FrontCCW FrontFace = iota
	FrontCW
)

// String spells the winding for a debug document.
func (face FrontFace) String() string {
	switch face {
	case FrontCCW:
		return "ccw"
	case FrontCW:
		return "cw"
	}
	return "unknown(" + strconv.Itoa(int(face)) + ")"
}
