package types

import "strconv"

// AddressMode selects how texture coordinates outside [0,1] are sampled on one
// axis. It is an enum rather than a bitmask because mirroring is a third mode,
// not a combination of the other two, and a flag that reads as a combination is
// a flag that gets silently reinterpreted.
type AddressMode uint8

const (
	AddressClamp AddressMode = iota
	AddressRepeat
	AddressMirror
)

// Name spells the address mode for a debug document.
func (mode AddressMode) String() string {
	switch mode {
	case AddressClamp:
		return "clamp"
	case AddressRepeat:
		return "repeat"
	case AddressMirror:
		return "mirror"
	}
	return "unknown(" + strconv.Itoa(int(mode)) + ")"
}

// MarshalText names the value in a JSON document, so a snapshot spells it
// rather than numbering it.
func (mode AddressMode) MarshalText() ([]byte, error) { return []byte(mode.String()), nil }
