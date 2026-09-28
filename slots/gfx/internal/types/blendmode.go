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

// String spells the blend mode for a debug document.
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
