package scene

import (
	"cmp"
	"math"
	"slices"
)

// sortEntry is what a pass sorts: a key and the index of the survivor it
// stands for. The entries are sorted rather than the draws themselves so a swap
// moves one sixteen-byte entry - twelve of payload, padded - rather than a
// draw, and the survivor index doubles as the recording ordinal because
// survivors are listed in recording order.
type sortEntry struct {
	key  uint64
	draw uint32
}

// opaqueKey is the sort key of an opaque or alpha-masked draw: material first,
// mesh second, and no depth term at all.
//
// Front-to-back was rejected. With no depth prepass it buys only what early-z
// catches unaided, and it directly fights instancing: two copies of one crate
// at different distances would land far apart, so a hundred-crate floor is one
// draw or a hundred. The ids are scene's own dense ones - gfx's ShaderID,
// PipelineID and TextureID are assigned on the render thread and scene never
// sees them.
func opaqueKey(materialID, meshID uint32) uint64 {
	return uint64(materialID)<<32 | uint64(meshID)
}

// blendKey is the sort key of a blended draw: its view-space depth, flipped so
// that a farther draw sorts first. Sorting is scene's entire contribution to
// transparency - the blend state itself already writes no depth.
//
// The float's bits are made monotonic - negative depths are complemented,
// positive ones have the sign bit set - and then inverted whole, so an
// ascending sort walks back to front.
func blendKey(depth float32) uint64 {
	bits := math.Float32bits(depth)
	if bits&(1<<31) != 0 {
		bits = ^bits
	} else {
		bits |= 1 << 31
	}
	return uint64(^bits)
}

// sortEntries orders one class of one pass in place. The recording ordinal is
// the final tiebreak, so an unstable sort is still deterministic frame to
// frame, and the sort allocates nothing on a reused slice.
func sortEntries(entries []sortEntry) {
	slices.SortFunc(entries, func(a, b sortEntry) int {
		if a.key != b.key {
			return cmp.Compare(a.key, b.key)
		}
		return cmp.Compare(a.draw, b.draw)
	})
}
