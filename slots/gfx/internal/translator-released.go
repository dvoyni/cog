package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// releasedIDs is every id the translator has seen released, one bit an id.
//
// It records releases rather than bakes because the question is only whether
// an id has been let go, and a release is the one op that says so. Ids come
// from monotonic counters and are never reused, so a released bit is never
// cleared and needs no generation beside it, and the table is as long as the
// highest released id. A DrawParams set is an id a draw names too, and joins
// it as a table of its own.
type releasedIDs struct {
	buffers []uint64
}

// releaseBuffer marks one buffer id released. The table grows only on a
// release naming an id past its end, and by append, so a draw's lookup never
// allocates and a release only rarely does.
func (r *releasedIDs) releaseBuffer(id types.BufferID) {
	word := int(id / 64)
	if word >= len(r.buffers) {
		r.buffers = append(r.buffers, make([]uint64, word+1-len(r.buffers))...)
	}
	r.buffers[word] |= 1 << (id % 64)
}

// buffer reports whether a buffer id has been released.
func (r *releasedIDs) buffer(id types.BufferID) bool {
	word := int(id / 64)
	return word < len(r.buffers) && r.buffers[word]&(1<<(id%64)) != 0
}
