package sfx

import "strconv"

// Voice is an opaque handle to one playing sound. It is comparable, copyable
// and usable as a map key, and the zero value means no voice.
//
// Its index is in the low 32 bits and its generation in the high 32, and
// neither is exported: generations start at 1, so Voice(0) means no voice
// while index 0 stays an ordinary slot, and a handle whose generation does
// not match its slot's addresses nothing.
type Voice uint64

// NoVoice is the absent handle. Compare with ==.
const NoVoice Voice = 0

// String renders a voice as its index and generation, "Voice(7v2)" being
// index 7 at generation 2, so a log tells a reused index from the handle
// before it; reading the halves back in code is what the type refuses.
func (v Voice) String() string {
	if v == NoVoice {
		return "NoVoice"
	}
	return "Voice(" + strconv.FormatUint(uint64(uint32(v)), 10) + "v" +
		strconv.FormatUint(uint64(uint32(v>>32)), 10) + ")"
}

// ClipID is an opaque handle to a prepared clip, minted by the backend's
// Install. Zero means none.
type ClipID uint32
