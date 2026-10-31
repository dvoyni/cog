package types

import "strconv"

// CullMode selects which faces a pipeline discards.
type CullMode uint8

const (
	CullNone CullMode = iota
	CullFront
	CullBack
)

// String spells the cull mode for a debug document.
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

// MarshalText names the value in a JSON document, so a snapshot spells it
// rather than numbering it.
func (mode CullMode) MarshalText() ([]byte, error) { return []byte(mode.String()), nil }
