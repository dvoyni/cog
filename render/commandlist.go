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

// copyRowAlignment is what the distance between the rows of a copy between a
// buffer and a texture is a multiple of, as WebGPU asks.
const copyRowAlignment = 256

// CommandList is one task's run of passes for a frame, kept in the order they
// were recorded, from Frame.BeginCommandList to its End, with the chunks of
// the temporary arena its temporary buffers, its pass and draw uniform
// blocks and those of the material versions its draws make are blocks of.
// One task fills it; lists fill concurrently, each into
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
	passDescriptor, formats, firstTarget, skipped, err := l.makeRenderPassDescriptor(descriptor)
	switch {
	case err != nil:
		l.frame.state.Get().report(ReportKindPassTargetsRefused, fmt.Errorf("render: pass %q: %w", descriptor.Label, err))
		return Pass{}
	case skipped:
		return Pass{}
	}
	l.listState.clearParameterSettings()
	pass := Pass{
		frame:     l.frame,
		listState: l.listState,
		encoder:   l.encoder.BeginRenderPass(passDescriptor),
		formats:   formats,
		width:     firstTarget.width,
		height:    firstTarget.height,
	}
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
	bufferRange, err := l.frame.table.Get().allocateTemporary(l.listState, castToBytes(data), l.frame.device.Get())
	if err != nil {
		state.logUnlessLost(fmt.Errorf("render: acquiring a temporary buffer: %w", err))
	}
	return bufferRange
}

// AcquireTemporaryMesh answers a mesh of descriptor valid until the frame
// ends, drawn with Pass.Draw as a made mesh is, over buffer ranges valid
// that long: usually temporary buffers of the frame's lists, or ranges of
// made buffers. It draws per-frame geometry in a vertex layout of its own,
// such as a Dear ImGui-style UI's: each of its commands is a temporary mesh
// over sub-ranges of the frame's vertices and indices, its first index the
// index range's offset and its base vertex the vertex range's. Its vertex
// layouts join a pipeline's key by value, as a made mesh's do, so it draws
// with the pipelines of any mesh of the same layouts. It is refused and
// reported as MakeMesh's is, answering the zero Mesh, as does a list
// recording into nothing. Nothing gives it back: the frame's end takes it,
// and a draw of its handle after that is skipped and reported once. Once
// render's tables have grown, acquiring one of vertex layouts drawn before
// allocates nothing.
func (l *CommandList) AcquireTemporaryMesh(descriptor MeshDescriptor) Mesh {
	if l.encoder == nil {
		return Mesh{}
	}
	table := l.frame.table.Get()
	mesh, err := table.addMesh(l.frame.device.Get(), descriptor)
	if err != nil {
		table.report(ReportKindMeshRefused, fmt.Errorf("render: acquiring temporary mesh %q: %w", descriptor.Label, err))
		return Mesh{}
	}
	l.listState.temporaryMeshes = append(l.listState.temporaryMeshes, mesh)
	return mesh
}

// BeginComputePass begins a compute pass labelled label, its label for
// reports and inspections, to record dispatches into until its End. A list
// recording into nothing answers a compute pass recording nothing.
func (l *CommandList) BeginComputePass(label string) ComputePass {
	if l.encoder == nil {
		return ComputePass{}
	}
	l.listState.clearParameterSettings()
	return ComputePass{frame: l.frame, listState: l.listState, encoder: l.encoder.BeginComputePass(gfx.ComputePassDescriptor{Label: label})}
}

// CopyBuffer copies source's bytes, a zero Size meaning the rest of its
// buffer, to destination's offset, at this point of the list: after what the
// list recorded before it and before what it records after. A copy that
// does not fit is refused and reported, recording nothing: from or into a
// buffer render does not hold or of no bytes, from a buffer made without
// BufferUsageFlagCopySrc, within one buffer, at an offset or of a size no
// multiple of 4, or from or into a range past its buffer's end, a
// destination with a Size holding fewer bytes than the copy.
func (l *CommandList) CopyBuffer(source, destination BufferRange) {
	if l.encoder == nil {
		return
	}
	table := l.frame.table.Get()
	device := l.frame.device.Get()
	sourceBuffer, size, err := source.findCopiedBuffer(table, device, gfx.BufferUsageFlagCopySrc)
	var destinationBuffer gfx.Buffer
	var destinationSize uint64
	if err == nil {
		destinationBuffer, destinationSize, err = destination.findCopiedBuffer(table, device, gfx.BufferUsageFlagCopyDst)
	}
	switch {
	case err != nil:
	case source.Buffer == destination.Buffer:
		err = errors.New("is within one buffer")
	case source.Offset%4 != 0 || destination.Offset%4 != 0 || size%4 != 0:
		err = fmt.Errorf("is %d bytes from offset %d to offset %d, where the offsets and the size are multiples of 4", size, source.Offset, destination.Offset)
	case size > destinationSize:
		err = fmt.Errorf("is %d bytes, into a destination of %d", size, destinationSize)
	}
	if err != nil {
		table.report(ReportKindCopyRefused, fmt.Errorf("render: a copy from %+v to %+v %w", source, destination, err))
		return
	}
	l.encoder.CopyBufferToBuffer(sourceBuffer, source.Offset, destinationBuffer, destination.Offset, size)
}

// CopyTexture copies sourceRegion of source, a zero width or height
// meaning the rest of its level, into destinationRegion of destination,
// from its top-left texel, at this point of the list, as CopyBuffer does. A
// copy that does not fit is refused and reported, recording nothing: from
// or into a texture render does not hold, of a mip level or layer it lacks,
// or a region past its level's edges; between textures of different formats
// or sample counts; into a destination region smaller than the copy; within
// one mip level and layer of one texture; or of part of a level of a
// texture of a depth or stencil format or of several samples, which copies
// whole levels alone.
func (l *CommandList) CopyTexture(source Texture, sourceRegion TextureRegion, destination Texture, destinationRegion TextureRegion) {
	if l.encoder == nil {
		return
	}
	sourceTexture, sourceDescriptor, width, height, err := l.findCopiedTexture(source, sourceRegion)
	var destinationTexture gfx.Texture
	var destinationDescriptor gfx.TextureDescriptor
	var destinationWidth, destinationHeight uint32
	if err == nil {
		destinationTexture, destinationDescriptor, destinationWidth, destinationHeight, err = l.findCopiedTexture(destination, destinationRegion)
	}
	isWholeLevel := func(descriptor gfx.TextureDescriptor, region TextureRegion) bool {
		levelWidth, levelHeight, _ := TextureRegion{MipLevel: region.MipLevel}.findSize(descriptor)
		return region.X == 0 && region.Y == 0 && width == levelWidth && height == levelHeight
	}
	switch {
	case err != nil:
	case sourceDescriptor.Format != destinationDescriptor.Format || sourceDescriptor.SampleCount != destinationDescriptor.SampleCount:
		err = fmt.Errorf("is between textures of formats %d and %d, of %d and %d samples, where they are the same", sourceDescriptor.Format, destinationDescriptor.Format, sourceDescriptor.SampleCount, destinationDescriptor.SampleCount)
	case width > destinationWidth || height > destinationHeight:
		err = fmt.Errorf("is %d×%d texels, into a destination region of %d×%d", width, height, destinationWidth, destinationHeight)
	case source == destination && sourceRegion.MipLevel == destinationRegion.MipLevel && sourceRegion.Layer == destinationRegion.Layer:
		err = errors.New("is within one mip level and layer of one texture")
	case (sourceDescriptor.SampleCount > 1 || TextureFormat(sourceDescriptor.Format).info().flags&(textureFormatFlagDepth|textureFormatFlagStencil) != 0) &&
		(!isWholeLevel(sourceDescriptor, sourceRegion) || !isWholeLevel(destinationDescriptor, destinationRegion)):
		err = errors.New("copies part of a level of a texture of a depth or stencil format or of several samples, which copies whole levels alone")
	}
	if err != nil {
		l.frame.table.Get().report(ReportKindCopyRefused, fmt.Errorf("render: a copy from %+v of texture %v to %+v of texture %v %w", sourceRegion, source, destinationRegion, destination, err))
		return
	}
	l.listState.textureCopies[0] = gfx.TextureCopy{
		Source:      makeImageCopyTexture(sourceTexture, sourceRegion),
		Destination: makeImageCopyTexture(destinationTexture, destinationRegion),
		Size:        gfx.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
	}
	l.encoder.CopyTextureToTexture(sourceTexture, destinationTexture, l.listState.textureCopies[:])
}

// CopyBufferToTexture copies source's bytes into region of destination, a
// zero width or height meaning the rest of its level, at this point of the
// list, as CopyBuffer does. The bytes are the region's rows of texels from
// source's offset, each texel of the format's size and each row starting a
// multiple of 256 bytes after the one before, which a copy needs: the range
// holds at least 256 bytes for each row but the last, and the last row
// whole. A copy that does not fit is refused and reported, recording
// nothing: from a buffer render does not hold, of no bytes or made without
// BufferUsageFlagCopySrc, at an offset no multiple of 4 and of the texel
// size, or of a range shorter than the rows; into a texture render does
// not hold, of several samples or of a format gfx's copies cannot fill, of
// a mip level or layer it lacks, or a region past its level's edges.
func (l *CommandList) CopyBufferToTexture(source BufferRange, destination Texture, region TextureRegion) {
	if l.encoder == nil {
		return
	}
	buffer, texture, err := l.findBufferTextureCopy(source, gfx.BufferUsageFlagCopySrc, destination, region)
	if err != nil {
		l.frame.table.Get().report(ReportKindCopyRefused, fmt.Errorf("render: a copy from %+v to %+v of texture %v %w", source, region, destination, err))
		return
	}
	l.encoder.CopyBufferToTexture(buffer, texture, l.listState.bufferTextureCopies[:])
}

// CopyTextureToBuffer copies region of source, a zero width or height
// meaning the rest of its level, into destination's bytes at this point of
// the list, as CopyBuffer does, its rows laid out as CopyBufferToTexture
// lays them. A copy that does not fit is refused and reported, recording
// nothing, as CopyBufferToTexture's is, a buffer needing no usage but its
// own.
func (l *CommandList) CopyTextureToBuffer(source Texture, region TextureRegion, destination BufferRange) {
	if l.encoder == nil {
		return
	}
	buffer, texture, err := l.findBufferTextureCopy(destination, gfx.BufferUsageFlagCopyDst, source, region)
	if err != nil {
		l.frame.table.Get().report(ReportKindCopyRefused, fmt.Errorf("render: a copy from %+v of texture %v to %+v %w", region, source, destination, err))
		return
	}
	l.encoder.CopyTextureToBuffer(texture, buffer, l.listState.bufferTextureCopies[:])
}

// ClearBuffer zeroes bufferRange's bytes, a zero Size meaning the rest of
// its buffer, at this point of the list, as CopyBuffer does. A clear that
// does not fit is refused and reported, recording nothing: of a buffer
// render does not hold or of no bytes, of a range past its buffer's end, or
// at an offset or of a size no multiple of 4.
func (l *CommandList) ClearBuffer(bufferRange BufferRange) {
	if l.encoder == nil {
		return
	}
	table := l.frame.table.Get()
	buffer, size, err := bufferRange.findCopiedBuffer(table, l.frame.device.Get(), gfx.BufferUsageFlagCopyDst)
	if err == nil && (bufferRange.Offset%4 != 0 || size%4 != 0) {
		err = fmt.Errorf("is %d bytes at offset %d, where both are multiples of 4", size, bufferRange.Offset)
	}
	if err != nil {
		table.report(ReportKindCopyRefused, fmt.Errorf("render: a clear of %+v %w", bufferRange, err))
		return
	}
	l.encoder.ClearBuffer(buffer, bufferRange.Offset, size)
}

// End ends the list: nothing more is recorded into it, and the chunks of
// its temporary buffers, its pass and draw uniform blocks and its material
// versions' go to gfx, one write each, landing before every command list of
// the frame's submit.
func (l *CommandList) End() {
	if l.encoder != nil {
		l.listState.writeChunks(l.frame.table.Get(), l.frame.device.Get())
	}
	l.encoder = nil
	l.listState = nil
}

// makeRenderPassDescriptor answers gfx's descriptor of the pass descriptor
// declares, the formats and sample count of its targets, its first target
// found, whose size every target has, and true when the pass is skipped for
// a window with no framebuffer this frame; an error saying why it is
// refused.
func (l *CommandList) makeRenderPassDescriptor(descriptor PassDescriptor) (gfx.RenderPassDescriptor, passFormat, passTarget, bool, error) {
	colorCount := len(descriptor.ColorTargets)
	maxColorCount := min(int(l.frame.device.Get().Limits().MaxColorAttachments), maxColorTargetCount)
	switch {
	case colorCount == 0 && descriptor.DepthStencil.Texture == (Texture{}):
		return gfx.RenderPassDescriptor{}, passFormat{}, passTarget{}, false, errors.New("it has neither a colour target nor a depth-stencil target")
	case colorCount > maxColorCount:
		return gfx.RenderPassDescriptor{}, passFormat{}, passTarget{}, false, fmt.Errorf("it has %d colour targets, more than %d", colorCount, maxColorCount)
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
			return gfx.RenderPassDescriptor{}, passFormat{}, passTarget{}, false, fmt.Errorf("colour target %d %w", index, err)
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
			return gfx.RenderPassDescriptor{}, passFormat{}, passTarget{}, false, fmt.Errorf("depth-stencil target %w", err)
		}
		passDescriptor.DepthStencilAttachment = attachment
		formats.depthStencilFormat = foundTarget.format
		if firstTarget.view == (gfx.TextureView{}) {
			firstTarget = foundTarget
		}
	}
	formats.sampleCount = firstTarget.sampleCount
	return passDescriptor, formats, firstTarget, skipped, nil
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

// findCopiedTexture answers the gfx texture behind texture, the descriptor
// gfx made it from, and the width and height of region of it, a zero width
// or height filled in; an error saying why region cannot be copied: a
// texture render does not hold, a mip level or layer it lacks, or a region
// past its level's edges.
func (l *CommandList) findCopiedTexture(texture Texture, region TextureRegion) (gfx.Texture, gfx.TextureDescriptor, uint32, uint32, error) {
	gfxTexture, ok := l.frame.table.Get().findTexture(texture)
	if !ok {
		return gfx.Texture{}, gfx.TextureDescriptor{}, 0, 0, fmt.Errorf("names texture %v, which render does not hold", texture)
	}
	descriptor := l.frame.device.Get().TextureDescriptor(gfxTexture)
	width, height, err := region.findSize(descriptor)
	return gfxTexture, descriptor, width, height, err
}

// findBufferTextureCopy answers the gfx buffer behind bufferRange and the
// gfx texture behind texture, for a copy between the range and region of
// the texture, whose buffer needs usage, and fills the list's state's
// bufferTextureCopies with the copy's one region, its rows laid out as
// CopyBufferToTexture says; an error saying why the copy does not fit.
func (l *CommandList) findBufferTextureCopy(bufferRange BufferRange, usage gfx.BufferUsageFlags, texture Texture, region TextureRegion) (gfx.Buffer, gfx.Texture, error) {
	gfxTexture, descriptor, width, height, err := l.findCopiedTexture(texture, region)
	if err != nil {
		return gfx.Buffer{}, gfx.Texture{}, err
	}
	texelSize := uint64(TextureFormat(descriptor.Format).info().texelSize)
	if texelSize == 0 || descriptor.SampleCount > 1 {
		return gfx.Buffer{}, gfx.Texture{}, fmt.Errorf("names texture %q, of %d samples in format %d, where it has one sample and a format gfx's copies fill", descriptor.Label, descriptor.SampleCount, descriptor.Format)
	}
	buffer, size, err := bufferRange.findCopiedBuffer(l.frame.table.Get(), l.frame.device.Get(), usage)
	if err != nil {
		return gfx.Buffer{}, gfx.Texture{}, err
	}
	rowSize := uint64(width) * texelSize
	bytesPerRow := (rowSize + copyRowAlignment - 1) &^ (copyRowAlignment - 1)
	if rowsSize := bytesPerRow*uint64(height-1) + rowSize; bufferRange.Offset%4 != 0 || bufferRange.Offset%texelSize != 0 || size < rowsSize {
		return gfx.Buffer{}, gfx.Texture{}, fmt.Errorf("is %d rows of %d bytes, %d apart, from %d bytes at offset %d, where the offset is a multiple of 4 and of the texel size and the bytes hold the rows", height, rowSize, bytesPerRow, size, bufferRange.Offset)
	}
	l.listState.bufferTextureCopies[0] = gfx.BufferTextureCopy{
		BufferLayout: gfx.ImageDataLayout{Offset: bufferRange.Offset, BytesPerRow: uint32(bytesPerRow), RowsPerImage: height},
		TextureBase:  makeImageCopyTexture(gfxTexture, region),
		Size:         gfx.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1},
	}
	return buffer, gfxTexture, nil
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
