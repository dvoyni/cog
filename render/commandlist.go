package render

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// Order places a command list among the frame's others, in one space every
// user of render shares: the frame runs its lists from the lowest order up.
// Two lists of a frame never share an order, and the lowest, math.MinInt32,
// is render's own.
type Order int32

// ownOrder is the order of render's own command list, below every user's.
const ownOrder Order = math.MinInt32

// CommandList is one task's run of passes for a frame, kept in the order they
// were recorded, from Frame.BeginCommandList to its End. One task fills it;
// lists fill concurrently. A refused list records into nothing.
type CommandList struct {
	// frame is the parameter the list was begun from, and gfxFrame gfx's
	// frame it records into.
	frame    Frame
	gfxFrame gfx.Frame
	// encoder is the list's gfx frame encoder; nil when the list records into
	// nothing, refused or ended.
	encoder gfx.CommandEncoder
}

// BeginPass begins a pass into the targets descriptor declares, to record
// draws into until its End. A pass into a window with no framebuffer this
// frame, minimized or hidden, is skipped silently. A pass with no colour
// targets, more than the device allows (8 by WebGPU's default limits), a
// target naming no window, or targets of different sizes is refused and
// reported once. A skipped or refused pass records
// nothing, so its draws are skipped.
func (l *CommandList) BeginPass(descriptor PassDescriptor) Pass {
	if l.encoder == nil {
		return Pass{}
	}
	state := l.frame.state.Get()
	device := l.frame.device.Get()
	if err := checkColorTargets(descriptor, device.Limits().MaxColorAttachments); err != nil {
		state.report(ReportKindPassTargetsRefused, err)
		return Pass{}
	}
	attachments := make([]gfx.RenderPassColorAttachment, len(descriptor.ColorTargets))
	// firstSize is the first target's size, and sizeError the first target
	// found of another, compared only when the pass has several targets.
	var firstSize gfx.Extent3D
	var sizeError error
	for index, target := range descriptor.ColorTargets {
		view := device.Framebuffer(l.gfxFrame, target.Window)
		if view == (gfx.TextureView{}) {
			return Pass{}
		}
		attachments[index] = makeColorAttachment(view, target)
		if len(descriptor.ColorTargets) == 1 {
			break
		}
		size := device.TextureDescriptor(device.TextureViewTexture(view)).Size
		switch {
		case index == 0:
			firstSize = size
		case size != firstSize && sizeError == nil:
			sizeError = fmt.Errorf("render: pass %q's colour target %d is %dx%d, and its first %dx%d", descriptor.Label, index, size.Width, size.Height, firstSize.Width, firstSize.Height)
		}
	}
	if sizeError != nil {
		state.report(ReportKindPassTargetsRefused, sizeError)
		return Pass{}
	}
	return Pass{encoder: l.encoder.BeginRenderPass(gfx.RenderPassDescriptor{Label: descriptor.Label, ColorAttachments: attachments})}
}

// End ends the list: nothing more is recorded into it.
func (l *CommandList) End() {
	l.encoder = nil
}

// checkColorTargets answers why descriptor's colour targets are refused
// before any is looked up: none, more than maxCount, or one naming no window.
func checkColorTargets(descriptor PassDescriptor, maxCount uint32) error {
	switch count := len(descriptor.ColorTargets); {
	case count == 0:
		return fmt.Errorf("render: pass %q has no colour targets", descriptor.Label)
	case count > int(maxCount):
		return fmt.Errorf("render: pass %q has %d colour targets, more than %d", descriptor.Label, count, maxCount)
	}
	for index, target := range descriptor.ColorTargets {
		if target.Window == (platform.Window{}) {
			return fmt.Errorf("render: pass %q's colour target %d names no window", descriptor.Label, index)
		}
	}
	return nil
}

// makeColorAttachment answers gfx's attachment of target, drawn into view.
func makeColorAttachment(view gfx.TextureView, target ColorTarget) gfx.RenderPassColorAttachment {
	attachment := gfx.RenderPassColorAttachment{View: view, LoadOp: gfx.LoadOpLoad, StoreOp: gfx.StoreOpStore}
	if target.Clear {
		attachment.LoadOp = gfx.LoadOpClear
		attachment.ClearValue = gfx.Color(target.ClearColor)
	}
	if target.Discard {
		attachment.StoreOp = gfx.StoreOpDiscard
	}
	return attachment
}
