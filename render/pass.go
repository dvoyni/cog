package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// PassDescriptor is what a pass declares: its label, for reports and
// inspections; its colour targets, up to 8 by WebGPU's default limits; and
// its depth-stencil target, a zero Texture meaning none. Its targets are of
// one size and sample count, each texture's at the mip level drawn into, and
// it has a colour target or a depth-stencil target.
type PassDescriptor struct {
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
}

// ColorTarget is one colour target of a pass: a window, drawn into, never
// sampled; or a texture of a colour format, at one mip level and one layer.
// It names one of the two. Its zero load and store keep what the target
// holds and store what the pass draws.
type ColorTarget struct {
	Window          platform.Window
	Texture         Texture
	MipLevel, Layer uint32
	// Clear clears the target to ClearColor as the pass begins, in place of
	// keeping what it holds.
	Clear      bool
	ClearColor Color
	// Discard throws away what the pass draws into the target as it ends, in
	// place of storing it.
	Discard bool
	// Resolve, of a multisampled target, is where the pass resolves what it
	// draws into the target as it ends; its zero value is none.
	Resolve ResolveTarget
}

// ResolveTarget is where a multisampled colour target resolves: a window, or
// a texture of the target's format, with one sample, at one mip level and
// one layer, of the target's size. It names one of the two, or neither for
// no resolve.
type ResolveTarget struct {
	Window          platform.Window
	Texture         Texture
	MipLevel, Layer uint32
}

// DepthStencilTarget is the depth-stencil target of a pass: a texture of a
// depth or stencil format, at one mip level and one layer, its aspects
// following its format. Its zero load and store keep what the target holds
// and store what the pass draws.
type DepthStencilTarget struct {
	Texture         Texture
	MipLevel, Layer uint32
	// Clear clears the target's depth to ClearDepth and its stencil to
	// ClearStencil as the pass begins, in place of keeping what it holds.
	Clear        bool
	ClearDepth   float32
	ClearStencil uint32
	// Discard throws away what the pass writes into the target as it ends,
	// in place of storing it.
	Discard bool
	// ReadOnly tests the pass's draws against the target without writing
	// it, so they may sample it too. A read-only target is neither cleared
	// nor discarded: a pass declaring one so is refused.
	ReadOnly bool
}

// Color is a linear RGBA colour.
type Color struct {
	R, G, B, A float64
}

// Pass is one pass of a command list, from CommandList.BeginPass to its End.
// A skipped or refused pass records nothing.
type Pass struct {
	// encoder is the pass's gfx encoder; nil when the pass records nothing,
	// skipped, refused or ended.
	encoder gfx.RenderPassEncoder
}

// End ends the pass: nothing more is recorded into it.
func (p *Pass) End() {
	if p.encoder != nil {
		p.encoder.End()
		p.encoder = nil
	}
}
