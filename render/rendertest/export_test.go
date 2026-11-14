package rendertest

import "github.com/dvoyni/cog/gfx/recording"

// Calls answers every call gfx's recording adapter has recorded so far, for
// rendertest's tests to check the order of what render did on gfx.
func (h *Harness) Calls() []recording.Call {
	return h.instance.Recording().Calls
}

// RecordedFrames answers the frames gfx's recording adapter has recorded so
// far, for rendertest's tests to read the commands of render's own list.
func (h *Harness) RecordedFrames() []recording.Frame {
	return h.instance.Recording().Frames()
}
