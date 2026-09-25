package types

import "strconv"

// BlendMode selects color blending against the render target.
type BlendMode uint8

const (
	// BlendAlpha is straight-alpha over blending.
	BlendAlpha BlendMode = iota
	// BlendOpaque overwrites the target (no blend).
	BlendOpaque
	// BlendAdditive adds source color weighted by source alpha.
	BlendAdditive
	// BlendMultiply multiplies source and destination color.
	BlendMultiply
)

// Name spells the blend mode for a debug document.
func (mode BlendMode) String() string {
	switch mode {
	case BlendAlpha:
		return "alpha"
	case BlendOpaque:
		return "opaque"
	case BlendAdditive:
		return "additive"
	case BlendMultiply:
		return "multiply"
	}
	return "unknown(" + strconv.Itoa(int(mode)) + ")"
}

// CompareFunc is a depth or sampler comparison. The zero value passes
// everything, which is the WebGPU default and what a draw that ignores depth
// wants. Depth is conventional: near maps to 0, far to 1, so CompareLess keeps
// the nearer fragment.
type CompareFunc uint8

const (
	CompareAlways CompareFunc = iota
	CompareNever
	CompareLess
	CompareLessEqual
	CompareGreater
	CompareGreaterEqual
	CompareEqual
	CompareNotEqual
)

// Name spells the comparison for a debug document.
func (compare CompareFunc) String() string {
	switch compare {
	case CompareAlways:
		return "always"
	case CompareNever:
		return "never"
	case CompareLess:
		return "less"
	case CompareLessEqual:
		return "lessEqual"
	case CompareGreater:
		return "greater"
	case CompareGreaterEqual:
		return "greaterEqual"
	case CompareEqual:
		return "equal"
	case CompareNotEqual:
		return "notEqual"
	}
	return "unknown(" + strconv.Itoa(int(compare)) + ")"
}

// CullMode selects which faces a pipeline discards.
type CullMode uint8

const (
	CullNone CullMode = iota
	CullFront
	CullBack
)

// Name spells the cull mode for a debug document.
func (mode CullMode) String() string {
	switch mode {
	case CullNone:
		return "none"
	case CullFront:
		return "front"
	case CullBack:
		return "back"
	}
	return "unknown(" + strconv.Itoa(int(mode)) + ")"
}

// FrontFace selects the winding that counts as the front face. glTF requires
// the reversed winding on nodes whose transform has a negative determinant.
type FrontFace uint8

const (
	FrontCCW FrontFace = iota
	FrontCW
)

// Name spells the winding for a debug document.
func (face FrontFace) String() string {
	switch face {
	case FrontCCW:
		return "ccw"
	case FrontCW:
		return "cw"
	}
	return "unknown(" + strconv.Itoa(int(face)) + ")"
}

// MaterialState controls fixed render-pipeline state. Depth compare and depth
// write are independent because the states 3D needs most - test but do not
// write, or test with another compare - are inexpressible as one flag. Every
// zero value is both the WebGPU default and what the backend did before the
// field existed, so MaterialState{} renders as it always has.
type MaterialState struct {
	Blend        BlendMode
	DepthCompare CompareFunc
	DepthWrite   bool
	Cull         CullMode
	FrontFace    FrontFace
}
