package recording

import "github.com/dvoyni/cog/gfx"

// Command is one recorded encoder or pass command with its arguments, slices
// copied; a pass's commands are under its BeginRenderPass or
// BeginComputePass, in order.
type Command struct {
	Kind      CommandKind
	Arguments any
	Commands  []Command
}

// CommandKind names a kind of encoder or pass command, for the commands a
// CommandBuffer holds.
type CommandKind uint16

const (
	// CommandKindBeginRenderPass is CommandEncoder.BeginRenderPass, its
	// Arguments the gfx.RenderPassDescriptor, its Commands the pass's.
	CommandKindBeginRenderPass CommandKind = iota
	// CommandKindBeginComputePass is CommandEncoder.BeginComputePass, its
	// Arguments the gfx.ComputePassDescriptor, its Commands the pass's.
	CommandKindBeginComputePass
	// CommandKindCopyBufferToBuffer is CommandEncoder.CopyBufferToBuffer, its
	// Arguments a CopyBufferToBufferArguments.
	CommandKindCopyBufferToBuffer
	// CommandKindCopyBufferToTexture is CommandEncoder.CopyBufferToTexture,
	// its Arguments a CopyBufferToTextureArguments.
	CommandKindCopyBufferToTexture
	// CommandKindCopyTextureToBuffer is CommandEncoder.CopyTextureToBuffer,
	// its Arguments a CopyTextureToBufferArguments.
	CommandKindCopyTextureToBuffer
	// CommandKindCopyTextureToTexture is CommandEncoder.CopyTextureToTexture,
	// its Arguments a CopyTextureToTextureArguments.
	CommandKindCopyTextureToTexture
	// CommandKindClearBuffer is CommandEncoder.ClearBuffer, its Arguments a
	// ClearBufferArguments.
	CommandKindClearBuffer
	// CommandKindSetPipeline is a pass's SetPipeline, its Arguments the
	// gfx.RenderPipeline or the gfx.ComputePipeline.
	CommandKindSetPipeline
	// CommandKindSetBindGroup is a pass's SetBindGroup, its Arguments a
	// SetBindGroupArguments.
	CommandKindSetBindGroup
	// CommandKindSetVertexBuffer is RenderPassEncoder.SetVertexBuffer, its
	// Arguments a SetVertexBufferArguments.
	CommandKindSetVertexBuffer
	// CommandKindSetIndexBuffer is RenderPassEncoder.SetIndexBuffer, its
	// Arguments a SetIndexBufferArguments.
	CommandKindSetIndexBuffer
	// CommandKindSetViewport is RenderPassEncoder.SetViewport, its Arguments
	// the gfx.Viewport.
	CommandKindSetViewport
	// CommandKindSetScissorRect is RenderPassEncoder.SetScissorRect, its
	// Arguments the gfx.ScissorRect.
	CommandKindSetScissorRect
	// CommandKindSetBlendConstant is RenderPassEncoder.SetBlendConstant, its
	// Arguments the gfx.Color.
	CommandKindSetBlendConstant
	// CommandKindSetStencilReference is RenderPassEncoder.SetStencilReference,
	// its Arguments the uint32 reference.
	CommandKindSetStencilReference
	// CommandKindDraw is RenderPassEncoder.Draw, its Arguments the
	// gfx.DrawArgs.
	CommandKindDraw
	// CommandKindDrawIndexed is RenderPassEncoder.DrawIndexed, its Arguments
	// the gfx.DrawIndexedArgs.
	CommandKindDrawIndexed
	// CommandKindDrawIndirect is RenderPassEncoder.DrawIndirect, its Arguments
	// an IndirectArguments.
	CommandKindDrawIndirect
	// CommandKindDrawIndexedIndirect is RenderPassEncoder.DrawIndexedIndirect,
	// its Arguments an IndirectArguments.
	CommandKindDrawIndexedIndirect
	// CommandKindMultiDrawIndirect is RenderPassEncoder.MultiDrawIndirect, its
	// Arguments a MultiDrawIndirectArguments.
	CommandKindMultiDrawIndirect
	// CommandKindMultiDrawIndexedIndirect is
	// RenderPassEncoder.MultiDrawIndexedIndirect, its Arguments a
	// MultiDrawIndirectArguments.
	CommandKindMultiDrawIndexedIndirect
	// CommandKindDispatch is ComputePassEncoder.Dispatch, its Arguments a
	// DispatchArguments.
	CommandKindDispatch
	// CommandKindDispatchIndirect is ComputePassEncoder.DispatchIndirect, its
	// Arguments an IndirectArguments.
	CommandKindDispatchIndirect
)

// CopyBufferToBufferArguments are what CommandEncoder.CopyBufferToBuffer was
// called with.
type CopyBufferToBufferArguments struct {
	Source            gfx.Buffer
	SourceOffset      uint64
	Destination       gfx.Buffer
	DestinationOffset uint64
	Size              uint64
}

// CopyBufferToTextureArguments are what CommandEncoder.CopyBufferToTexture
// was called with, the regions copied.
type CopyBufferToTextureArguments struct {
	Source      gfx.Buffer
	Destination gfx.Texture
	Regions     []gfx.BufferTextureCopy
}

// CopyTextureToBufferArguments are what CommandEncoder.CopyTextureToBuffer
// was called with, the regions copied.
type CopyTextureToBufferArguments struct {
	Source      gfx.Texture
	Destination gfx.Buffer
	Regions     []gfx.BufferTextureCopy
}

// CopyTextureToTextureArguments are what CommandEncoder.CopyTextureToTexture
// was called with, the regions copied.
type CopyTextureToTextureArguments struct {
	Source      gfx.Texture
	Destination gfx.Texture
	Regions     []gfx.TextureCopy
}

// ClearBufferArguments are what CommandEncoder.ClearBuffer was called with.
type ClearBufferArguments struct {
	Buffer gfx.Buffer
	Offset uint64
	Size   uint64
}

// SetBindGroupArguments are what a pass's SetBindGroup was called with, the
// dynamic offsets copied.
type SetBindGroupArguments struct {
	Index          uint32
	Group          gfx.BindGroup
	DynamicOffsets []uint32
}

// SetVertexBufferArguments are what RenderPassEncoder.SetVertexBuffer was
// called with.
type SetVertexBufferArguments struct {
	Slot   uint32
	Buffer gfx.Buffer
	Offset uint64
}

// SetIndexBufferArguments are what RenderPassEncoder.SetIndexBuffer was
// called with.
type SetIndexBufferArguments struct {
	Buffer gfx.Buffer
	Format gfx.IndexFormat
	Offset uint64
}

// IndirectArguments are what DrawIndirect, DrawIndexedIndirect or
// DispatchIndirect was called with: the buffer holding the arguments, and
// where in it.
type IndirectArguments struct {
	Buffer gfx.Buffer
	Offset uint64
}

// MultiDrawIndirectArguments are what MultiDrawIndirect or
// MultiDrawIndexedIndirect was called with.
type MultiDrawIndirectArguments struct {
	Buffer    gfx.Buffer
	Offset    uint64
	DrawCount uint32
}

// DispatchArguments are what ComputePassEncoder.Dispatch was called with:
// the workgroup counts.
type DispatchArguments struct {
	X, Y, Z uint32
}
