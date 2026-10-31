package types

import "strconv"

// DepthKind says what a DepthDescr renders depth into.
type DepthKind uint8

// DepthDescrAuto is the zero value for the same reason the screen is: a 3D pass
// wants depth, and forgetting to ask for it renders the scene inside out.
const (
	DepthKindAuto DepthKind = iota
	DepthKindNone
	DepthKindTexture
)

// String spells the depth kind for a debug document.
func (kind DepthKind) String() string {
	switch kind {
	case DepthKindAuto:
		return "auto"
	case DepthKindNone:
		return "none"
	case DepthKindTexture:
		return "texture"
	}
	return "unknown(" + strconv.Itoa(int(kind)) + ")"
}
