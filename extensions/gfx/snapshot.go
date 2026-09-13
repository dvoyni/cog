package gfx

import "github.com/dvoyni/cog/kernel"

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd kernel.Command[ArmFrameRequest, ArmFrameResponse]

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest struct {
	// Pass, when set, keeps only the passes whose Label is exactly it. Passes
	// it drops are counted in the result rather than silently missing, and
	// every pass keeps its own declaration index, so an index read off a
	// filtered snapshot still addresses the same pass in an unfiltered one.
	Pass string
}

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse struct {
	// Done receives exactly one FrameSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan FrameSnapshot
	// Viewport is the window as of the arm, which a capability body cannot
	// read for itself. A resize between the arm and the tick it binds to is a
	// stated non-guarantee, exactly as it is for a capture.
	Viewport Viewport
}

// FrameSnapshot is one produced snapshot, or the reason there is none. One
// struct carries both so that a caller cannot handle one and forget the other.
type FrameSnapshot struct {
	Frame FrameView
	// Tick is app.UpdateEvent.Tick of the tick the snapshot was taken in. It
	// travels with the snapshot rather than being asked for afterwards,
	// because only the tick itself knows which one it was.
	Tick int64
	Err  error
}

// FrameView is one tick's renderer declarations, rendered while they are still
// alive. It is not a copy of the queue: no queue outlives the tick that filled
// it, and between ticks the queue is empty rather than stale, so the view is
// produced inside the tick and shaped by the request that asked for it.
//
// Every index in it is a source index - a position in the queue that recorded
// the thing, never a position in the emitted array. Filtering makes the
// emitted array a subset, and if indices were positions in that subset every
// cross-reference would point at the wrong thing. With source indices the
// index is the address, and an elided pass stays addressable for free.
type FrameView struct {
	// Passes are the frame's declared passes in run order - Order first,
	// declaration sequence breaking ties - which is the order the translator
	// runs them in.
	Passes []PassView `json:"passes,omitempty"`
	// ResourceOps are the frame's resource traffic: the durable queue's
	// pending operations first, then the frame queue's own, which is the order
	// the translator emits them in. They belong to no pass - every bake is
	// hoisted ahead of all of them - so they carry no pass index and a pass
	// filter never drops them: "was the texture ever baked" is half of what
	// this answers, and a filter that hid it would hide the answer.
	ResourceOps []ResourceOpView `json:"resourceOps,omitempty"`
	// PassCount, DrawCount and InstanceCount are the whole frame's, whatever
	// the filter kept, so a filtered response still says how much of the frame
	// it is describing.
	PassCount     int `json:"passCount"`
	DrawCount     int `json:"drawCount"`
	InstanceCount int `json:"instanceCount"`
	// StrayDraws are draws recorded outside every pass. They are dropped by
	// the renderer and reported as ErrDrawWithoutPass, and they are the answer
	// to "nothing is on screen" often enough to be named here too.
	StrayDraws int `json:"strayDraws,omitempty"`
	// Filter echoes the pass label the request asked for, and OmittedPasses is
	// how many passes it dropped. Whatever a snapshot omits it says it
	// omitted: a tool that silently truncates cannot be told from a game that
	// drew nothing.
	Filter        string `json:"filter,omitempty"`
	OmittedPasses int    `json:"omittedPasses,omitempty"`
}

// PassView is one declared render pass: where it draws, in what order, what
// happens to its attachments at either end, and how much work it carries.
type PassView struct {
	// Index is the pass's declaration index in the frame's queue: its address,
	// stable under filtering. Run is its position in the frame's whole run
	// order, which is what "a pass ordered wrong" is read off.
	Index int    `json:"index"`
	Run   int    `json:"run"`
	Label string `json:"label,omitempty"`
	Order int    `json:"order"`
	// Target is screen, texture or none. A pass drawing into something that is
	// not the screen, and nothing compositing it afterwards, is one of the
	// ways a frame ends up black.
	Target        string    `json:"target"`
	TargetTexture TextureID `json:"targetTexture,omitempty"`
	TargetWidth   int       `json:"targetWidth,omitempty"`
	TargetHeight  int       `json:"targetHeight,omitempty"`
	TargetMip     int       `json:"targetMip,omitempty"`
	TargetLayer   int       `json:"targetLayer,omitempty"`
	// Depth is auto, texture or none.
	Depth        string    `json:"depth"`
	DepthTexture TextureID `json:"depthTexture,omitempty"`
	Load         string    `json:"load"`
	// Clear is the colour the pass clears to - r, g, b, a - and is present
	// only when Load is clear.
	Clear      []float32 `json:"clear,omitempty"`
	Store      string    `json:"store"`
	DepthLoad  string    `json:"depthLoad"`
	DepthClear float32   `json:"depthClear,omitempty"`
	DepthStore string    `json:"depthStore"`
	// Draws and Instances are counted rather than listed, because a draw's
	// mesh and material are opaque handles with nothing to resolve them
	// against. The aggregate is the informative part, and it is exact.
	Draws     int `json:"draws"`
	Instances int `json:"instances"`
	// Runs reports whether the pass is observable at all. A pass with no draws
	// that loads nothing is skipped by the translator, which is the difference
	// between a pass that ran and drew nothing and a pass that never ran.
	Runs bool `json:"runs"`
}

// ResourceOpView is one resource operation: what it does, to which handle, and
// how big the thing is. Bulk bytes are reported as a count and left where they
// are.
type ResourceOpView struct {
	// Queue is durable or frame: the persistent ResourceQueue, whose
	// operations wait until the render thread consumes them, or the frame's
	// own queue, whose uploads live and die with the frame. Index is the
	// position within that queue, so the address is the pair.
	Queue string `json:"queue"`
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	// Path is the resource path, for the operations that name one.
	Path string `json:"path,omitempty"`
	// Buffer, BufferKind and Size describe a buffer operation.
	Buffer     BufferID `json:"buffer,omitempty"`
	BufferKind string   `json:"bufferKind,omitempty"`
	Size       int      `json:"size,omitempty"`
	// Texture and the fields after it describe a texture operation.
	Texture    TextureID `json:"texture,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Layers     int       `json:"layers,omitempty"`
	Layer      int       `json:"layer,omitempty"`
	Region     *Region   `json:"region,omitempty"`
	Format     string    `json:"format,omitempty"`
	Mipmaps    bool      `json:"mipmaps,omitempty"`
	Renderable bool      `json:"renderable,omitempty"`
	// Bytes is how much data the operation uploads. The data itself does not
	// travel: a baked texture in a response is a base64 megabyte nobody asked
	// for.
	Bytes int `json:"bytes,omitempty"`
}
