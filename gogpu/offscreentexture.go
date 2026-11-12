package gogpu

import (
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// offscreenTexture is a captured frame's offscreen texture, which it is drawn
// into, since gfx copies the capture out of it and gogpu offers no surface
// texture, and which its present draws onto the window, sampling it through
// its view and bind group. It is made at the frame's acquire, at the size the
// window's surface is configured with, and destroyed at its present.
type offscreenTexture struct {
	texture   *wgpu.Texture
	view      *wgpu.TextureView
	bindGroup *wgpu.BindGroup
}

// destroy releases offscreen's texture, view and bind group; wgpu keeps them
// while a submit using them lives.
func (offscreen *offscreenTexture) destroy() {
	offscreen.bindGroup.Release()
	offscreen.view.Release()
	offscreen.texture.Release()
}

// newOffscreenTexture answers an offscreen texture of size, with its view
// and bind group, making first the pipeline drawing one onto a window when
// it is not made yet. A failure answers wgpu's error, and makes nothing.
func (d *device) newOffscreenTexture(size wgpu.Extent3D) (*offscreenTexture, error) {
	if d.offscreenPipeline == nil {
		if err := d.createOffscreenPipeline(); err != nil {
			return nil, err
		}
	}
	texture, err := d.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         offscreenLabel,
		Size:          size,
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gputypes.TextureDimension2D,
		Format:        gputypes.TextureFormatBGRA8Unorm,
		Usage:         gputypes.TextureUsageRenderAttachment | gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopySrc,
	})
	if err != nil {
		return nil, err
	}
	view, err := d.device.CreateTextureView(texture, nil)
	if err != nil {
		texture.Release()
		return nil, err
	}
	bindGroup, err := d.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:   offscreenLabel,
		Layout:  d.offscreenGroupLayout,
		Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}},
	})
	if err != nil {
		view.Release()
		texture.Release()
		return nil, err
	}
	return &offscreenTexture{texture: texture, view: view, bindGroup: bindGroup}, nil
}

// drawOffscreenTexture draws offscreen onto surfaceView, texel for texel,
// with one triangle covering it, and submits it, after gfx's submit of the
// frame drawn into offscreen and copied out of it. wgpu places the barrier
// between that copy and this sample, from the texture's last use.
func (d *device) drawOffscreenTexture(offscreen *offscreenTexture, surfaceView *wgpu.TextureView) error {
	encoder, err := d.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: offscreenLabel})
	if err != nil {
		return err
	}
	pass, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		Label: offscreenLabel,
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View: surfaceView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore,
		}},
	})
	if err != nil {
		encoder.DiscardEncoding()
		return err
	}
	pass.SetPipeline(d.offscreenPipeline)
	pass.SetBindGroup(0, offscreen.bindGroup, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	if err := pass.End(); err != nil {
		encoder.DiscardEncoding()
		return err
	}
	commandBuffer, err := encoder.Finish()
	if err != nil {
		return err
	}
	_, err = d.device.Queue().Submit(commandBuffer)
	return err
}

// createOffscreenPipeline creates the pipeline drawing an offscreen texture
// onto a window's surface, and the layout of the bind groups it samples one
// through. Its pipeline layout is kept for the pipeline, which binds through
// it, and all three go at the device's Release; its shader module is released
// once the pipeline is made. A failure releases what it created, and answers
// wgpu's error.
func (d *device) createOffscreenPipeline() error {
	module, err := d.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: offscreenLabel, WGSL: offscreenWGSL})
	if err != nil {
		return err
	}
	defer module.Release()
	groupLayout, err := d.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: offscreenLabel,
		Entries: []gputypes.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gputypes.ShaderStageFragment,
			Texture:    &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D},
		}},
	})
	if err != nil {
		return err
	}
	layout, err := d.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            offscreenLabel,
		BindGroupLayouts: []*wgpu.BindGroupLayout{groupLayout},
	})
	if err != nil {
		groupLayout.Release()
		return err
	}
	pipeline, err := d.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:       offscreenLabel,
		Layout:      layout,
		Vertex:      wgpu.VertexState{Module: module, EntryPoint: "cover"},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList},
		Multisample: gputypes.MultisampleState{Count: 1, Mask: ^uint64(0)},
		Fragment: &wgpu.FragmentState{Module: module, EntryPoint: "load", Targets: []gputypes.ColorTargetState{{
			Format: gputypes.TextureFormatBGRA8Unorm, WriteMask: gputypes.ColorWriteMaskAll,
		}}},
	})
	if err != nil {
		layout.Release()
		groupLayout.Release()
		return err
	}
	d.offscreenGroupLayout = groupLayout
	d.offscreenPipelineLayout = layout
	d.offscreenPipeline = pipeline
	return nil
}

// offscreenLabel labels the objects drawing an offscreen texture onto a
// window.
const offscreenLabel = "gogpu: offscreen"

// offscreenWGSL draws a texture onto a surface of its size, texel for texel,
// with one triangle covering the surface.
const offscreenWGSL = `
@group(0) @binding(0) var frame: texture_2d<f32>;

@vertex
fn cover(@builtin(vertex_index) index: u32) -> @builtin(position) vec4f {
	return vec4f(f32(index & 1u) * 4 - 1, f32(index >> 1u) * 4 - 1, 0, 1);
}

@fragment
fn load(@builtin(position) position: vec4f) -> @location(0) vec4f {
	return textureLoad(frame, vec2i(position.xy), 0);
}
`
