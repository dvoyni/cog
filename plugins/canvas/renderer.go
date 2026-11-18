package canvas

import (
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// renderer is canvas's private state for drawing a frame's entries into
// render, and records them: the default material and the white texel
// shapes sample, made at canvas's first draw, and what each draw reuses from
// the frame before, so a steady frame allocates nothing.
type renderer struct {
	// defaultMaterial and whiteTexel are zero until made.
	defaultMaterial render.Material
	whiteTexel      render.Texture
	// instances are the frame's instance data, written over each frame.
	instances []instanceData
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

// recordDefaultCamera records into list the default camera's pass into
// window, cleared to opaque black, with camera as its pass parameters, and
// in it shapes, fills each, as one instanced draw of the default material
// over the white texel, an instance each.
func (r *renderer) recordDefaultCamera(list *render.CommandList, window platform.Window, camera cameraBlock, shapes []Shape) {
	r.instances = r.instances[:0]
	for _, shape := range shapes {
		r.instances = append(r.instances, makeShapeInstanceData(shape))
	}
	instances := list.AcquireTemporaryBuffer(r.instances)
	r.colorTargets[0] = render.ColorTarget{Window: window, Clear: true, ClearColor: render.Color{A: 1}}
	pass := list.BeginPass(render.PassDescriptor{Label: "canvas default camera", ColorTargets: r.colorTargets[:]})
	pass.SetPassUniform("canvas_camera", camera)
	pass.SetMaterial(r.defaultMaterial)
	pass.SetBuffer("canvas_instances", instances)
	pass.SetTexture("canvas_atlas", r.whiteTexel)
	pass.SetSampler("canvas_sampler", render.SamplerDescriptor{})
	pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: uint32(len(r.instances))})
	pass.End()
}
