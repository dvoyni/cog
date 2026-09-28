package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// SnapshotView is what every snapshot response carries whatever it is a
// snapshot of: the three coordinate sizes, and whether producing it cost a
// step.
//
// All three sizes, not two. A capture reports pixels and window units and
// deliberately omits the logical viewport, because that is the game's own
// sizing policy and means nothing to an agent looking at a picture. A snapshot
// is the inverse case - its coordinates are in that space - so leaving it out
// breaks the find-the-button-click-the-button flow silently, which is the
// failure mode worth spending a field on.
type SnapshotView struct {
	// PixelWidth and PixelHeight are the framebuffer's size: the units a
	// capture's image is in.
	PixelWidth  int `json:"pixelWidth"`
	PixelHeight int `json:"pixelHeight"`
	// WindowWidth and WindowHeight are the window's size in device-independent
	// units: what input capabilities take, and what a click is expressed in.
	WindowWidth  float32 `json:"windowWidth"`
	WindowHeight float32 `json:"windowHeight"`
	// ViewportWidth and ViewportHeight are the logical world size the game's
	// own sizing policy resolved to. Snapshot coordinates are in this space,
	// and window = viewport x WindowWidth / ViewportWidth.
	ViewportWidth  float32 `json:"viewportWidth"`
	ViewportHeight float32 `json:"viewportHeight"`
	// Tick names the update tick this snapshot describes, counting from one
	// and never resetting. It is here so that several snapshots can be shown
	// to describe one tick rather than merely claimed to: two responses
	// carrying one number describe one moment, and two carrying different
	// numbers have split, whatever else they say.
	//
	// It is one field rather than three because gfx_frame, canvas_draws and
	// ui_layout all embed this view, and a tick is the same tick in all of
	// them. Zero means the host does not number ticks.
	Tick int64 `json:"tick"`
	// Stepped reports that the engine was paused and one tick was stepped to
	// have something to record. Joined reports that the step joined one
	// another arm had already raised, which is what makes snapshots armed
	// together describe one tick rather than three.
	//
	// Neither is evidence on its own: two snapshots both reporting Stepped
	// may be one tick apart. Tick is what settles it.
	Stepped bool `json:"stepped"`
	Joined  bool `json:"joined,omitempty"`
}

// SnapshotViewOf fills in the three coordinate sizes from a viewport. The
// step fields belong to the capability body, which is the only place that
// knows whether one was performed, and Tick to the snapshot, which is the
// only thing produced inside the tick it names.
func SnapshotViewOf(viewport types.Viewport) SnapshotView {
	return SnapshotView{
		PixelWidth:     int(viewport.FramebufferWidth),
		PixelHeight:    int(viewport.FramebufferHeight),
		WindowWidth:    viewport.WindowWidth,
		WindowHeight:   viewport.WindowHeight,
		ViewportWidth:  viewport.Width,
		ViewportHeight: viewport.Height,
	}
}
