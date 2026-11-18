package canvas

import (
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material and the white texel
// shapes sample, made at canvas's first draw, and what each draw reuses from
// the frame before, so a steady frame allocates nothing: the instance data
// and the batches.
type renderer struct {
	// defaultMaterial and whiteTexel are zero until made.
	defaultMaterial render.Material
	whiteTexel      render.Texture
	// instances are the frame's instance data, in sort order, and batches
	// the frame's batches, the last drawnBatch; both are written over each frame.
	instances []instanceData
	batches   []batch
	// colorTargets is the default camera's pass's one target.
	colorTargets [1]render.ColorTarget
}

// init makes the default material, over the default shader, which it keeps
// acquired, blending premultiplied colour over what the target holds, and
// the white texel, a 1×1 layer of opaque white.
func (r *renderer) init(inventory render.Inventory) {
	over := render.BlendComponent{SourceFactor: render.BlendFactorOne, DestinationFactor: render.BlendFactorOneMinusSourceAlpha}
	var state render.State
	state.ColorTargets[0].Blend = render.BlendState{Color: over, Alpha: over}
	r.defaultMaterial = inventory.MakeMaterial(render.MaterialDescriptor{
		Label:            "canvas default",
		Shader:           inventory.AcquireShader(defaultShaderPath),
		VertexEntryPoint: spriteVertexEntryPoint,
		State:            state,
	})
	r.whiteTexel = inventory.MakeTexture(render.TextureDescriptor{Label: "canvas white texel", Width: 1, Height: 1, Format: render.TextureFormatRGBA8Unorm}, []byte{255, 255, 255, 255})
}

// drawDefaultCamera draws shapes, fills each, through the default camera
// into window, the main window, while it is open and sized, and answers
// whether it drew: one command list at the default camera's order, of one
// pass into window, cleared to opaque black, in it entries, the shapes' in
// sort order, an instanced draw of each batch over the white texel, an
// instance each. It makes the default material and the texel at the first
// draw. With no shape, or no main window, it begins no list.
func (r *renderer) drawDefaultCamera(frame render.Frame, inventory render.Inventory, manager *platform.WindowManager, window platform.Window, shapes []Shape, entries []sortedEntry) bool {
	if len(shapes) == 0 || window == (platform.Window{}) {
		return false
	}
	size := manager.Size(window)
	framebufferSize := manager.FramebufferSize(window)
	if size.Width <= 0 || size.Height <= 0 || framebufferSize.Width <= 0 || framebufferSize.Height <= 0 {
		return false
	}
	if r.defaultMaterial == (render.Material{}) {
		r.init(inventory)
	}
	list := frame.BeginCommandList("canvas", defaultCameraOrder)
	r.batches, r.instances = makeBatches(r.batches, r.instances, entries, shapes)
	instances := list.AcquireTemporaryBuffer(r.instances)
	r.colorTargets[0] = render.ColorTarget{Window: window, Clear: true, ClearColor: render.Color{A: 1}}
	pass := list.BeginPass(render.PassDescriptor{Label: "canvas default camera", ColorTargets: r.colorTargets[:]})
	pass.SetPassUniform("canvas_camera", makeDefaultCameraBlock(size, framebufferSize))
	for _, drawnBatch := range r.batches {
		// The default material is the only one canvas makes yet. Setting a
		// material resets the draw parameters, so each batch sets its own.
		pass.SetMaterial(r.defaultMaterial)
		pass.SetBuffer("canvas_instances", instances)
		pass.SetTexture("canvas_atlas", r.whiteTexel)
		pass.SetSampler("canvas_sampler", render.SamplerDescriptor{})
		pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: drawnBatch.instanceCount, FirstInstance: drawnBatch.firstInstance})
	}
	pass.End()
	list.End()
	return true
}
