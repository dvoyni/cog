package recording

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// commandEncoder is the recording adapter's encoder: it keeps the commands
// recorded into it, for Finish. One goroutine records into it at a time, as
// gfx.CommandEncoder says, so it needs no mutex of its own.
//
// A command naming a handle missing at its index, or a resource without the
// usage the command puts it to, is recorded all the same, and Finish answers
// the first such error, as wgpu does; a zero handle names none.
type commandEncoder struct {
	device   *device
	commands []Command
	// err is the first command's error, for Finish.
	err error
	// spent is set once the encoder is finished or discarded: wgpu answers
	// ErrReleased for a Finish after either.
	spent bool
}

// BeginRenderPass records the pass, its color attachments copied, and
// answers its encoder, which records under it. Each attachment's view needs
// RenderAttachment.
func (encoder *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	descriptor.ColorAttachments = slices.Clone(descriptor.ColorAttachments)
	encoder.check(func(d *device) error {
		for index, attachment := range descriptor.ColorAttachments {
			if err := d.requireView(attachment.View, gfx.TextureUsageFlagRenderAttachment, fmt.Sprintf("color attachment %d", index)); err != nil {
				return err
			}
			if err := d.requireView(attachment.ResolveTarget, gfx.TextureUsageFlagRenderAttachment, fmt.Sprintf("color attachment %d's resolve target", index)); err != nil {
				return err
			}
		}
		return d.requireView(descriptor.DepthStencilAttachment.View, gfx.TextureUsageFlagRenderAttachment, "the depth-stencil attachment")
	})
	encoder.record(CommandKindBeginRenderPass, descriptor)
	return renderPassEncoder{pass{encoder: encoder, index: len(encoder.commands) - 1}}
}

// BeginComputePass records the pass and answers its encoder, which records
// under it.
func (encoder *commandEncoder) BeginComputePass(descriptor gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	encoder.record(CommandKindBeginComputePass, descriptor)
	return computePassEncoder{pass{encoder: encoder, index: len(encoder.commands) - 1}}
}

func (encoder *commandEncoder) ResolveQuerySet(querySet gfx.QuerySet, firstQuery, queryCount uint32, destination gfx.Buffer, destinationOffset uint64) {
	panic("gfx listing: no bodies")
}

// CopyBufferToBuffer records the copy, from a buffer with CopySrc into one
// with CopyDst.
func (encoder *commandEncoder) CopyBufferToBuffer(source gfx.Buffer, sourceOffset uint64, destination gfx.Buffer, destinationOffset uint64, size uint64) {
	encoder.check(func(d *device) error {
		if err := d.requireBuffer(source, gfx.BufferUsageFlagCopySrc, "the copy's source"); err != nil {
			return err
		}
		return d.requireBuffer(destination, gfx.BufferUsageFlagCopyDst, "the copy's destination")
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
// with CopySrc into a texture with CopyDst.
func (encoder *commandEncoder) CopyBufferToTexture(source gfx.Buffer, destination gfx.Texture, regions []gfx.BufferTextureCopy) {
	encoder.check(func(d *device) error {
		if err := d.requireBuffer(source, gfx.BufferUsageFlagCopySrc, "the copy's source"); err != nil {
			return err
		}
		return d.requireTexture(destination, gfx.TextureUsageFlagCopyDst, "the copy's destination")
	})
	encoder.record(CommandKindCopyBufferToTexture, CopyBufferToTextureArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// CopyTextureToBuffer records the copy, its regions copied, from a texture
// with CopySrc into a buffer with CopyDst.
func (encoder *commandEncoder) CopyTextureToBuffer(source gfx.Texture, destination gfx.Buffer, regions []gfx.BufferTextureCopy) {
	encoder.check(func(d *device) error {
		if err := d.requireTexture(source, gfx.TextureUsageFlagCopySrc, "the copy's source"); err != nil {
			return err
		}
		return d.requireBuffer(destination, gfx.BufferUsageFlagCopyDst, "the copy's destination")
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

// ClearBuffer records the clear, of a buffer with CopyDst.
func (encoder *commandEncoder) ClearBuffer(buffer gfx.Buffer, offset, size uint64) {
	encoder.check(func(d *device) error {
		return d.requireBuffer(buffer, gfx.BufferUsageFlagCopyDst, "the cleared buffer")
	})
	encoder.record(CommandKindClearBuffer, ClearBufferArguments{Buffer: buffer, Offset: offset, Size: size})
}

// Finish records the call, and answers the commands recorded, or the error
// AnswerNext queued for it, or an error when the encoder is already finished
// or discarded, or a command's error. The encoder is spent either way.
func (encoder *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	err := encoder.device.instance.record(CallKindFinish, nil, func() error {
		if encoder.spent {
			return errors.New("recording: finishing an encoder already finished or discarded")
		}
		return encoder.err
	}).Error
	encoder.spent = true
	if err != nil {
		return nil, err
	}
	return &commandBuffer{commands: encoder.commands}, nil
}

// DiscardEncoding drops the commands recorded; the encoder is spent.
func (encoder *commandEncoder) DiscardEncoding() {
	encoder.commands = nil
	encoder.spent = true
}

// check runs check over the device under the instance's mutex, keeping its
// error when the encoder has none yet.
func (encoder *commandEncoder) check(check func(d *device) error) {
	instance := encoder.device.instance
	instance.mutex.Lock()
	defer instance.mutex.Unlock()
	if err := check(encoder.device); err != nil && encoder.err == nil {
		encoder.err = err
	}
}

// record appends a command of kind with arguments.
func (encoder *commandEncoder) record(kind CommandKind, arguments any) {
	encoder.commands = append(encoder.commands, Command{Kind: kind, Arguments: arguments})
}

// commandBuffer is a finished commandEncoder's commands, as Submit records
// them.
type commandBuffer struct {
	commands []Command
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

// End records nothing: the pass's commands are those under its beginning.
func (pass) End() {}

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

// SetPipeline records the pipeline.
func (p renderPassEncoder) SetPipeline(pipeline gfx.RenderPipeline) {
	p.encoder.check(func(d *device) error {
		_, err := requireObject(&d.renderPipelines, pipeline, "the pipeline")
		return err
	})
	p.record(CommandKindSetPipeline, pipeline)
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

// Draw records the draw.
func (p renderPassEncoder) Draw(arguments gfx.DrawArgs) {
	p.record(CommandKindDraw, arguments)
}

// DrawIndexed records the draw.
func (p renderPassEncoder) DrawIndexed(arguments gfx.DrawIndexedArgs) {
	p.record(CommandKindDrawIndexed, arguments)
}

// DrawIndirect records the draw.
func (p renderPassEncoder) DrawIndirect(buffer gfx.Buffer, offset uint64) {
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDrawIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// DrawIndexedIndirect records the draw.
func (p renderPassEncoder) DrawIndexedIndirect(buffer gfx.Buffer, offset uint64) {
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDrawIndexedIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// MultiDrawIndirect records the draws.
func (p renderPassEncoder) MultiDrawIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindMultiDrawIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// MultiDrawIndexedIndirect records the draws.
func (p renderPassEncoder) MultiDrawIndexedIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindMultiDrawIndexedIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// computePassEncoder records one compute pass under its BeginComputePass.
type computePassEncoder struct {
	pass
}

// SetPipeline records the pipeline.
func (p computePassEncoder) SetPipeline(pipeline gfx.ComputePipeline) {
	p.encoder.check(func(d *device) error {
		_, err := requireObject(&d.computePipelines, pipeline, "the pipeline")
		return err
	})
	p.record(CommandKindSetPipeline, pipeline)
}

// Dispatch records the dispatch.
func (p computePassEncoder) Dispatch(x, y, z uint32) {
	p.record(CommandKindDispatch, DispatchArguments{X: x, Y: y, Z: z})
}

// DispatchIndirect records the dispatch.
func (p computePassEncoder) DispatchIndirect(buffer gfx.Buffer, offset uint64) {
	p.requireIndirectBuffer(buffer)
	p.record(CommandKindDispatchIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}
