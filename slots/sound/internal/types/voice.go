package types

import "strconv"

// Voice is an opaque handle to one playing sound. It is comparable, copyable
// and usable as a map key, and the zero value means no Voice.
//
// It is ecs.Entity's shape verbatim, down to the reasoning: a uint64 with the
// index in the low 32 bits and the generation in the high 32, and that split is
// deliberately not public contract - there is no exported Index or Generation,
// only the unexported accessors below. A struct of two uint32s is the same
// eight bytes and equally comparable, but it leaks its layout into every call
// site and can never be re-cut.
//
// Generations start at 1, so Voice(0) unambiguously means "no Voice" while
// index 0 stays an ordinary usable slot. Staleness is therefore one compare
// inside sound and is never surfaced: a handle whose generation does not match
// its slot's addresses nothing, which is what lets every verb be a no-op on a
// Voice that is gone without an error path.
type Voice uint64

// NoVoice is the absent handle. Compare with ==.
const NoVoice Voice = 0

func newVoice(index, generation uint32) Voice {
	return Voice(uint64(generation)<<32 | uint64(index))
}

func (v Voice) idx() uint32 { return uint32(v) }

func (v Voice) gen() uint32 { return uint32(v >> 32) }

// String renders a Voice as its index and generation, "Voice(7v2)" being index
// 7 at generation 2. The halves are shown because a log that cannot distinguish
// a recycled index from the handle that preceded it is useless; reading them
// back in code is what the type refuses.
func (v Voice) String() string {
	if v == NoVoice {
		return "NoVoice"
	}
	return "Voice(" + strconv.FormatUint(uint64(v.idx()), 10) + "v" +
		strconv.FormatUint(uint64(v.gen()), 10) + ")"
}
