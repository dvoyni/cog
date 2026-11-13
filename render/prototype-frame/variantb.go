package main

import "prototype.invalid/renderframe/render"

// drawSceneB is drawSceneA in variant B: parameters as values built by
// constructors and passed as lists.
func drawSceneB(frame *render.Frame, assets *render.Assets, s scene, targets frameTargets, number int) {
	list := frame.BeginCommandList("scene", sceneOrder)
	instances := list.AcquireTemporaryBuffer(make([]byte, 500*64))
	pass := list.BeginPass(render.PassDescriptor{
		Label:        "scene",
		ColorTargets: []render.ColorTarget{{Texture: targets.color, Clear: true, ClearColor: render.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}}},
		DepthStencil: render.DepthStencilTarget{Texture: targets.depth, Clear: true, ClearDepth: 1},
	})
	pass.SetPassParameters(
		render.MakeUniformParameter("camera", Camera{}),
		render.MakeBufferParameter("objects", instances),
	)
	pass.SetMaterial(s.crateMaterial)
	pass.Draw(s.crate, render.DrawArgs{InstanceCount: 250})
	assets.SetMaterialParameters(s.crateMaterial,
		render.MakeUniformParameter("surface", Surface{Tint: [4]float32{1, 0.5, 0.5, float32(number)}}),
	)
	pass.Draw(s.crate, render.DrawArgs{InstanceCount: 250, FirstInstance: 250})
	pass.End()
	list.End()
}
