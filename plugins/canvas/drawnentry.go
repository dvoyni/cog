package canvas

import "github.com/dvoyni/cog/libs/m"

// drawnEntry is what the renderer found of one of a frame's entries,
// written at its entry's index: its bounds, grown by its material's
// CullMargin, its batch key, and why it is skipped, skipReasonNone for an
// entry drawn. A skipped entry's bounds and batch key are zero.
type drawnEntry struct {
	bounds m.Rect
	key    batchKey
	skip   skipReason
}
