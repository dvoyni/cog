package render

// poolHold is whether something pooled is held this frame, and for how many
// frames since the last that held it it has gone unused: what the texture
// pool and the temporary arena age their entries by.
type poolHold struct {
	held       bool
	idleFrames int
}

// endFrame takes the entry back for the next frame, and answers whether it
// has now gone unused for idleFrames frames, to be freed.
func (h *poolHold) endFrame(idleFrames int) bool {
	if h.held {
		h.held = false
		h.idleFrames = 0
	} else {
		h.idleFrames++
	}
	return h.idleFrames >= idleFrames
}
