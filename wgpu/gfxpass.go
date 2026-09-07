package wgpu

import (
	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gfxbDepthKey is a target size: every DepthAuto pass at one size shares one
// depth texture, which is why such a pass must clear depth to start clean.
type gfxbDepthKey struct{ width, height int }

// gfxbViewKey identifies one renderable view of a baked texture.
type gfxbViewKey struct {
	texture    cgfx.TextureID
	mip, layer int
}

// gfxbView is a cached texture view, the ID pass descriptors name it by, and
// the size it renders at.
type gfxbView struct {
	id            cgfx.TextureViewID
	view          *wgpu.TextureView
	width, height int
}

// TextureView returns a renderable view of one mip level of one layer,
// minting and caching it on first use.
func (b *gfxBackend) TextureView(texture cgfx.TextureID, mip, layer int) cgfx.TextureViewID {
	key := gfxbViewKey{texture: texture, mip: mip, layer: layer}
	if cached, ok := b.views[key]; ok {
		return cached.id
	}
	tex, ok := b.bakedTextures[texture]
	if !ok {
		return 0
	}
	desc := b.bakedTextureDescs[texture]
	view, err := b.device.CreateTextureView(tex.tex, &wgpu.TextureViewDescriptor{
		Label: "gfx.target", Format: textureFormat(desc.Format),
		Dimension: gputypes.TextureViewDimension2D, Aspect: gputypes.TextureAspectAll,
		BaseMipLevel: uint32(mip), MipLevelCount: 1,
		BaseArrayLayer: uint32(layer), ArrayLayerCount: 1,
	})
	if err != nil {
		return 0
	}
	cached := &gfxbView{
		id:     cgfx.TextureViewID(b.id()),
		view:   view,
		width:  max(desc.Width>>mip, 1),
		height: max(desc.Height>>mip, 1),
	}
	b.views[key] = cached
	b.viewID[cached.id] = cached
	return cached.id
}

// releaseTextureViews drops the cached views of a texture that is being
// replaced or released, since they point at the old native texture.
func (b *gfxBackend) releaseTextureViews(texture cgfx.TextureID) {
	for key, view := range b.views {
		if key.texture != texture {
			continue
		}
		view.view.Release()
		delete(b.views, key)
		delete(b.viewID, view.id)
	}
}

// BeginPass encodes one pass's attachments into the frame's encoder and returns
// the RenderPass its commands go to.
func (b *gfxBackend) BeginPass(desc cgfx.GpuPassDesc) cgfx.RenderPass {
	if b.encoder == nil {
		return nil
	}
	if !depthOnlyPassSupported && isDepthOnly(desc) {
		// One refusal is permanent - it is a property of the HAL, not of this
		// pass - so it is recorded once and reported once. See gfxdepthonly.go
		// for what the backend cannot encode and why.
		if !b.refusedDepthOnly {
			b.refusedDepthOnly = true
			b.refusal = ErrDepthOnlyPassUnsupported{Pass: desc.Label}
		}
		return nil
	}
	colour, width, height := b.passColour(desc)
	depth := b.passDepth(desc, width, height)
	if colour == nil {
		// Either there is nothing to render at all, or the pass declares a
		// colour target whose view does not exist yet - which every temporary
		// target does on its first frame, since its allocation is a bake this
		// same Execute replays after the descriptors were built. Both are
		// skipped, and skipping is not optional for the second: a descriptor
		// carrying only a depth attachment is the shape the HAL never begins
		// and then faults ending, so opening one here is a segfault rather than
		// a wasted pass. isDepthOnly above has already reported the case that
		// asked for it deliberately.
		return nil
	}
	pass := &wgpu.RenderPassDescriptor{Label: desc.Label}
	if colour != nil {
		clear := gputypes.Color{
			R: float64(desc.Clear.R), G: float64(desc.Clear.G),
			B: float64(desc.Clear.B), A: float64(desc.Clear.A),
		}
		pass.ColorAttachments = []wgpu.RenderPassColorAttachment{{
			View: colour, LoadOp: loadOp(desc.Load), StoreOp: storeOp(desc.Store), ClearValue: clear,
		}}
	}
	if depth != nil {
		pass.DepthStencilAttachment = &wgpu.RenderPassDepthStencilAttachment{
			View:        depth,
			DepthLoadOp: loadOp(desc.DepthLoad), DepthStoreOp: storeOp(desc.DepthStore),
			DepthClearValue: desc.DepthClear,
			// Depth32Float has no stencil aspect, and StencilReadOnly is what
			// keeps the browser binding from emitting stencil load/store ops.
			StencilReadOnly: true,
		}
	}
	encoded, err := b.encoder.BeginRenderPass(pass)
	if err != nil {
		return nil
	}
	// Neither pending entries nor bound groups survive a pass boundary.
	b.resetAcc()
	b.resetBound()
	return &gfxRenderPass{backend: b, pass: encoded}
}

// TransitionTextures places the barriers gfx computed for the pass about to be
// encoded. It is called outside any render pass, because that is the only place
// a barrier can be recorded.
//
// gogpu tracks resources for lifetime and for submit-time validation but
// derives no barriers from that tracking, so a texture written as an attachment
// and then sampled is not ordered against those writes - not within one command
// encoder, and not across a submit boundary. On Vulkan the sample reads the
// image mid-write. The symptom is a flicker that looks random, is not a CPU/GPU
// race, and is invisible to any capture taken while the app redraws at speed,
// so the barrier has to ship with the feature rather than after it.
//
// Cost is one image transition per write-then-read pair, independent of scene
// complexity - unlike Device.WaitIdle, which also removes the artifact but
// stalls the CPU on the GPU to do it.
func (b *gfxBackend) TransitionTextures(transitions []cgfx.TextureTransition) {
	if b.encoder == nil || len(transitions) == 0 {
		return
	}
	b.barriers = b.barriers[:0]
	for _, transition := range transitions {
		texture, ok := b.bakedTextures[transition.Texture]
		if !ok || texture == nil {
			continue
		}
		desc := b.bakedTextureDescs[transition.Texture]
		levels := uint32(1)
		if desc.Mipmaps && mipmapsSupported(desc.Format) {
			levels = uint32(mipLevelCount(desc.Width, desc.Height))
		}
		b.barriers = append(b.barriers, wgpu.TextureBarrier{
			Texture: texture.tex,
			// The whole texture, not the one mip and layer the pass rendered
			// into: a sampler reads every level, so ordering only the written
			// one leaves the rest of the read unordered.
			Range: wgpu.TextureRange{
				Aspect:        gputypes.TextureAspectAll,
				MipLevelCount: levels, ArrayLayerCount: uint32(max(desc.Layers, 1)),
			},
			Usage: wgpu.TextureUsageTransition{
				OldUsage: textureBarrierUsage(transition.From),
				NewUsage: textureBarrierUsage(transition.To),
			},
		})
	}
	if len(b.barriers) > 0 {
		b.encoder.TransitionTextures(b.barriers)
	}
}

// textureBarrierUsage maps gfx's two attachment roles onto the backend's usage
// flags. gfx names only the two it can put a texture in; every other usage is
// the backend's own business.
func textureBarrierUsage(usage cgfx.TextureUsage) gputypes.TextureUsage {
	if usage == cgfx.TextureUsageTextureBinding {
		return gputypes.TextureUsageTextureBinding
	}
	return gputypes.TextureUsageRenderAttachment
}

// EndPass closes the pass BeginPass opened.
func (b *gfxBackend) EndPass(pass cgfx.RenderPass) {
	if encoded, ok := pass.(*gfxRenderPass); ok && encoded != nil {
		_ = encoded.pass.End()
	}
}

// passColour resolves a pass's colour attachment and the size it renders at.
func (b *gfxBackend) passColour(desc cgfx.GpuPassDesc) (*wgpu.TextureView, int, int) {
	if desc.Screen {
		// A screen pass renders into the frame buffer, never into the surface:
		// only the present pass touches that. This is also where the buffer is
		// allocated, so first use is what pays for it.
		frame := b.frameBuffer()
		if frame == nil {
			return nil, 0, 0
		}
		return frame.view, b.frameW, b.frameH
	}
	if view, ok := b.viewID[desc.Target]; ok {
		return view.view, view.width, view.height
	}
	return nil, 0, 0
}

// passDepth resolves a pass's depth attachment, allocating the shared DepthAuto
// texture for the target's size when it does not exist yet.
func (b *gfxBackend) passDepth(desc cgfx.GpuPassDesc, width, height int) *wgpu.TextureView {
	if desc.DepthAuto {
		return b.autoDepth(width, height)
	}
	if view, ok := b.viewID[desc.Depth]; ok {
		return view.view
	}
	return nil
}

// autoDepth returns the depth texture shared by every DepthAuto pass at a size.
func (b *gfxBackend) autoDepth(width, height int) *wgpu.TextureView {
	if width <= 0 || height <= 0 {
		return nil
	}
	key := gfxbDepthKey{width: width, height: height}
	if depth, ok := b.depths[key]; ok {
		return depth.view
	}
	depth, err := b.newTexture(cgfx.TextureDesc{
		Width: width, Height: height, Layers: 1,
		Format: cgfx.FormatDepth32F, Renderable: true, Label: "gfx.depth",
	})
	if err != nil {
		return nil
	}
	b.depths[key] = depth
	return depth.view
}

// releaseDepths frees every shared depth texture, which the surface being
// resized or the backend shutting down makes stale.
func (b *gfxBackend) releaseDepths() {
	for key, depth := range b.depths {
		b.freeTexture(depth)
		delete(b.depths, key)
	}
}

func loadOp(op cgfx.LoadOp) gputypes.LoadOp {
	// WebGPU has no "don't care" load, and clearing is the legal way to say the
	// previous contents are not read.
	if op == cgfx.LoadPreserve {
		return gputypes.LoadOpLoad
	}
	return gputypes.LoadOpClear
}

func storeOp(op cgfx.StoreOp) gputypes.StoreOp {
	if op == cgfx.StoreDiscard {
		return gputypes.StoreOpDiscard
	}
	return gputypes.StoreOpStore
}
