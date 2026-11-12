package gogpu

import (
	"fmt"
	"sync/atomic"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel/container"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// commandEncoder is wgpu's command encoder as gfx's. It keeps the first
// error recorded, wgpu's or a command's not built yet, and Finish answers
// it; once it has one, no pass it begins reaches wgpu.
type commandEncoder struct {
	device  *device
	encoder *wgpu.CommandEncoder
	err     error
}

// BeginRenderPass begins wgpu's render pass on the views stored at the
// attachments' indices. An attachment's view holding no object, a zero color
// view among them, is an error, and so are timestamp writes, not built yet.
func (e *commandEncoder) BeginRenderPass(descriptor gfx.RenderPassDescriptor) gfx.RenderPassEncoder {
	if descriptor.TimestampWrites != (gfx.PassTimestampWrites{}) {
		e.fail(unbuilt("a render pass's TimestampWrites"))
	}
	colorAttachments := make([]wgpu.RenderPassColorAttachment, len(descriptor.ColorAttachments))
	for index, attachment := range descriptor.ColorAttachments {
		colorAttachments[index] = wgpu.RenderPassColorAttachment{
			View:       e.attachmentView(attachment.View, "color attachment"),
			LoadOp:     gputypes.LoadOp(attachment.LoadOp),
			StoreOp:    gputypes.StoreOp(attachment.StoreOp),
			ClearValue: gputypes.Color(attachment.ClearValue),
		}
		if attachment.ResolveTarget != (gfx.TextureView{}) {
			colorAttachments[index].ResolveTarget = e.attachmentView(attachment.ResolveTarget, "resolve target")
		}
	}
	var depthStencilAttachment *wgpu.RenderPassDepthStencilAttachment
	if attachment := descriptor.DepthStencilAttachment; attachment.View != (gfx.TextureView{}) {
		depthStencilAttachment = &wgpu.RenderPassDepthStencilAttachment{
			View:              e.attachmentView(attachment.View, "depth-stencil attachment"),
			DepthLoadOp:       gputypes.LoadOp(attachment.DepthLoadOp),
			DepthStoreOp:      gputypes.StoreOp(attachment.DepthStoreOp),
			DepthClearValue:   attachment.DepthClearValue,
			DepthReadOnly:     attachment.DepthReadOnly,
			StencilLoadOp:     gputypes.LoadOp(attachment.StencilLoadOp),
			StencilStoreOp:    gputypes.StoreOp(attachment.StencilStoreOp),
			StencilClearValue: attachment.StencilClearValue,
			StencilReadOnly:   attachment.StencilReadOnly,
		}
	}
	if e.err != nil {
		return renderPassEncoder{encoder: e}
	}
	pass, err := e.encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		Label:                  descriptor.Label,
		ColorAttachments:       colorAttachments,
		DepthStencilAttachment: depthStencilAttachment,
	})
	e.fail(err)
	return renderPassEncoder{encoder: e, pass: pass}
}

// BeginComputePass begins wgpu's compute pass. Timestamp writes are an
// error, not built yet.
func (e *commandEncoder) BeginComputePass(descriptor gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	if descriptor.TimestampWrites != (gfx.PassTimestampWrites{}) {
		e.fail(unbuilt("a compute pass's TimestampWrites"))
	}
	if e.err != nil {
		return computePassEncoder{encoder: e}
	}
	pass, err := e.encoder.BeginComputePass(&wgpu.ComputePassDescriptor{Label: descriptor.Label})
	e.fail(err)
	return computePassEncoder{encoder: e, pass: pass}
}

func (e *commandEncoder) ResolveQuerySet(gfx.QuerySet, uint32, uint32, gfx.Buffer, uint64) {
	e.fail(unbuilt("ResolveQuerySet"))
}

// CopyBufferToBuffer records wgpu's copy between the buffers stored at
// source's and destination's indexes.
func (e *commandEncoder) CopyBufferToBuffer(source gfx.Buffer, sourceOffset uint64, destination gfx.Buffer, destinationOffset uint64, size uint64) {
	copied := e.recordedBuffer(source)
	written := e.recordedBuffer(destination)
	if e.err == nil {
		e.encoder.CopyBufferToBuffer(copied, sourceOffset, written, destinationOffset, size)
	}
}

// CopyBufferToTexture records wgpu's copy of regions of the buffer stored at
// source's index into the texture at destination's. Each region copies into
// destination, whatever texture its TextureBase names.
func (e *commandEncoder) CopyBufferToTexture(source gfx.Buffer, destination gfx.Texture, regions []gfx.BufferTextureCopy) {
	copied := e.recordedBuffer(source)
	written := recordedObject(e, &e.device.textures, destination, "texture")
	if e.err == nil {
		e.encoder.CopyBufferToTexture(copied, written, makeBufferTextureCopies(written, regions))
	}
}

// CopyTextureToBuffer records wgpu's copy of regions of the texture stored
// at source's index into the buffer at destination's. Each region copies
// from source, whatever texture its TextureBase names.
func (e *commandEncoder) CopyTextureToBuffer(source gfx.Texture, destination gfx.Buffer, regions []gfx.BufferTextureCopy) {
	copied := recordedObject(e, &e.device.textures, source, "texture")
	written := e.recordedBuffer(destination)
	if e.err == nil {
		e.encoder.CopyTextureToBuffer(copied, written, makeBufferTextureCopies(copied, regions))
	}
}

// CopyTextureToTexture records wgpu's copy of regions between the textures
// stored at source's and destination's indexes. Each region copies from
// source into destination, whatever textures it names.
func (e *commandEncoder) CopyTextureToTexture(source, destination gfx.Texture, regions []gfx.TextureCopy) {
	copied := recordedObject(e, &e.device.textures, source, "texture")
	written := recordedObject(e, &e.device.textures, destination, "texture")
	if e.err != nil {
		return
	}
	copies := make([]wgpu.TextureCopy, len(regions))
	for index, region := range regions {
		copies[index] = wgpu.TextureCopy{
			Source:      makeImageCopyTexture(copied, region.Source),
			Destination: makeImageCopyTexture(written, region.Destination),
			Size:        wgpu.Extent3D(region.Size),
		}
	}
	e.encoder.CopyTextureToTexture(copied, written, copies)
}

// ClearBuffer records wgpu's clear of the buffer stored at buffer's index.
func (e *commandEncoder) ClearBuffer(buffer gfx.Buffer, offset, size uint64) {
	if cleared := e.recordedBuffer(buffer); cleared != nil {
		e.encoder.ClearBuffer(cleared, offset, size)
	}
}

// Finish answers wgpu's command buffer, or the first error recorded, after
// discarding the encoding.
func (e *commandEncoder) Finish() (gfx.CommandBuffer, error) {
	if e.err != nil {
		e.encoder.DiscardEncoding()
		return nil, e.err
	}
	buffer, err := e.encoder.Finish()
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

func (e *commandEncoder) DiscardEncoding() { e.encoder.DiscardEncoding() }

// attachmentView answers the texture view stored at view's index, recording
// an error, naming role, for the zero view and one holding none.
func (e *commandEncoder) attachmentView(view gfx.TextureView, role string) *wgpu.TextureView {
	var textureView *wgpu.TextureView
	if slot := e.device.textureViews.At(view.Index()); slot != nil && view != (gfx.TextureView{}) {
		textureView = slot.Load()
	}
	if textureView == nil {
		e.fail(fmt.Errorf("gogpu: a render pass's %s, %v, holds no texture view", role, view))
	}
	return textureView
}

// recordedBuffer answers the buffer stored at buffer's index for a command,
// or nil once the encoder has an error, recording one when it holds none.
func (e *commandEncoder) recordedBuffer(buffer gfx.Buffer) *wgpu.Buffer {
	stored, err := e.device.storedBuffer(buffer)
	e.fail(err)
	if e.err != nil {
		return nil
	}
	return stored
}

// fail keeps err, unless nil, as the encoder's error when it has none yet.
func (e *commandEncoder) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// renderPassEncoder is wgpu's render pass as gfx's; pass is nil when its
// encoder had an error at its begin.
type renderPassEncoder struct {
	encoder *commandEncoder
	pass    *wgpu.RenderPassEncoder
}

// SetPipeline sets the pipeline stored at pipeline's index.
func (p renderPassEncoder) SetPipeline(pipeline gfx.RenderPipeline) {
	if stored := recordedObject(p.encoder, &p.encoder.device.renderPipelines, pipeline, "render pipeline"); stored != nil {
		p.pass.SetPipeline(stored)
	}
}

// SetBindGroup sets the bind group stored at group's index.
func (p renderPassEncoder) SetBindGroup(index uint32, group gfx.BindGroup, dynamicOffsets []uint32) {
	if stored := recordedObject(p.encoder, &p.encoder.device.bindGroups, group, "bind group"); stored != nil {
		p.pass.SetBindGroup(index, stored, dynamicOffsets)
	}
}

// SetVertexBuffer sets the buffer stored at buffer's index in slot.
func (p renderPassEncoder) SetVertexBuffer(slot uint32, buffer gfx.Buffer, offset uint64) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.SetVertexBuffer(slot, stored, offset)
	}
}

// SetIndexBuffer sets the buffer stored at buffer's index as the index
// buffer.
func (p renderPassEncoder) SetIndexBuffer(buffer gfx.Buffer, format gfx.IndexFormat, offset uint64) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.SetIndexBuffer(stored, gputypes.IndexFormat(format), offset)
	}
}

func (p renderPassEncoder) SetViewport(viewport gfx.Viewport) {
	if p.encoder.err == nil {
		p.pass.SetViewport(gputypes.Viewport(viewport))
	}
}

func (p renderPassEncoder) SetScissorRect(rectangle gfx.ScissorRect) {
	if p.encoder.err == nil {
		p.pass.SetScissorRect(gputypes.ScissorRect(rectangle))
	}
}

func (p renderPassEncoder) SetBlendConstant(color gfx.Color) {
	if p.encoder.err == nil {
		constant := gputypes.Color(color)
		p.pass.SetBlendConstant(&constant)
	}
}

func (p renderPassEncoder) SetStencilReference(reference uint32) {
	if p.encoder.err == nil {
		p.pass.SetStencilReference(reference)
	}
}

func (p renderPassEncoder) Draw(arguments gfx.DrawArgs) {
	if p.encoder.err == nil {
		p.pass.Draw(gputypes.DrawArgs(arguments))
	}
}

func (p renderPassEncoder) DrawIndexed(arguments gfx.DrawIndexedArgs) {
	if p.encoder.err == nil {
		p.pass.DrawIndexed(gputypes.DrawIndexedArgs(arguments))
	}
}

// DrawIndirect draws with the arguments at offset in the buffer stored at
// buffer's index, as the other indirect draws do.
func (p renderPassEncoder) DrawIndirect(buffer gfx.Buffer, offset uint64) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.DrawIndirect(stored, offset)
	}
}

func (p renderPassEncoder) DrawIndexedIndirect(buffer gfx.Buffer, offset uint64) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.DrawIndexedIndirect(stored, offset)
	}
}

func (p renderPassEncoder) MultiDrawIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.MultiDrawIndirect(stored, offset, drawCount)
	}
}

func (p renderPassEncoder) MultiDrawIndexedIndirect(buffer gfx.Buffer, offset uint64, drawCount uint32) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.MultiDrawIndexedIndirect(stored, offset, drawCount)
	}
}

// End ends wgpu's pass, when it began, keeping its error as the encoder's.
func (p renderPassEncoder) End() {
	if p.pass != nil {
		p.encoder.fail(p.pass.End())
	}
}

// computePassEncoder is wgpu's compute pass as gfx's; pass is nil when its
// encoder had an error at its begin.
type computePassEncoder struct {
	encoder *commandEncoder
	pass    *wgpu.ComputePassEncoder
}

// SetPipeline sets the pipeline stored at pipeline's index.
func (p computePassEncoder) SetPipeline(pipeline gfx.ComputePipeline) {
	if stored := recordedObject(p.encoder, &p.encoder.device.computePipelines, pipeline, "compute pipeline"); stored != nil {
		p.pass.SetPipeline(stored)
	}
}

// SetBindGroup sets the bind group stored at group's index.
func (p computePassEncoder) SetBindGroup(index uint32, group gfx.BindGroup, dynamicOffsets []uint32) {
	if stored := recordedObject(p.encoder, &p.encoder.device.bindGroups, group, "bind group"); stored != nil {
		p.pass.SetBindGroup(index, stored, dynamicOffsets)
	}
}

func (p computePassEncoder) Dispatch(x, y, z uint32) {
	if p.encoder.err == nil {
		p.pass.Dispatch(x, y, z)
	}
}

// DispatchIndirect dispatches with the workgroup counts at offset in the
// buffer stored at buffer's index.
func (p computePassEncoder) DispatchIndirect(buffer gfx.Buffer, offset uint64) {
	if stored := p.encoder.recordedBuffer(buffer); stored != nil {
		p.pass.DispatchIndirect(stored, offset)
	}
}

// End ends wgpu's pass, when it began, keeping its error as the encoder's.
func (p computePassEncoder) End() {
	if p.pass != nil {
		p.encoder.fail(p.pass.End())
	}
}

// recordedObject answers the object stored in objects at handle's index for
// a command, or nil once the encoder has an error, recording one naming the
// object when it holds none. A command records nothing once its encoder has
// an error, its pass not begun among them.
func recordedObject[T any](e *commandEncoder, objects *container.ObjectTable[atomic.Pointer[T]], handle interface{ Index() uint32 }, object string) *T {
	stored, err := storedObject(objects, handle, object)
	e.fail(err)
	if e.err != nil {
		return nil
	}
	return stored
}

// makeBufferTextureCopies answers regions as wgpu's, each copying texture.
func makeBufferTextureCopies(texture *wgpu.Texture, regions []gfx.BufferTextureCopy) []wgpu.BufferTextureCopy {
	copies := make([]wgpu.BufferTextureCopy, len(regions))
	for index, region := range regions {
		copies[index] = wgpu.BufferTextureCopy{
			BufferLayout: wgpu.ImageDataLayout(region.BufferLayout),
			TextureBase:  makeImageCopyTexture(texture, region.TextureBase),
			Size:         wgpu.Extent3D(region.Size),
		}
	}
	return copies
}

// makeImageCopyTexture answers location as wgpu's, in texture.
func makeImageCopyTexture(texture *wgpu.Texture, location gfx.ImageCopyTexture) wgpu.ImageCopyTexture {
	return wgpu.ImageCopyTexture{
		Texture:  texture,
		MipLevel: location.MipLevel,
		Origin:   wgpu.Origin3D(location.Origin),
		Aspect:   gputypes.TextureAspect(location.Aspect),
	}
}
