package wgpu

import (
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// The frame buffer and the pass that shows it.
//
// ScreenTarget does not mean the swapchain: it means a frame-sized colour
// buffer this backend owns, which every screen pass renders into and which one
// implicit full-screen pass then copies to the surface. The engine has no
// choice about that. gogpu hardcodes BGRA8Unorm for the surface and exposes no
// view formats on either path, and bgra8unorm-srgb is not a legal
// canvas-context format on the web, so a hardware sRGB swapchain is
// unreachable and owning the buffer is the only way the engine gets a say over
// the colour space at all. The cost - about 8 MiB at 1080p and one full-screen
// pass - is also exactly the hook post-processing needs later.

// presentSource is the present pass's shader for a frame buffer in the given
// format: a full-screen triangle that samples the buffer and writes it to the
// swapchain.
//
// The transfer function follows from the format and nothing else. Sampling an
// sRGB texture decodes it, so an sRGB frame buffer arrives here as light and
// has to be encoded again for the unorm swapchain, which is the sRGB OETF. A
// plain unorm frame buffer arrives exactly as it was written and is passed
// through, because encoding it here would encode a second time what the
// recorders already gamma-encoded themselves.
func presentSource(format cgfx.TextureFormat) string {
	const header = `
@group(0) @binding(0) var frameTexture: texture_2d<f32>;
@group(0) @binding(1) var frameSampler: sampler;

struct Vertex {
    @builtin(position) position: vec4<f32>,
    @location(0) uv: vec2<f32>,
};

// One triangle big enough to cover the surface, so the present pass needs no
// vertex buffer and has no seam down the middle of the screen.
@vertex
fn vs_main(@builtin(vertex_index) index: u32) -> Vertex {
    let uv = vec2<f32>(f32((index << 1u) & 2u), f32(index & 2u));
    var out: Vertex;
    out.position = vec4<f32>(uv * vec2<f32>(2.0, -2.0) + vec2<f32>(-1.0, 1.0), 0.0, 1.0);
    out.uv = uv;
    return out;
}
`
	const passthrough = `
@fragment
fn fs_main(in: Vertex) -> @location(0) vec4<f32> {
    return textureSample(frameTexture, frameSampler, in.uv);
}
`
	// Alpha is coverage, not light, so the OETF applies to the colour channels
	// only - the same split the mip filter makes.
	const encode = `
@fragment
fn fs_main(in: Vertex) -> @location(0) vec4<f32> {
    let light = textureSample(frameTexture, frameSampler, in.uv);
    let low = light.rgb * 12.92;
    let high = 1.055 * pow(light.rgb, vec3<f32>(1.0 / 2.4)) - 0.055;
    let encoded = select(high, low, light.rgb <= vec3<f32>(0.0031308));
    return vec4<f32>(encoded, light.a);
}
`
	if format.Resolve() == cgfx.FormatRGBA8Srgb {
		return header + encode
	}
	return header + passthrough
}

// frameBuffer returns the buffer every screen pass renders into, allocating it
// at the surface's size on first use. Nothing allocates it up front: a frame
// that renders only into its own textures never asks for it and never pays for
// it.
func (b *gfxBackend) frameBuffer() *gfxbTexture {
	if b.frame != nil {
		return b.frame
	}
	if b.screenW <= 0 || b.screenH <= 0 {
		return nil
	}
	frame, err := b.newTexture(cgfx.TextureDesc{
		Width: b.screenW, Height: b.screenH, Layers: 1,
		Format: cgfx.FrameBufferFormat, Renderable: true, Label: "gfx.framebuffer",
	})
	if err != nil {
		return nil
	}
	b.frame, b.frameW, b.frameH = frame, b.screenW, b.screenH
	return b.frame
}

// releaseFrameBuffer drops the frame buffer and the bind group that names it,
// which the surface resizing makes stale. The next screen pass allocates one
// at the new size.
func (b *gfxBackend) releaseFrameBuffer() {
	if b.presentBind != nil {
		b.presentBind.Release()
		b.presentBind = nil
	}
	if b.frame != nil {
		b.freeTexture(b.frame)
		b.frame, b.frameW, b.frameH = nil, 0, 0
	}
}

// Present encodes the frame's implicit present pass: a full-screen triangle
// that samples the frame buffer into the surface. This is the only place in
// the engine that names the swapchain's own format, which is why the rest of
// it can be built against the frame buffer's.
func (b *gfxBackend) Present() {
	if b.encoder == nil || b.frame == nil || b.screenView == nil {
		return
	}
	// The screen passes wrote the frame buffer as a colour attachment and this
	// pass samples it. gogpu tracks resources for lifetime and for submit-time
	// validation but derives no barriers from that tracking, so nothing orders
	// this sample against those writes - not within one encoder, and not across
	// a submit boundary either. On Vulkan the sample then reads the image while
	// it is still being written, which shows as tiles of a half-drawn frame and
	// is invisible to any capture taken while the app is redrawing.
	//
	// This is not a rule about the present pass: every pass in this engine that
	// samples what an earlier pass rendered into has to place its own
	// transition, so post-processing and scene render targets will each need
	// one. Cost is a single image transition per frame, independent of scene
	// complexity - unlike Device.WaitIdle, which also removes the artifact but
	// stalls the CPU on the GPU and gives up a frame of overlap to do it.
	b.encoder.TransitionTextures([]wgpu.TextureBarrier{{
		Texture: b.frame.tex,
		Range: wgpu.TextureRange{
			Aspect: gputypes.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1,
		},
		Usage: wgpu.TextureUsageTransition{
			OldUsage: gputypes.TextureUsageRenderAttachment,
			NewUsage: gputypes.TextureUsageTextureBinding,
		},
	}})
	pipeline := b.presentPipeline()
	if pipeline == nil {
		return
	}
	bind := b.presentBindGroup()
	if bind == nil {
		return
	}
	pass, err := b.encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		Label: "gfx.present",
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			// The triangle covers every pixel, so there is nothing worth
			// reading back in first.
			View: b.screenView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore,
		}},
	})
	if err != nil {
		return
	}
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, bind, nil)
	pass.Draw(3, 1, 0, 0)
	_ = pass.End()
	// This pass binds outside the per-draw path, so what the draws thought was
	// bound no longer holds.
	b.resetAcc()
	b.resetBound()
}

// presentPipeline builds the present pipeline once and keeps it: it has no
// vertex buffer, no depth attachment and no blending, and it is built for the
// surface format rather than the frame buffer's.
//
// The module and both layouts are kept for the backend's lifetime, exactly as
// buildShaderLayouts keeps a shader's. Binding a group reads the layout off the
// pipeline that is currently set, so releasing the pipeline layout after
// building the pipeline hands vkCmdBindDescriptorSets a destroyed handle - it
// survives a quiet frame and faults once anything else recycles it.
func (b *gfxBackend) presentPipeline() *wgpu.RenderPipeline {
	if b.present != nil || b.presentFailed {
		return b.present
	}
	// One failure is permanent - the source and the layouts are constants - so
	// it is recorded rather than retried every frame.
	b.presentFailed = true
	module, err := b.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "gfx.present", WGSL: presentSource(cgfx.FrameBufferFormat),
	})
	if err != nil {
		return nil
	}
	b.presentModule = module
	b.presentGroupLayout, err = b.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "gfx.present",
		Entries: []gputypes.BindGroupLayoutEntry{
			{
				Binding: 0, Visibility: gputypes.ShaderStageFragment,
				Texture: &gputypes.TextureBindingLayout{
					SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D,
				},
			},
			{
				Binding: 1, Visibility: gputypes.ShaderStageFragment,
				Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering},
			},
		},
	})
	if err != nil {
		return nil
	}
	b.presentPipeLayout, err = b.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "gfx.present", BindGroupLayouts: []*wgpu.BindGroupLayout{b.presentGroupLayout},
	})
	if err != nil {
		return nil
	}
	pipeline, err := b.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "gfx.present",
		Layout: b.presentPipeLayout,
		Vertex: wgpu.VertexState{Module: module, EntryPoint: "vs_main"},
		Primitive: gputypes.PrimitiveState{
			Topology: gputypes.PrimitiveTopologyTriangleList,
			CullMode: gputypes.CullModeNone, FrontFace: gputypes.FrontFaceCCW,
		},
		Fragment: &wgpu.FragmentState{
			Module: module, EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{
				Format: b.surfaceFormat, WriteMask: gputypes.ColorWriteMaskAll,
			}},
		},
	})
	if err != nil {
		return nil
	}
	b.present, b.presentFailed = pipeline, false
	return b.present
}

// presentBindGroup binds the current frame buffer to the present pipeline,
// rebuilt whenever the buffer is, which is whenever the surface resizes.
func (b *gfxBackend) presentBindGroup() *wgpu.BindGroup {
	if b.presentBind != nil {
		return b.presentBind
	}
	if b.frame == nil || b.presentGroupLayout == nil {
		return nil
	}
	bind, err := b.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "gfx.present",
		Layout: b.presentGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, TextureView: b.frame.view},
			{Binding: 1, Sampler: b.defaultSampler},
		},
	})
	if err != nil {
		return nil
	}
	b.presentBind = bind
	return b.presentBind
}
