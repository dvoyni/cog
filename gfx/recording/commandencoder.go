package recording

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// commandEncoder is the recording adapter's encoder: it keeps the commands
// recorded into it, for Finish. One goroutine records into it at a time, as
// gfx.CommandEncoder says, so it needs no mutex of its own.
//
// A command naming a handle missing at its index, a resource without the
// usage the command puts it to, a misaligned copy or clear or one ending past
// its buffer, or a broken pass sequence is recorded all the same, and Finish answers the first such error,
// as wgpu does; a zero handle names none. Once its command buffer is
// submitted, the device answers it again from CreateCommandEncoder.
type commandEncoder struct {
	device *device
	// label is the label the encoder was made with, for its command buffer.
	label    string
	commands []Command
	// bufferEffects are what the copies, clears, resolves and timestamp
	// writes recorded do to buffers' and query sets' bytes, applied in order
	// at the submit.
	bufferEffects []bufferEffect
	// tickCount is how many commands were recorded, kept or not: the tick,
	// from the encoder's first, of the next.
	tickCount uint64
	// endTimestamp is where the open pass writes its end's timestamp, nil
	// for none.
	endTimestamp []byte
	// err is the first command's error, for Finish.
	err error
	// passOpen is set from a pass's beginning to its End.
	passOpen bool
	// passCommandIndex is the index of the command that began the last pass, which
	// holds its commands when they are kept.
	passCommandIndex int
	// pipelineSet is set once the open pass sets its pipeline.
	pipelineSet bool
	// spent is set once the encoder is finished or discarded: wgpu answers
	// ErrReleased for a Finish after either.
	spent bool
}

// BeginRenderPass records the pass, its color attachments copied, and
// answers its encoder, which records under it. No other pass may be open,
// each attachment's view needs RenderAttachment, and its timestamp writes
// name no query set, or a timestamp one, as beginPass checks.
func (encoder *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	encoder.beginPass(descriptor.TimestampWrites)
	encoder.check(func(d *device) error {
		for index, attachment := range descriptor.ColorAttachments {
			if err := d.requireView(attachment.View, gfx.TextureUsageFlagRenderAttachment, indexedRole{"color attachment %d", index}); err != nil {
				return err
			}
			if err := d.requireView(attachment.ResolveTarget, gfx.TextureUsageFlagRenderAttachment, indexedRole{"color attachment %d's resolve target", index}); err != nil {
				return err
			}
		}
		return d.requireView(descriptor.DepthStencilAttachment.View, gfx.TextureUsageFlagRenderAttachment, "the depth-stencil attachment")
	})
	descriptor.ColorAttachments = encoder.device.instance.cloneKept(descriptor.ColorAttachments)
	encoder.record(CommandKindBeginRenderPass, descriptor)
	return renderPassEncoder{pass{encoder: encoder}}
}

// BeginComputePass records the pass and answers its encoder, which records
// under it. No other pass may be open, and its timestamp writes name no query
// set, or a timestamp one, as beginPass checks.
func (encoder *commandEncoder) BeginComputePass(descriptor gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	encoder.beginPass(descriptor.TimestampWrites)
	encoder.record(CommandKindBeginComputePass, descriptor)
	return computePassEncoder{pass{encoder: encoder}}
}

// ResolveQuerySet records the resolve of queryCount queries of a query set
// kept, from firstQuery, within its count, into a buffer with QueryResolve,
// at an offset that is a multiple of queryResolveAlignment, within the
// buffer. Unless Instance.EnableTimestamps has enabled timestamps, it always
// fails: no query set is kept, and the zero one names none.
func (encoder *commandEncoder) ResolveQuerySet(querySet gfx.QuerySet, firstQuery, queryCount uint32, destination gfx.Buffer, destinationOffset uint64) {
	encoder.check(func(d *device) error {
		object, err := d.requireQuerySet(querySet, "the resolved query set")
		if err != nil {
			return err
		}
		if err := cmp.Or(
			requireWithin(uint64(firstQuery), uint64(queryCount), uint64(object.queryCount), "the resolved queries"),
			d.requireBuffer(destination, gfx.BufferUsageFlagQueryResolve, "the resolve's destination"),
			requireAligned(destinationOffset, queryResolveAlignment, "the resolve's destination offset"),
		); err != nil {
			return err
		}
		resolvedTo, err := d.requireRange(destination, destinationOffset, queryResultSize*uint64(queryCount), "the resolve's destination")
		if err != nil {
			return err
		}
		resolvedFrom := object.queryResults[queryResultSize*uint64(firstQuery) : queryResultSize*uint64(firstQuery+queryCount)]
		encoder.bufferEffects = append(encoder.bufferEffects, bufferEffect{destination: resolvedTo, source: resolvedFrom})
		return nil
	})
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
		encoder.bufferEffects = append(encoder.bufferEffects, bufferEffect{destination: copiedTo, source: copiedFrom})
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
	encoder.record(CommandKindCopyBufferToTexture, CopyBufferToTextureArguments{Source: source, Destination: destination, Regions: encoder.device.instance.cloneKept(regions)})
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
	encoder.record(CommandKindCopyTextureToBuffer, CopyTextureToBufferArguments{Source: source, Destination: destination, Regions: encoder.device.instance.cloneKept(regions)})
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
	encoder.record(CommandKindCopyTextureToTexture, CopyTextureToTextureArguments{Source: source, Destination: destination, Regions: encoder.device.instance.cloneKept(regions)})
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
		encoder.bufferEffects = append(encoder.bufferEffects, bufferEffect{kind: bufferEffectKindClear, destination: cleared})
		return nil
	})
	encoder.record(CommandKindClearBuffer, ClearBufferArguments{Buffer: buffer, Offset: offset, Size: size})
}

// Finish records the call, and answers the commands recorded, or the error
// AnswerNext queued for it, or an error when the encoder is already finished
// or discarded, or a command's error, or an error when a pass is still open.
// The encoder is spent either way.
func (encoder *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	err := encoder.device.instance.record[any](CallKindFinish, nil, func() error {
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
	return commandBuffer{encoder: encoder}, nil
}

// DiscardEncoding drops the commands recorded; the encoder is spent.
func (encoder *commandEncoder) DiscardEncoding() {
	encoder.commands = nil
	encoder.bufferEffects = nil
	encoder.spent = true
}

// beginPass opens a pass, its pipeline not yet set, failing the encoder when
// one is open already, and writes its beginning's timestamp where
// timestampWrites says, its end's to be written at its End. A zero query set
// writes none; any other is a timestamp query set kept, both indices below
// its count and different.
func (encoder *commandEncoder) beginPass(timestampWrites gfx.PassTimestampWrites) {
	if encoder.passOpen {
		encoder.fail(errors.New("recording: a pass begun inside another"))
	}
	encoder.passOpen = true
	encoder.pipelineSet = false
	encoder.passCommandIndex = len(encoder.commands)
	encoder.endTimestamp = nil
	if timestampWrites.QuerySet == (gfx.QuerySet{}) {
		return
	}
	encoder.check(func(d *device) error {
		object, err := d.requireQuerySet(timestampWrites.QuerySet, "the pass's timestamp writes")
		if err != nil {
			return err
		}
		begin := timestampWrites.BeginningOfPassWriteIndex
		end := timestampWrites.EndOfPassWriteIndex
		switch {
		case begin >= object.queryCount || end >= object.queryCount:
			return fmt.Errorf("recording: the pass's timestamp writes at %d and %d, past a query set of %d queries", begin, end, object.queryCount)
		case begin == end:
			return fmt.Errorf("recording: the pass's timestamp writes both at %d", begin)
		}
		encoder.bufferEffects = append(encoder.bufferEffects, bufferEffect{kind: bufferEffectKindTimestamp, destination: object.queryResults[queryResultSize*begin:][:queryResultSize], ticks: encoder.tickCount})
		encoder.endTimestamp = object.queryResults[queryResultSize*end:][:queryResultSize]
		return nil
	})
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

// record appends a command of kind with arguments, unless recording is
// disabled; arguments are made an interface only when kept.
func (encoder *commandEncoder) record[A any](kind CommandKind, arguments A) {
	encoder.tickCount++
	if encoder.device.instance.keepsCalls() {
		encoder.commands = append(encoder.commands, Command{Kind: kind, Arguments: arguments})
	}
}

// copyBufferAlignment is what a buffer copy's offsets and size, and a clear's
// offset and size, are multiples of, as wgpu's COPY_BUFFER_ALIGNMENT.
const copyBufferAlignment = 4

// queryResolveAlignment is what a resolve's destination offset is a
// multiple of, as wgpu's QUERY_RESOLVE_BUFFER_ALIGNMENT.
const queryResolveAlignment = 256

// copyBytesPerRowAlignment is what the buffer rows of a copy between a buffer
// and a texture are a multiple of apart, as wgpu's
// COPY_BYTES_PER_ROW_ALIGNMENT.
const copyBytesPerRowAlignment = 256

// requireAligned answers an error naming role when value is not a multiple of
// alignment.
func requireAligned[R role](value, alignment uint64, role R) error {
	if value%alignment != 0 {
		return fmt.Errorf("recording: %s %d is not a multiple of %d", role, value, alignment)
	}
	return nil
}

// requireWithin answers an error naming role when the range from offset, size
// long, ends past length.
func requireWithin[R role](offset, size, length uint64, role R) error {
	if offset > length || size > length-offset {
		return fmt.Errorf("recording: %s's range of %d bytes from %d ends past %d", role, size, offset, length)
	}
	return nil
}

// requireRowsAligned answers an error when a region's buffer rows are not a
// multiple of copyBytesPerRowAlignment apart.
func requireRowsAligned(regions []gfx.BufferTextureCopy) error {
	for index, region := range regions {
		if err := requireAligned(uint64(region.BufferLayout.BytesPerRow), copyBytesPerRowAlignment, indexedRole{"region %d's bytes per row", index}); err != nil {
			return err
		}
	}
	return nil
}

// commandBuffer is a finished commandEncoder, whose commands Submit records
// and whose buffer effects it applies. It holds the encoder alone, so it is
// made an interface without allocating.
type commandBuffer struct {
	encoder *commandEncoder
}

// Release does nothing: the adapter holds nothing for a buffer.
func (commandBuffer) Release() {}

// pass is what a render and a compute pass share: the encoder they record
// into, under the command that began the last pass. It holds the encoder
// alone, so a pass encoder is made an interface without allocating.
type pass struct {
	encoder *commandEncoder
}

// SetBindGroup records the bind group, its dynamic offsets copied.
func (p pass) SetBindGroup(index uint32, group gfx.BindGroup, dynamicOffsets []uint32) {
	p.encoder.check(func(d *device) error {
		_, err := d.bindGroups.require(group, indexedRole{"bind group %d", int(index)})
		return err
	})
	p.record(CommandKindSetBindGroup, SetBindGroupArguments{Index: index, Group: group, DynamicOffsets: p.encoder.device.instance.cloneKept(dynamicOffsets)})
}

// End records nothing, the pass's commands being those under its beginning,
// and closes the pass, for the next to begin, writing its end's timestamp
// where its beginning said.
func (p pass) End() {
	p.encoder.passOpen = false
	if p.encoder.endTimestamp != nil {
		p.encoder.bufferEffects = append(p.encoder.bufferEffects, bufferEffect{kind: bufferEffectKindTimestamp, destination: p.encoder.endTimestamp, ticks: p.encoder.tickCount})
		p.encoder.endTimestamp = nil
	}
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

// record appends a command of kind with arguments under the pass's
// beginning, when the encoder keeps its commands; arguments are made an
// interface only then.
func (p pass) record[A any](kind CommandKind, arguments A) {
	p.encoder.tickCount++
	if !p.encoder.device.instance.keepsCalls() {
		return
	}
	begun := &p.encoder.commands[p.encoder.passCommandIndex]
	begun.Commands = append(begun.Commands, Command{Kind: kind, Arguments: arguments})
}

// renderPassEncoder records one render pass under its BeginRenderPass.
type renderPassEncoder struct {
	pass
}

// SetPipeline records the pipeline, set for the pass's draws or dispatches.
func (p renderPassEncoder) SetPipeline(pipeline gfx.RenderPipeline) {
	p.encoder.check(func(d *device) error {
		_, err := d.renderPipelines.require(pipeline, "the pipeline")
		return err
	})
	p.record(CommandKindSetPipeline, pipeline)
	p.encoder.pipelineSet = true
}

// SetVertexBuffer records the vertex buffer, which needs Vertex.
func (p renderPassEncoder) SetVertexBuffer(slot uint32, buffer gfx.Buffer, offset uint64) {
	p.encoder.check(func(d *device) error {
		return d.requireBuffer(buffer, gfx.BufferUsageFlagVertex, indexedRole{"vertex buffer %d", int(slot)})
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
		_, err := d.computePipelines.require(pipeline, "the pipeline")
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
