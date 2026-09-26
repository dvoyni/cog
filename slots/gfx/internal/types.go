package internal

import "github.com/dvoyni/cog/slots/gfx/internal/types"

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
	// StrayDraws are draws naming no pass declared this frame. They are dropped by
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
	Target        string          `json:"target"`
	TargetTexture types.TextureID `json:"targetTexture,omitempty"`
	TargetWidth   int             `json:"targetWidth,omitempty"`
	TargetHeight  int             `json:"targetHeight,omitempty"`
	TargetMip     int             `json:"targetMip,omitempty"`
	TargetLayer   int             `json:"targetLayer,omitempty"`
	// Depth is auto, texture or none.
	Depth        string          `json:"depth"`
	DepthTexture types.TextureID `json:"depthTexture,omitempty"`
	Load         string          `json:"load"`
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
	// position among that queue's resource ops, so the address is the pair.
	Queue string `json:"queue"`
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	// Path is the resource path, for the operations that name one.
	Path string `json:"path,omitempty"`
	// Buffer, BufferKind and Size describe a buffer operation.
	Buffer     types.BufferID `json:"buffer,omitempty"`
	BufferKind string         `json:"bufferKind,omitempty"`
	Size       int            `json:"size,omitempty"`
	// Texture and the fields after it describe a texture operation.
	Texture    types.TextureID `json:"texture,omitempty"`
	Width      int             `json:"width,omitempty"`
	Height     int             `json:"height,omitempty"`
	Layers     int             `json:"layers,omitempty"`
	Layer      int             `json:"layer,omitempty"`
	Region     *types.Region   `json:"region,omitempty"`
	Format     string          `json:"format,omitempty"`
	Mipmaps    bool            `json:"mipmaps,omitempty"`
	Renderable bool            `json:"renderable,omitempty"`
	// Bytes is how much data the operation uploads. The data itself does not
	// travel: a baked texture in a response is a base64 megabyte nobody asked
	// for.
	Bytes int `json:"bytes,omitempty"`
}
