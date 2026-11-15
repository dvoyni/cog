package render

import (
	"errors"
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
// were recorded, from Frame.BeginCommandList to its End, with the chunks of
// the temporary arena its temporary buffers and pass and draw uniform blocks
// are blocks of. One task fills it; lists fill concurrently, each into
// chunks of its own. A refused list records into nothing.
type CommandList struct {
	// frame is the parameter the list was begun from, and gfxFrame gfx's
	// frame it records into.
	frame    Frame
	gfxFrame gfx.Frame
	// encoder is the list's gfx frame encoder, and listState what it keeps
	// while it records; nil when the list records into nothing, refused or
	// ended.
	encoder   gfx.CommandEncoder
	listState *commandListState
}

// BeginPass begins a pass into the targets descriptor declares, to record
// draws into until its End. A pass into a window with no framebuffer this
// frame, minimized or hidden, even as a resolve target, is skipped silently.
// A pass is refused and reported once when it has:
//   - neither a colour target nor a depth-stencil target, or more colour
//     targets than the device allows (8 by WebGPU's default limits);
//   - a target naming neither or both of a window and a texture, or a
//     texture render does not hold or with no such mip level or layer;
//   - a colour target of no colour format a pass draws into, or a
//     depth-stencil target of no depth or stencil format;
//   - targets of different sizes or sample counts;
//   - a resolve from a target of one sample, or into a target of another
//     size or format or of several samples;
//   - a read-only depth-stencil target cleared or discarded.
//
// A skipped or refused pass records nothing, so its draws are skipped.
func (l *CommandList) BeginPass(descriptor PassDescriptor) Pass {
	if l.encoder == nil {
		return Pass{}
	}
	passDescriptor, formats, skipped, err := l.makeRenderPassDescriptor(descriptor)
	switch {
	case err != nil:
		l.frame.state.Get().report(ReportKindPassTargetsRefused, fmt.Errorf("render: pass %q: %w", descriptor.Label, err))
		return Pass{}
	case skipped:
		return Pass{}
	}
	l.listState.clearParameterSettings()
	pass := Pass{frame: l.frame, listState: l.listState, encoder: l.encoder.BeginRenderPass(passDescriptor), formats: formats}
	for _, target := range descriptor.ColorTargets {
		pass.addTarget(drawnTarget{texture: target.Texture, mipLevel: target.MipLevel, layer: target.Layer})
		pass.addTarget(drawnTarget{texture: target.Resolve.Texture, mipLevel: target.Resolve.MipLevel, layer: target.Resolve.Layer})
	}
	target := descriptor.DepthStencil
	pass.addTarget(drawnTarget{texture: target.Texture, mipLevel: target.MipLevel, layer: target.Layer, readOnly: target.ReadOnly})
	return pass
}

// AcquireTemporaryBuffer answers a range of the temporary arena holding
// data, its bytes as Go lays them out, T as MakeBuffer's, at an offset that
// is a multiple of 256, which binds at any offset alignment WebGPU allows:
// vertices, indices or rows, for a mesh, a pass or draw parameter or a
// copy, in any of the frame's lists. Taking one makes no gfx object: the
// list copies data into an arena chunk of its own, of Config's
// TemporaryChunkSize, or of data's size rounded up to a multiple of it where
// it is larger, and hands each
// chunk to gfx in one write at its End, so a range taken from a list never
// ended holds nothing. The range is valid until the frame ends: a later
// frame hands the same bytes to another user, so a range kept past its frame
// silently reads theirs. No data, or a list recording into nothing, answers
// the zero BufferRange; a chunk gfx refuses to make is logged, and the zero
// BufferRange answered.
func (l *CommandList) AcquireTemporaryBuffer[T any](data []T) BufferRange {
	if l.encoder == nil {
		return BufferRange{}
	}
	state := l.frame.state.Get()
	bufferRange, err := l.listState.allocate(castToBytes(data), state.temporaryChunkSize, &state.arena, l.frame.table.Get(), l.frame.device.Get())
	if err != nil {
		state.logUnlessLost(fmt.Errorf("render: acquiring a temporary buffer: %w", err))
	}
	return bufferRange
}

// End ends the list: nothing more is recorded into it, and the chunks of
// its temporary buffers and pass and draw uniform blocks go to gfx, one
// write each, landing before every command list of the frame's submit.
func (l *CommandList) End() {
	if l.encoder != nil {
		l.listState.writeChunks(l.frame.table.Get(), l.frame.device.Get())
	}
	l.encoder = nil
	l.listState = nil
}

// makeRenderPassDescriptor answers gfx's descriptor of the pass descriptor
// declares, the formats and sample count of its targets, and true when the
// pass is skipped for a window with no framebuffer this frame; an error
// saying why it is refused.
func (l *CommandList) makeRenderPassDescriptor(descriptor PassDescriptor) (gfx.RenderPassDescriptor, passFormat, bool, error) {
	colorCount := len(descriptor.ColorTargets)
	maxColorCount := min(int(l.frame.device.Get().Limits().MaxColorAttachments), maxColorTargetCount)
	switch {
	case colorCount == 0 && descriptor.DepthStencil.Texture == (Texture{}):
		return gfx.RenderPassDescriptor{}, passFormat{}, false, errors.New("it has neither a colour target nor a depth-stencil target")
	case colorCount > maxColorCount:
		return gfx.RenderPassDescriptor{}, passFormat{}, false, fmt.Errorf("it has %d colour targets, more than %d", colorCount, maxColorCount)
	}
	formats := passFormat{colorCount: colorCount}
	passDescriptor := gfx.RenderPassDescriptor{Label: descriptor.Label}
	if colorCount > 0 {
		passDescriptor.ColorAttachments = make([]gfx.RenderPassColorAttachment, colorCount)
	}
	// firstTarget is the first target found, whose size and sample count the
	// others must have.
	var firstTarget passTarget
	skipped := false
	for index, target := range descriptor.ColorTargets {
		attachment, foundTarget, ok, err := l.makeColorAttachment(target, firstTarget)
		switch {
		case err != nil:
			return gfx.RenderPassDescriptor{}, passFormat{}, false, fmt.Errorf("colour target %d %w", index, err)
		case !ok:
			skipped = true
		case firstTarget.view == (gfx.TextureView{}):
			firstTarget = foundTarget
		}
		passDescriptor.ColorAttachments[index] = attachment
		formats.colorFormats[index] = foundTarget.format
	}
	if target := descriptor.DepthStencil; target.Texture != (Texture{}) {
		attachment, foundTarget, err := l.makeDepthStencilAttachment(target, firstTarget)
		if err != nil {
			return gfx.RenderPassDescriptor{}, passFormat{}, false, fmt.Errorf("depth-stencil target %w", err)
		}
		passDescriptor.DepthStencilAttachment = attachment
		formats.depthStencilFormat = foundTarget.format
		if firstTarget.view == (gfx.TextureView{}) {
			firstTarget = foundTarget
		}
	}
	formats.sampleCount = firstTarget.sampleCount
	return passDescriptor, formats, skipped, nil
}

// makeColorAttachment answers gfx's attachment of the colour target, and the
// target found, which agrees with firstTarget, the first target found of its
// pass; false when it is skipped for a window with no framebuffer this
// frame, and an error saying why it is refused.
func (l *CommandList) makeColorAttachment(target ColorTarget, firstTarget passTarget) (gfx.RenderPassColorAttachment, passTarget, bool, error) {
	foundTarget, ok, err := l.findTarget(target.Window, target.Texture, target.MipLevel, target.Layer)
	if err == nil && ok && foundTarget.format.info().flags&textureFormatFlagColor == 0 {
		err = fmt.Errorf("is of format %d, no colour format a pass draws into", foundTarget.format)
	}
	if err == nil && ok {
		err = foundTarget.checkAgreement(firstTarget)
	}
	if err != nil || !ok {
		return gfx.RenderPassColorAttachment{}, passTarget{}, false, err
	}
	loadOp, storeOp := makeLoadStoreOps(target.Clear, target.Discard)
	attachment := gfx.RenderPassColorAttachment{View: foundTarget.view, LoadOp: loadOp, StoreOp: storeOp, ClearValue: gfx.Color(target.ClearColor)}
	if target.Resolve == (ResolveTarget{}) {
		return attachment, foundTarget, true, nil
	}
	resolveTarget, ok, err := l.findTarget(target.Resolve.Window, target.Resolve.Texture, target.Resolve.MipLevel, target.Resolve.Layer)
	switch {
	case err != nil:
		return gfx.RenderPassColorAttachment{}, passTarget{}, false, fmt.Errorf("resolves into a target that %w", err)
	case !ok:
		return gfx.RenderPassColorAttachment{}, passTarget{}, false, nil
	case foundTarget.sampleCount == 1:
		return gfx.RenderPassColorAttachment{}, passTarget{}, false, errors.New("has one sample, and nothing to resolve")
	case resolveTarget.sampleCount != 1 || resolveTarget.format != foundTarget.format || resolveTarget.width != foundTarget.width || resolveTarget.height != foundTarget.height:
		return gfx.RenderPassColorAttachment{}, passTarget{}, false, fmt.Errorf("resolves into a target %dx%d of format %d with %d samples, not %dx%d of format %d with one", resolveTarget.width, resolveTarget.height, resolveTarget.format, resolveTarget.sampleCount, foundTarget.width, foundTarget.height, foundTarget.format)
	}
	attachment.ResolveTarget = resolveTarget.view
	return attachment, foundTarget, true, nil
}

// makeDepthStencilAttachment answers gfx's attachment of the depth-stencil
// target, which agrees with firstTarget, the first target found of its pass:
// loaded and stored, or read-only, in each aspect its format has; and the
// target found; an error saying why it is refused.
func (l *CommandList) makeDepthStencilAttachment(target DepthStencilTarget, firstTarget passTarget) (gfx.RenderPassDepthStencilAttachment, passTarget, error) {
	foundTarget, _, err := l.findTarget(platform.Window{}, target.Texture, target.MipLevel, target.Layer)
	formatFlags := foundTarget.format.info().flags
	switch {
	case err != nil:
	case formatFlags&(textureFormatFlagDepth|textureFormatFlagStencil) == 0:
		err = fmt.Errorf("is of format %d, no depth or stencil format", foundTarget.format)
	case target.ReadOnly && (target.Clear || target.Discard):
		err = errors.New("is read-only, and cleared or discarded")
	default:
		err = foundTarget.checkAgreement(firstTarget)
	}
	if err != nil {
		return gfx.RenderPassDepthStencilAttachment{}, passTarget{}, err
	}
	attachment := gfx.RenderPassDepthStencilAttachment{View: foundTarget.view, DepthClearValue: target.ClearDepth, StencilClearValue: target.ClearStencil}
	loadOp, storeOp := makeLoadStoreOps(target.Clear, target.Discard)
	switch {
	case formatFlags&textureFormatFlagDepth == 0:
	case target.ReadOnly:
		attachment.DepthReadOnly = true
	default:
		attachment.DepthLoadOp = loadOp
		attachment.DepthStoreOp = storeOp
	}
	switch {
	case formatFlags&textureFormatFlagStencil == 0:
	case target.ReadOnly:
		attachment.StencilReadOnly = true
	default:
		attachment.StencilLoadOp = loadOp
		attachment.StencilStoreOp = storeOp
	}
	return attachment, foundTarget, nil
}

// findTarget answers the target of a pass naming window, or texture at
// mipLevel and layer, and false for a window with no framebuffer this frame;
// an error when it names neither or both, or a texture render does not hold
// or with no such mip level or layer.
func (l *CommandList) findTarget(window platform.Window, texture Texture, mipLevel, layer uint32) (passTarget, bool, error) {
	device := l.frame.device.Get()
	switch {
	case window != (platform.Window{}) && texture != (Texture{}):
		return passTarget{}, false, errors.New("names both a window and a texture")
	case window != (platform.Window{}):
		view := device.Framebuffer(l.gfxFrame, window)
		if view == (gfx.TextureView{}) {
			return passTarget{}, false, nil
		}
		return makePassTarget(view, device.TextureDescriptor(device.TextureViewTexture(view)), 0), true, nil
	case texture != (Texture{}):
		view, descriptor, err := l.frame.table.Get().findLevelView(device, texture, mipLevel, layer)
		if err != nil {
			return passTarget{}, false, fmt.Errorf("names %w", err)
		}
		return makePassTarget(view, descriptor, mipLevel), true, nil
	default:
		return passTarget{}, false, errors.New("names neither a window nor a texture")
	}
}

// makeLoadStoreOps answers the load and store of a target cleared or kept,
// and discarded or stored.
func makeLoadStoreOps(clear, discard bool) (gfx.LoadOp, gfx.StoreOp) {
	loadOp := gfx.LoadOpLoad
	if clear {
		loadOp = gfx.LoadOpClear
	}
	storeOp := gfx.StoreOpStore
	if discard {
		storeOp = gfx.StoreOpDiscard
	}
	return loadOp, storeOp
}
