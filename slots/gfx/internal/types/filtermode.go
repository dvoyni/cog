package types

import "strconv"

// FilterMode selects texture minification/magnification filtering.
type FilterMode uint8

const (
	FilterLinear FilterMode = iota
	FilterNearest
)

// Name spells the filter for a debug document. canvas reports one on every
// sprite transform, so a sampler filter reaches an agent in one spelling
// whichever tool showed it.
func (mode FilterMode) String() string {
	switch mode {
	case FilterLinear:
		return "linear"
	case FilterNearest:
		return "nearest"
	}
	return "unknown(" + strconv.Itoa(int(mode)) + ")"
}
