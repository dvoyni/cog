package gfx

// CommandEncoder records commands: passes, copies and clears. One goroutine
// records into it at a time. Its errors are deferred: a frame encoder's go to
// LogErr at the flush, a standalone one's come back from Finish.
type CommandEncoder interface {
	// BeginRenderPass begins a render pass; record into it until its End.
	BeginRenderPass(descriptor RenderPassDescriptor) RenderPassEncoder
	// BeginComputePass begins a compute pass; record into it until its End.
	BeginComputePass(descriptor ComputePassDescriptor) ComputePassEncoder
	// ResolveQuerySet writes queryCount results of querySet, from
	// firstQuery, into destination at destinationOffset, a multiple of 256,
	// as uint64 values; a QueryResolve buffer copied to a MapRead one is
	// mapped to read them.
	ResolveQuerySet(querySet QuerySet, firstQuery, queryCount uint32, destination Buffer, destinationOffset uint64)
	// CopyBufferToBuffer copies size bytes between two buffers.
	CopyBufferToBuffer(source Buffer, sourceOffset uint64, destination Buffer, destinationOffset uint64, size uint64)
	// CopyBufferToTexture copies regions of a buffer into a texture.
	CopyBufferToTexture(source Buffer, destination Texture, regions []BufferTextureCopy)
	// CopyTextureToBuffer copies regions of a texture into a buffer.
	CopyTextureToBuffer(source Texture, destination Buffer, regions []BufferTextureCopy)
	// CopyTextureToTexture copies regions between two textures.
	CopyTextureToTexture(source, destination Texture, regions []TextureCopy)
	// ClearBuffer zeroes size bytes of buffer from offset.
	ClearBuffer(buffer Buffer, offset, size uint64)
}

// StandaloneCommandEncoder is a CommandEncoder from
// Device.CreateCommandEncoder, outside any frame: its owner finishes it and
// submits the result with Device.Submit.
type StandaloneCommandEncoder interface {
	CommandEncoder
	// Finish ends recording and answers the commands, or the first error
	// recorded.
	Finish() (CommandBuffer, error)
	// DiscardEncoding throws the recording away; the encoder is spent.
	DiscardEncoding()
}

// CommandBuffer is a finished recording, for Device.Submit. One never
// submitted is released.
type CommandBuffer interface {
	// Release frees a command buffer that will not be submitted.
	Release()
}

// RenderPassEncoder records one render pass. Its draw-time errors are
// deferred to the encoder.
type RenderPassEncoder interface {
	SetPipeline(pipeline RenderPipeline)
	SetBindGroup(index uint32, group BindGroup, dynamicOffsets []uint32)
	SetVertexBuffer(slot uint32, buffer Buffer, offset uint64)
	SetIndexBuffer(buffer Buffer, format IndexFormat, offset uint64)
	SetViewport(viewport Viewport)
	SetScissorRect(rectangle ScissorRect)
	SetBlendConstant(color Color)
	SetStencilReference(reference uint32)
	Draw(arguments DrawArgs)
	DrawIndexed(arguments DrawIndexedArgs)
	DrawIndirect(buffer Buffer, offset uint64)
	DrawIndexedIndirect(buffer Buffer, offset uint64)
	MultiDrawIndirect(buffer Buffer, offset uint64, drawCount uint32)
	MultiDrawIndexedIndirect(buffer Buffer, offset uint64, drawCount uint32)
	// End ends the pass. Its error is the encoder's, not returned here.
	End()
}

// ComputePassEncoder records one compute pass.
type ComputePassEncoder interface {
	SetPipeline(pipeline ComputePipeline)
	SetBindGroup(index uint32, group BindGroup, dynamicOffsets []uint32)
	Dispatch(x, y, z uint32)
	DispatchIndirect(buffer Buffer, offset uint64)
	// End ends the pass. Its error is the encoder's, not returned here.
	End()
}
