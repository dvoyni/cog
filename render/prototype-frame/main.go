// PROTOTYPE for https://github.com/dvoyni/cog/issues/815: the calls a user of
// render makes to draw one frame. Throwaway, outside go.work.
//
//	GOWORK=off go run .     variant A, a setter per parameter kind
//	GOWORK=off go run . b   variant B, parameters as values
//
// Each function below would be a kernel task of the game's own plugin; the
// kernel is left out, and a render resource the task would reach through
// kernel.Read or kernel.Write arrives as a pointer.
package main

import (
	"os"
	"sync"

	"prototype.invalid/renderframe/render"
)

// Orders the game's plugins agree on, in the one space render's users share.
const (
	cullOrder      render.Order = 200
	sceneOrder     render.Order = 300
	windowOrder    render.Order = 800
	interfaceOrder render.Order = 900
)

const window render.Window = 1

// Camera mirrors the WGSL struct of the scene's pass parameter `camera`.
type Camera struct{ ViewProjection [16]float32 }

// Surface mirrors the WGSL struct of the lit material's `surface` block.
type Surface struct{ Tint [4]float32 }

// Panel mirrors the interface shader's per-draw `panel` block.
type Panel struct{ Rectangle, Color [4]float32 }

// scene is what the game loaded: every handle it holds, and what it must
// release.
type scene struct {
	lit, cull, interface_, present render.Shader
	crateAlbedo                    render.Texture
	crateVertices, crateIndices    render.Buffer
	crate                          render.Mesh
	crateMaterial                  render.Material
	panelMaterial                  render.Material
	presentMaterial                render.Material
}

// frameTargets is the game's own per-frame state: the temporary textures one
// task acquires and the others draw into or sample. In the plugin it would
// be a resource of the game's, written by prepareFrame before the drawing
// tasks run.
type frameTargets struct {
	color, depth render.Texture
}

func main() {
	assets := render.NewAssets()
	loaded := load(assets)
	drawScene := drawSceneA
	if len(os.Args) > 1 && os.Args[1] == "b" {
		drawScene = drawSceneB
	}
	for number := range 2 {
		frame := render.BeginFrame(number, assets)
		targets := prepareFrame(frame)
		var tasks sync.WaitGroup
		tasks.Go(func() { cullObjects(frame, loaded) })
		tasks.Go(func() { drawScene(frame, assets, loaded, targets, number) })
		tasks.Go(func() { drawInterface(frame, loaded) })
		tasks.Go(func() { present(frame, loaded, targets) })
		tasks.Wait()
		frame.Submit()
	}
	unload(assets, loaded)
}

// load is a loading task: every handle exists when its call returns, and the
// data is on the GPU before the frame that asked is submitted.
func load(assets *render.Assets) scene {
	var s scene
	s.lit = assets.AcquireShader("shaders/lit.wgsl", render.Define{Name: "SHADOWS", Value: "0"}, render.Define{Name: "INSTANCED", Value: "1"})
	s.cull = assets.AcquireShader("shaders/cull.wgsl")
	s.interface_ = assets.AcquireShader("shaders/panel.wgsl")
	s.present = assets.AcquireShader("shaders/present.wgsl")
	s.crateAlbedo = assets.AcquireTexture("textures/crate.png", render.TextureOptions{ColorSpace: render.ColorSpaceSRGB, Mipmaps: true})
	s.crateVertices = assets.AcquireBuffer("meshes/crate.vertices")
	s.crateIndices = assets.AcquireBuffer("meshes/crate.indices")
	s.crate = assets.MakeMesh(render.MeshDescriptor{
		Label: "crate",
		VertexBuffers: []render.VertexBuffer{{
			Range:  render.BufferRange{Buffer: s.crateVertices},
			Layout: render.VertexLayout{Stride: 32, Attributes: []string{"position float32x3", "normal float32x3", "uv float32x2"}},
		}},
		Indices:    render.BufferRange{Buffer: s.crateIndices},
		IndexCount: 36,
		Topology:   "triangle list",
	})
	s.crateMaterial = assets.MakeMaterial("crate", s.lit, render.RenderState{DepthTest: "less", DepthWrite: true, Cull: "back"})
	assets.SetMaterialTexture(s.crateMaterial, "albedo", s.crateAlbedo)
	assets.SetMaterialSampler(s.crateMaterial, "albedoSampler", render.SamplerDescriptor{Filter: "linear", Address: "repeat"})
	assets.SetMaterialUniform(s.crateMaterial, "surface", Surface{Tint: [4]float32{1, 1, 1, 1}})
	s.panelMaterial = assets.MakeMaterial("panel", s.interface_, render.RenderState{Blend: "alpha"})
	s.presentMaterial = assets.MakeMaterial("present", s.present, render.RenderState{})
	return s
}

// prepareFrame acquires the frame's temporary textures, which several of the
// game's tasks then use.
func prepareFrame(frame *render.Frame) frameTargets {
	return frameTargets{
		color: frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "scene colour", Width: 1920, Height: 1080, Format: "rgba16float"}),
		depth: frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "scene depth", Width: 1920, Height: 1080, Format: "depth32float"}),
	}
}

// cullObjects is a compute pass ahead of the scene, followed by a copy.
func cullObjects(frame *render.Frame, s scene) {
	list := frame.BeginCommandList("cull", cullOrder)
	objects := list.AcquireTemporaryBuffer(make([]byte, 500*64))
	visible := list.AcquireTemporaryBuffer(make([]byte, 500*4))
	compute := list.BeginComputePass("cull")
	compute.SetShader(s.cull)
	compute.SetBuffer("objects", objects)
	compute.SetBuffer("visible", visible)
	compute.SetUniform("camera", Camera{})
	compute.Dispatch(8, 1, 1)
	compute.End()
	list.End()
}

// drawSceneA draws 500 crates into the scene's temporary textures, in
// variant A: a setter per parameter kind.
func drawSceneA(frame *render.Frame, assets *render.Assets, s scene, targets frameTargets, number int) {
	list := frame.BeginCommandList("scene", sceneOrder)
	instances := list.AcquireTemporaryBuffer(make([]byte, 500*64))
	pass := list.BeginPass(render.PassDescriptor{
		Label:        "scene",
		ColorTargets: []render.ColorTarget{{Texture: targets.color, Clear: true, ClearColor: render.Color{R: 0.1, G: 0.1, B: 0.1, A: 1}}},
		DepthStencil: render.DepthStencilTarget{Texture: targets.depth, Clear: true, ClearDepth: 1},
	})
	pass.SetPassUniform("camera", Camera{})
	pass.SetPassBuffer("objects", instances)
	pass.SetMaterial(s.crateMaterial)
	pass.Draw(s.crate, render.DrawArgs{InstanceCount: 250})
	// A material changed between its draws: both draws read the value it
	// holds at the submit.
	assets.SetMaterialUniform(s.crateMaterial, "surface", Surface{Tint: [4]float32{1, 0.5, 0.5, float32(number)}})
	pass.Draw(s.crate, render.DrawArgs{InstanceCount: 250, FirstInstance: 250})
	pass.End()
	list.End()
}

// drawInterface draws two panels over the presented scene, clipped by the
// scissor. Its pass keeps the window, so its order must follow present's:
// the first run of this prototype had them the other way round, and present's
// clear wiped the panels.
func drawInterface(frame *render.Frame, s scene) {
	list := frame.BeginCommandList("interface", interfaceOrder)
	pass := list.BeginPass(render.PassDescriptor{Label: "interface", ColorTargets: []render.ColorTarget{{Window: window}}})
	pass.SetMaterial(s.panelMaterial)
	pass.SetScissor(render.Rectangle{X: 0, Y: 0, Width: 400, Height: 300})
	pass.SetUniform("panel", Panel{Rectangle: [4]float32{10, 10, 200, 100}, Color: [4]float32{0, 0, 0, 0.5}})
	pass.Draw(render.Mesh{}, render.DrawArgs{VertexCount: 6})
	pass.SetUniform("panel", Panel{Rectangle: [4]float32{10, 120, 200, 100}, Color: [4]float32{0, 0, 0, 0.5}})
	pass.Draw(render.Mesh{}, render.DrawArgs{VertexCount: 6})
	pass.End()
	list.End()
}

// present draws the scene colour into the window, full-screen, before the
// interface: the last pass into the window encodes sRGB in its shader.
func present(frame *render.Frame, s scene, targets frameTargets) {
	list := frame.BeginCommandList("present", windowOrder)
	pass := list.BeginPass(render.PassDescriptor{Label: "present", ColorTargets: []render.ColorTarget{{Window: window, Clear: true}}})
	pass.SetMaterial(s.presentMaterial)
	pass.SetTexture("scene", targets.color)
	pass.Draw(render.Mesh{}, render.DrawArgs{VertexCount: 3})
	pass.End()
	list.End()
}

// unload releases every hold the game took.
func unload(assets *render.Assets, s scene) {
	assets.ReleaseMaterial(s.presentMaterial)
	assets.ReleaseMaterial(s.panelMaterial)
	assets.ReleaseMaterial(s.crateMaterial)
	assets.ReleaseMesh(s.crate)
	assets.ReleaseBuffer(s.crateIndices)
	assets.ReleaseBuffer(s.crateVertices)
	assets.ReleaseTexture(s.crateAlbedo)
	assets.ReleaseShader(s.present)
	assets.ReleaseShader(s.interface_)
	assets.ReleaseShader(s.cull)
	assets.ReleaseShader(s.lit)
}
