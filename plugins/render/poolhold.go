package render

// poolHold is whether something pooled is held this frame, whether it is
// leased, held from frame to frame until let go, and for how many frames
// since the last that held it it has gone unused: what the texture pool,
// the temporary arena and the staging pool age their entries by.
type poolHold struct {
	held       bool
	leased     bool
	idleFrames int
}

// endFrame takes the entry back for the next frame, unless it is leased,
// and answers whether it has now gone unused for idleFrames frames, to be
// freed; a leased entry is never unused.
func (h *poolHold) endFrame(idleFrames int) bool {
	if h.held || h.leased {
		h.held = false
		h.idleFrames = 0
	} else {
		h.idleFrames++
	}
	return h.idleFrames >= idleFrames
}
