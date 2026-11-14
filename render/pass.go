package render

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// PassDescriptor is what a pass declares: its label, for reports and
// inspections, and its colour targets, up to 8 by WebGPU's default limits,
// all of one size.
type PassDescriptor struct {
	Label        string
	ColorTargets []ColorTarget
}

// ColorTarget is one colour target of a pass: a window, drawn into, never
// sampled. Its zero load and store keep what the target holds and store what
// the pass draws.
type ColorTarget struct {
	Window platform.Window
	// Clear clears the target to ClearColor as the pass begins, in place of
	// keeping what it holds.
	Clear      bool
	ClearColor Color
	// Discard throws away what the pass draws into the target as it ends, in
	// place of storing it.
	Discard bool
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
