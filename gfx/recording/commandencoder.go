package recording

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// commandEncoder is the recording adapter's encoder: it keeps the commands
// recorded into it, for Finish. One goroutine records into it at a time, as
// gfx.CommandEncoder says, so it needs no mutex of its own.
//
// A command naming a handle missing at its index, a resource without the
// usage the command puts it to, a misaligned copy or clear or one ending past
// its buffer, or a broken pass sequence is recorded all the same, and Finish answers the first such error,
// as wgpu does; a zero handle names none.
type commandEncoder struct {
	device   *device
	commands []Command
	// bufferEffects are what the copies and clears recorded do to buffers'
	// bytes, applied in order at the submit.
	bufferEffects []func()
	// err is the first command's error, for Finish.
	err error
	// passOpen is set from a pass's beginning to its End.
	passOpen bool
	// pipelineSet is set once the open pass sets its pipeline.
	pipelineSet bool
	// spent is set once the encoder is finished or discarded: wgpu answers
	// ErrReleased for a Finish after either.
	spent bool
}

// BeginRenderPass records the pass, its color attachments copied, and
// answers its encoder, which records under it. No other pass may be open,
// each attachment's view needs RenderAttachment, and its timestamp writes
// name no query set.
func (encoder *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	descriptor.ColorAttachments = slices.Clone(descriptor.ColorAttachments)
	encoder.beginPass()
	encoder.check(func(d *device) error {
		for index, attachment := range descriptor.ColorAttachments {
			if err := d.requireView(attachment.View, gfx.TextureUsageFlagRenderAttachment, fmt.Sprintf("color attachment %d", index)); err != nil {
				return err
			}
			if err := d.requireView(attachment.ResolveTarget, gfx.TextureUsageFlagRenderAttachment, fmt.Sprintf("color attachment %d's resolve target", index)); err != nil {
				return err
			}
		}
		return cmp.Or(
			d.requireView(descriptor.DepthStencilAttachment.View, gfx.TextureUsageFlagRenderAttachment, "the depth-stencil attachment"),
			requireQuerySet(descriptor.TimestampWrites.QuerySet, "the pass's timestamp writes"),
		)
	})
	encoder.record(CommandKindBeginRenderPass, descriptor)
	return renderPassEncoder{pass{encoder: encoder, index: len(encoder.commands) - 1}}
}

// BeginComputePass records the pass and answers its encoder, which records
// under it. No other pass may be open, and its timestamp writes name no query
// set.
func (encoder *commandEncoder) BeginComputePass(descriptor gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	encoder.beginPass()
	encoder.fail(requireQuerySet(descriptor.TimestampWrites.QuerySet, "the pass's timestamp writes"))
	encoder.record(CommandKindBeginComputePass, descriptor)
	return computePassEncoder{pass{encoder: encoder, index: len(encoder.commands) - 1}}
}

// ResolveQuerySet records the resolve, which always fails: there is no query
// set to resolve, none kept and the zero one naming none.
func (encoder *commandEncoder) ResolveQuerySet(querySet gfx.QuerySet, firstQuery, queryCount uint32, destination gfx.Buffer, destinationOffset uint64) {
	if querySet == (gfx.QuerySet{}) {
		encoder.fail(errors.New("recording: a resolve of no query set"))
	}
	encoder.fail(requireQuerySet(querySet, "the resolved query set"))
	encoder.record(CommandKindResolveQuerySet, ResolveQuerySetArguments{
		QuerySet:          querySet,
		FirstQuery:        firstQuery,
		QueryCount:        queryCount,
		Destination:       destination,
		DestinationOffset: destinationOffset,
	})
}

// CopyBufferToBuffer records the copy, from a buffer with CopySrc into one
// with CopyDst, its offsets and size multiples of copyBufferAlignment and its
// ranges within the buffers.
func (encoder *commandEncoder) CopyBufferToBuffer(source gfx.Buffer, sourceOffset uint64, destination gfx.Buffer, destinationOffset uint64, size uint64) {
	encoder.check(func(d *device) error {
		if err := cmp.Or(
			d.requireBuffer(source, gfx.BufferUsageFlagCopySrc, "the copy's source"),
			d.requireBuffer(destination, gfx.BufferUsageFlagCopyDst, "the copy's destination"),
			requireAligned(sourceOffset, copyBufferAlignment, "the copy's source offset"),
			requireAligned(destinationOffset, copyBufferAlignment, "the copy's destination offset"),
			requireAligned(size, copyBufferAlignment, "the copy's size"),
		); err != nil {
			return err
		}
		copiedFrom, err := d.requireRange(source, sourceOffset, size, "the copy's source")
		if err != nil {
			return err
		}
		copiedTo, err := d.requireRange(destination, destinationOffset, size, "the copy's destination")
		if err != nil {
			return err
		}
		encoder.bufferEffects = append(encoder.bufferEffects, func() { copy(copiedTo, copiedFrom) })
		return nil
	})
	encoder.record(CommandKindCopyBufferToBuffer, CopyBufferToBufferArguments{
		Source:            source,
		SourceOffset:      sourceOffset,
		Destination:       destination,
		DestinationOffset: destinationOffset,
		Size:              size,
	})
}

// CopyBufferToTexture records the copy, its regions copied, from a buffer
// with CopySrc into a texture with CopyDst, each region's buffer rows a
// multiple of copyBytesPerRowAlignment apart.
func (encoder *commandEncoder) CopyBufferToTexture(source gfx.Buffer, destination gfx.Texture, regions []gfx.BufferTextureCopy) {
	encoder.check(func(d *device) error {
		return cmp.Or(
			d.requireBuffer(source, gfx.BufferUsageFlagCopySrc, "the copy's source"),
			d.requireTexture(destination, gfx.TextureUsageFlagCopyDst, "the copy's destination"),
			requireRowsAligned(regions),
		)
	})
	encoder.record(CommandKindCopyBufferToTexture, CopyBufferToTextureArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// CopyTextureToBuffer records the copy, its regions copied, from a texture
// with CopySrc into a buffer with CopyDst, each region's buffer rows a
// multiple of copyBytesPerRowAlignment apart.
func (encoder *commandEncoder) CopyTextureToBuffer(source gfx.Texture, destination gfx.Buffer, regions []gfx.BufferTextureCopy) {
	encoder.check(func(d *device) error {
		return cmp.Or(
			d.requireTexture(source, gfx.TextureUsageFlagCopySrc, "the copy's source"),
			d.requireBuffer(destination, gfx.BufferUsageFlagCopyDst, "the copy's destination"),
			requireRowsAligned(regions),
		)
	})
	encoder.record(CommandKindCopyTextureToBuffer, CopyTextureToBufferArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// CopyTextureToTexture records the copy, its regions copied, from a texture
// with CopySrc into one with CopyDst.
func (encoder *commandEncoder) CopyTextureToTexture(source, destination gfx.Texture, regions []gfx.TextureCopy) {
	encoder.check(func(d *device) error {
		if err := d.requireTexture(source, gfx.TextureUsageFlagCopySrc, "the copy's source"); err != nil {
			return err
		}
		return d.requireTexture(destination, gfx.TextureUsageFlagCopyDst, "the copy's destination")
	})
	encoder.record(CommandKindCopyTextureToTexture, CopyTextureToTextureArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// ClearBuffer records the clear, of a buffer with CopyDst, its offset and
// size multiples of copyBufferAlignment and its range within the buffer.
func (encoder *commandEncoder) ClearBuffer(buffer gfx.Buffer, offset, size uint64) {
	encoder.check(func(d *device) error {
		if err := cmp.Or(
			d.requireBuffer(buffer, gfx.BufferUsageFlagCopyDst, "the cleared buffer"),
			requireAligned(offset, copyBufferAlignment, "the clear's offset"),
			requireAligned(size, copyBufferAlignment, "the clear's size"),
		); err != nil {
			return err
		}
		cleared, err := d.requireRange(buffer, offset, size, "the clear")
		if err != nil {
			return err
		}
		encoder.bufferEffects = append(encoder.bufferEffects, func() { clear(cleared) })
		return nil
	})
	encoder.record(CommandKindClearBuffer, ClearBufferArguments{Buffer: buffer, Offset: offset, Size: size})
}

// Finish records the call, and answers the commands recorded, or the error
// AnswerNext queued for it, or an error when the encoder is already finished
// or discarded, or a command's error, or an error when a pass is still open.
// The encoder is spent either way.
func (encoder *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	err := encoder.device.instance.record(CallKindFinish, nil, func() error {
		if encoder.spent {
			return errors.New("recording: finishing an encoder already finished or discarded")
		}
		if encoder.err == nil && encoder.passOpen {
			return errors.New("recording: finishing an encoder with a pass open")
		}
		return encoder.err
	}).Error
	encoder.spent = true
	if err != nil {
		return nil, err
	}
	return &commandBuffer{commands: encoder.commands, bufferEffects: encoder.bufferEffects}, nil
}

// DiscardEncoding drops the commands recorded; the encoder is spent.
func (encoder *commandEncoder) DiscardEncoding() {
	encoder.commands = nil
	encoder.bufferEffects = nil
	encoder.spent = true
}

// beginPass opens a pass, its pipeline not yet set, failing the encoder when
// one is open already.
func (encoder *commandEncoder) beginPass() {
	if encoder.passOpen {
		encoder.fail(errors.New("recording: a pass begun inside another"))
	}
	encoder.passOpen = true
	encoder.pipelineSet = false
}

// check runs check over the device under the instance's mutex, and fails the
// encoder with its error.
func (encoder *commandEncoder) check(check func(d *device) error) {
	instance := encoder.device.instance
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	encoder.fail(check(encoder.device))
}

// fail keeps err, when not nil, as the encoder's error when it has none yet.
func (encoder *commandEncoder) fail(err error) {
	if encoder.err == nil {
		encoder.err = err
	}
}

// record appends a command of kind with arguments.
func (encoder *commandEncoder) record(kind CommandKind, arguments any) {
	encoder.commands = append(encoder.commands, Command{Kind: kind, Arguments: arguments})
}

// copyBufferAlignment is what a buffer copy's offsets and size, and a clear's
// offset and size, are multiples of, as wgpu's COPY_BUFFER_ALIGNMENT.
const copyBufferAlignment = 4

// copyBytesPerRowAlignment is what the buffer rows of a copy between a buffer
// and a texture are a multiple of apart, as wgpu's
// COPY_BYTES_PER_ROW_ALIGNMENT.
const copyBytesPerRowAlignment = 256

// requireAligned answers an error naming role when value is not a multiple of
// alignment.
func requireAligned(value, alignment uint64, role string) error {
	if value%alignment != 0 {
		return fmt.Errorf("recording: %s %d is not a multiple of %d", role, value, alignment)
	}
	return nil
}

// requireWithin answers an error naming role when the range from offset, size
// long, ends past length.
func requireWithin(offset, size, length uint64, role string) error {
	if offset > length || size > length-offset {
		return fmt.Errorf("recording: %s's range of %d bytes from %d ends past %d", role, size, offset, length)
	}
	return nil
}

// requireRowsAligned answers an error when a region's buffer rows are not a
// multiple of copyBytesPerRowAlignment apart.
func requireRowsAligned(regions []gfx.BufferTextureCopy) error {
	for index, region := range regions {
		if err := requireAligned(uint64(region.BufferLayout.BytesPerRow), copyBytesPerRowAlignment, fmt.Sprintf("region %d's bytes per row", index)); err != nil {
			return err
		}
	}
	return nil
}

// commandBuffer is a finished commandEncoder's commands, as Submit records
// them, and what they do to buffers' bytes, which Submit applies.
type commandBuffer struct {
	commands      []Command
	bufferEffects []func()
}

// Release does nothing: the adapter holds nothing for a buffer.
func (*commandBuffer) Release() {}

// pass is what a render and a compute pass share: the encoder they record
// into, and the index of the command that began them, which holds their
// commands.
type pass struct {
	encoder *commandEncoder
	index   int
}

// SetBindGroup records the bind group, its dynamic offsets copied.
func (p pass) SetBindGroup(index uint32, group gfx.BindGroup, dynamicOffsets []uint32) {
	p.encoder.check(func(d *device) error {
		_, err := requireObject(&d.bindGroups, group, fmt.Sprintf("bind group %d", index))
		return err
	})
	p.record(CommandKindSetBindGroup, SetBindGroupArguments{Index: index, Group: group, DynamicOffsets: slices.Clone(dynamicOffsets)})
}

// End records nothing, the pass's commands being those under its beginning,
// and closes the pass, for the next to begin.
func (p pass) End() {
	p.encoder.passOpen = false
}

// requirePipeline fails the encoder when the pass has no pipeline set for
// command, a draw or a dispatch.
func (p pass) requirePipeline(command string) {
	if !p.encoder.pipelineSet {
		p.encoder.fail(fmt.Errorf("recording: %s with no pipeline set", command))
	}
}

// requireIndirectBuffer checks the buffer an indirect draw or dispatch reads
// its arguments from.
func (p pass) requireIndirectBuffer(buffer gfx.Buffer) {
	p.encoder.check(func(d *device) error { return d.requireBuffer(buffer, gfx.BufferUsageFlagNone, "the indirect buffer") })
}

// record appends a command of kind with arguments under the pass's beginning.
func (p pass) record(kind CommandKind, arguments any) {
	begun := &p.encoder.commands[p.index]
	begun.Commands = append(begun.Commands, Command{Kind: kind, Arguments: arguments})
}

// renderPassEncoder records one render pass under its BeginRenderPass.
type renderPassEncoder struct {
	pass
}

// SetPipeline records the pipeline, set for the pass's draws or dispatches.
func (p renderPassEncoder) SetPipeline(pipeline gfx.RenderPipeline) {
	p.encoder.check(func(d *device) error {
		_, err := requireObject(&d.renderPipelines, pipeline, "the pipeline")
		return err
	})
	p.record(CommandKindSetPipeline, pipeline)
	p.encoder.pipelineSet = true
}

// SetVertexBuffer records the vertex buffer, which needs Vertex.
func (p renderPassEncoder) SetVertexBuffer(slot uint32, buffer gfx.Buffer, offset uint64) {
	p.encoder.check(func(d *device) error {
		return d.requireBuffer(buffer, gfx.BufferUsageFlagVertex, fmt.Sprintf("vertex buffer %d", slot))
	})
	p.record(CommandKindSetVertexBuffer, SetVertexBufferArguments{Slot: slot, Buffer: buffer, Offset: offset})
}

// SetIndexBuffer records the index buffer, which needs Index.
func (p renderPassEncoder) SetIndexBuffer(buffer gfx.Buffer, format gfx.IndexFormat, offset uint64) {
	p.encoder.check(func(d *device) error { return d.requireBuffer(buffer, gfx.BufferUsageFlagIndex, "the index buffer") })
	p.record(CommandKindSetIndexBuffer, SetIndexBufferArguments{Buffer: buffer, Format: format, Offset: offset})
}

// SetViewport records the viewport.
func (p renderPassEncoder) SetViewport(viewport gfx.Viewport) {
	p.record(CommandKindSetViewport, viewport)
}

// SetScissorRect records the scissor rectangle.
func (p renderPassEncoder) SetScissorRect(rectangle gfx.ScissorRect) {
	p.record(CommandKindSetScissorRect, rectangle)
}

// SetBlendConstant records the blend constant.
func (p renderPassEncoder) SetBlendConstant(color gfx.Color) {
	p.record(CommandKindSetBlendConstant, color)
}

// SetStencilReference records the stencil reference.
func (p renderPassEncoder) SetStencilReference(reference uint32) {
	p.record(CommandKindSetStencilReference, reference)
}

// Draw records the draw, after the pass's pipeline.
func (p renderPassEncoder) Draw(arguments gfx.DrawArgs) {
	p.requirePipeline("a draw")
	p.record(CommandKindDraw, arguments)
}

// DrawIndexed records the draw, after the pass's pipeline.
func (p renderPassEncoder) DrawIndexed(arguments gfx.DrawIndexedArgs) {
	p.requirePipeline("an indexed draw")
	p.record(CommandKindDrawIndexed, arguments)
}

// DrawIndirect records the draw, after the pass's pipeline.
func (p renderPassEncoder) DrawIndirect(buffer gfx.Buffer, offset uint64) {
	p.requirePipeline("an indirect draw")
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDrawIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// DrawIndexedIndirect records the draw, after the pass's pipeline.
func (p renderPassEncoder) DrawIndexedIndirect(buffer gfx.Buffer, offset uint64) {
	p.requirePipeline("an indexed indirect draw")
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDrawIndexedIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// MultiDrawIndirect records the draws, after the pass's pipeline.
func (p renderPassEncoder) MultiDrawIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.requirePipeline("indirect draws")
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindMultiDrawIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// MultiDrawIndexedIndirect records the draws, after the pass's pipeline.
func (p renderPassEncoder) MultiDrawIndexedIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.requirePipeline("indexed indirect draws")
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindMultiDrawIndexedIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// computePassEncoder records one compute pass under its BeginComputePass.
type computePassEncoder struct {
	pass
}

// SetPipeline records the pipeline, set for the pass's draws or dispatches.
func (p computePassEncoder) SetPipeline(pipeline gfx.ComputePipeline) {
	p.encoder.check(func(d *device) error {
		_, err := requireObject(&d.computePipelines, pipeline, "the pipeline")
		return err
	})
	p.record(CommandKindSetPipeline, pipeline)
	p.encoder.pipelineSet = true
}

// Dispatch records the dispatch, after the pass's pipeline.
func (p computePassEncoder) Dispatch(x, y, z uint32) {
	p.requirePipeline("a dispatch")
	p.record(CommandKindDispatch, DispatchArguments{X: x, Y: y, Z: z})
}

// DispatchIndirect records the dispatch, after the pass's pipeline.
func (p computePassEncoder) DispatchIndirect(buffer gfx.Buffer, offset uint64) {
	p.requirePipeline("an indirect dispatch")
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDispatchIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// requireQuerySet answers an error naming role for any query set but the
// zero one, which names none: none is kept until the wgpu fork exposes them.
func requireQuerySet(querySet gfx.QuerySet, role string) error {
	if querySet == (gfx.QuerySet{}) {
		return nil
	}
	return fmt.Errorf("recording: %s: no query set at index %d: %w", role, querySet.Index(), gfx.ErrQuerySetsUnavailable)
}
