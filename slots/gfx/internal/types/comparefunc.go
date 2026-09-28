package types

import "strconv"

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

// String spells the comparison for a debug document.
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
