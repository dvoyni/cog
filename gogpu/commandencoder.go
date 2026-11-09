package gogpu

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
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

func (e *commandEncoder) BeginComputePass(gfx.ComputePassDescriptor) gfx.ComputePassEncoder {
	e.fail(unbuilt("BeginComputePass"))
	return computePassEncoder{}
}

func (e *commandEncoder) ResolveQuerySet(gfx.QuerySet, uint32, uint32, gfx.Buffer, uint64) {
	e.fail(unbuilt("ResolveQuerySet"))
}

func (e *commandEncoder) CopyBufferToBuffer(gfx.Buffer, uint64, gfx.Buffer, uint64, uint64) {
	e.fail(unbuilt("CopyBufferToBuffer"))
}

func (e *commandEncoder) CopyBufferToTexture(gfx.Buffer, gfx.Texture, []gfx.BufferTextureCopy) {
	e.fail(unbuilt("CopyBufferToTexture"))
}

func (e *commandEncoder) CopyTextureToBuffer(gfx.Texture, gfx.Buffer, []gfx.BufferTextureCopy) {
	e.fail(unbuilt("CopyTextureToBuffer"))
}

func (e *commandEncoder) CopyTextureToTexture(gfx.Texture, gfx.Texture, []gfx.TextureCopy) {
	e.fail(unbuilt("CopyTextureToTexture"))
}

func (e *commandEncoder) ClearBuffer(gfx.Buffer, uint64, uint64) { e.fail(unbuilt("ClearBuffer")) }

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

// fail keeps err, unless nil, as the encoder's error when it has none yet.
func (e *commandEncoder) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// renderPassEncoder is wgpu's render pass as gfx's; pass is nil when its
// encoder had an error at its begin. Its commands are not built yet: each
// records an error naming it.
type renderPassEncoder struct {
	encoder *commandEncoder
	pass    *wgpu.RenderPassEncoder
}

func (p renderPassEncoder) SetPipeline(gfx.RenderPipeline) { p.encoder.fail(unbuilt("SetPipeline")) }
func (p renderPassEncoder) SetBindGroup(uint32, gfx.BindGroup, []uint32) {
	p.encoder.fail(unbuilt("SetBindGroup"))
}
func (p renderPassEncoder) SetVertexBuffer(uint32, gfx.Buffer, uint64) {
	p.encoder.fail(unbuilt("SetVertexBuffer"))
}
func (p renderPassEncoder) SetIndexBuffer(gfx.Buffer, gfx.IndexFormat, uint64) {
	p.encoder.fail(unbuilt("SetIndexBuffer"))
}
func (p renderPassEncoder) SetViewport(gfx.Viewport) { p.encoder.fail(unbuilt("SetViewport")) }
func (p renderPassEncoder) SetScissorRect(gfx.ScissorRect) {
	p.encoder.fail(unbuilt("SetScissorRect"))
}
func (p renderPassEncoder) SetBlendConstant(gfx.Color) { p.encoder.fail(unbuilt("SetBlendConstant")) }
func (p renderPassEncoder) SetStencilReference(uint32) {
	p.encoder.fail(unbuilt("SetStencilReference"))
}
func (p renderPassEncoder) Draw(gfx.DrawArgs)               { p.encoder.fail(unbuilt("Draw")) }
func (p renderPassEncoder) DrawIndexed(gfx.DrawIndexedArgs) { p.encoder.fail(unbuilt("DrawIndexed")) }
func (p renderPassEncoder) DrawIndirect(gfx.Buffer, uint64) {
	p.encoder.fail(unbuilt("DrawIndirect"))
}
func (p renderPassEncoder) DrawIndexedIndirect(gfx.Buffer, uint64) {
	p.encoder.fail(unbuilt("DrawIndexedIndirect"))
}
func (p renderPassEncoder) MultiDrawIndirect(gfx.Buffer, uint64, uint32) {
	p.encoder.fail(unbuilt("MultiDrawIndirect"))
}
func (p renderPassEncoder) MultiDrawIndexedIndirect(gfx.Buffer, uint64, uint32) {
	p.encoder.fail(unbuilt("MultiDrawIndexedIndirect"))
}

// End ends wgpu's pass, when it began, keeping its error as the encoder's.
func (p renderPassEncoder) End() {
	if p.pass != nil {
		p.encoder.fail(p.pass.End())
	}
}

// computePassEncoder is a compute pass, not built yet: its encoder has the
// error, and it records nothing.
type computePassEncoder struct{}

func (computePassEncoder) SetPipeline(gfx.ComputePipeline)              {}
func (computePassEncoder) SetBindGroup(uint32, gfx.BindGroup, []uint32) {}
func (computePassEncoder) Dispatch(uint32, uint32, uint32)              {}
func (computePassEncoder) DispatchIndirect(gfx.Buffer, uint64)          {}
func (computePassEncoder) End()                                         {}
