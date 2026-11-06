package recording

import (
	"errors"
	"slices"

	"github.com/dvoyni/cog/gfx"
)

// commandEncoder is the recording adapter's encoder: it keeps the commands
// recorded into it, for Finish. One goroutine records into it at a time, as
// gfx.CommandEncoder says, so it needs no mutex.
type commandEncoder struct {
	instance *Instance
	commands []Command
	// spent is set once the encoder is finished or discarded: wgpu answers
	// ErrReleased for a Finish after either.
	spent bool
}

// BeginRenderPass records the pass, its color attachments copied, and
// answers its encoder, which records under it.
func (encoder *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	descriptor.ColorAttachments = slices.Clone(descriptor.ColorAttachments)
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

// CopyBufferToBuffer records the copy.
func (encoder *commandEncoder) CopyBufferToBuffer(source gfx.Buffer, sourceOffset uint64, destination gfx.Buffer, destinationOffset uint64, size uint64) {
	encoder.record(CommandKindCopyBufferToBuffer, CopyBufferToBufferArguments{
		Source:            source,
		SourceOffset:      sourceOffset,
		Destination:       destination,
		DestinationOffset: destinationOffset,
		Size:              size,
	})
}

// CopyBufferToTexture records the copy, its regions copied.
func (encoder *commandEncoder) CopyBufferToTexture(source gfx.Buffer, destination gfx.Texture, regions []gfx.BufferTextureCopy) {
	encoder.record(CommandKindCopyBufferToTexture, CopyBufferToTextureArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// CopyTextureToBuffer records the copy, its regions copied.
func (encoder *commandEncoder) CopyTextureToBuffer(source gfx.Texture, destination gfx.Buffer, regions []gfx.BufferTextureCopy) {
	encoder.record(CommandKindCopyTextureToBuffer, CopyTextureToBufferArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// CopyTextureToTexture records the copy, its regions copied.
func (encoder *commandEncoder) CopyTextureToTexture(source, destination gfx.Texture, regions []gfx.TextureCopy) {
	encoder.record(CommandKindCopyTextureToTexture, CopyTextureToTextureArguments{Source: source, Destination: destination, Regions: slices.Clone(regions)})
}

// ClearBuffer records the clear.
func (encoder *commandEncoder) ClearBuffer(buffer gfx.Buffer, offset, size uint64) {
	encoder.record(CommandKindClearBuffer, ClearBufferArguments{Buffer: buffer, Offset: offset, Size: size})
}

// Finish records the call, and answers the commands recorded, or the error
// AnswerNext queued for it, or an error when the encoder is already finished
// or discarded. The encoder is spent either way.
func (encoder *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	err := encoder.instance.record(CallKindFinish, nil, func() error {
		if encoder.spent {
			return errors.New("recording: finishing an encoder already finished or discarded")
		}
		return nil
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
	p.record(CommandKindSetBindGroup, SetBindGroupArguments{Index: index, Group: group, DynamicOffsets: slices.Clone(dynamicOffsets)})
}

// End records nothing: the pass's commands are those under its beginning.
func (pass) End() {}

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
	p.record(CommandKindSetPipeline, pipeline)
}

// SetVertexBuffer records the vertex buffer.
func (p renderPassEncoder) SetVertexBuffer(slot uint32, buffer gfx.Buffer, offset uint64) {
	p.record(CommandKindSetVertexBuffer, SetVertexBufferArguments{Slot: slot, Buffer: buffer, Offset: offset})
}

// SetIndexBuffer records the index buffer.
func (p renderPassEncoder) SetIndexBuffer(buffer gfx.Buffer, format gfx.IndexFormat, offset uint64) {
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
	p.record(CommandKindDrawIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// DrawIndexedIndirect records the draw.
func (p renderPassEncoder) DrawIndexedIndirect(buffer gfx.Buffer, offset uint64) {
	p.record(CommandKindDrawIndexedIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}

// MultiDrawIndirect records the draws.
func (p renderPassEncoder) MultiDrawIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.record(CommandKindMultiDrawIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// MultiDrawIndexedIndirect records the draws.
func (p renderPassEncoder) MultiDrawIndexedIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	p.record(CommandKindMultiDrawIndexedIndirect, MultiDrawIndirectArguments{Buffer: buffer, Offset: offset, DrawCount: drawCount})
}

// computePassEncoder records one compute pass under its BeginComputePass.
type computePassEncoder struct {
	pass
}

// SetPipeline records the pipeline.
func (p computePassEncoder) SetPipeline(pipeline gfx.ComputePipeline) {
	p.record(CommandKindSetPipeline, pipeline)
}

// Dispatch records the dispatch.
func (p computePassEncoder) Dispatch(x, y, z uint32) {
	p.record(CommandKindDispatch, DispatchArguments{X: x, Y: y, Z: z})
}

// DispatchIndirect records the dispatch.
func (p computePassEncoder) DispatchIndirect(buffer gfx.Buffer, offset uint64) {
	p.record(CommandKindDispatchIndirect, IndirectArguments{Buffer: buffer, Offset: offset})
}
