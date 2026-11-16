package rendertest_test

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// The allocation gate's shaders: gatemesh.wgsl draws an indexed mesh with
// an instance buffer at location 1, a pass's camera, a material's albedo,
// its sampler and a tint, and a draw's object uniform block; gatevertices.wgsl
// draws with no mesh, reading a draw's rows at instance_index;
// gatetexture.wgsl draws a mesh sampling a draw's mask texture; and
// gatecompute.wgsl's step reads a uniform block of parameters and writes a
// storage buffer of values.
const (
	gateMeshDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 64, "members": [{"name": "view_projection", "offset": 0, "size": 64}]}, "entryPoints": ["vertex_main"]},
			{"group": 1, "binding": 0, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 1, "name": "albedo_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 2, "name": "tint", "buffer": {"type": "uniform", "size": 16, "members": [{"name": "value", "offset": 0, "size": 16}]}, "entryPoints": ["fragment_main"]},
			{"group": 2, "binding": 0, "name": "object", "buffer": {"type": "uniform", "size": 16, "members": [{"name": "value", "offset": 0, "size": 16}]}, "entryPoints": ["vertex_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}, {"location": 1, "type": "vec4<f32>"}]}
	}`
	gateVerticesDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 64, "members": [{"name": "view_projection", "offset": 0, "size": 64}]}, "entryPoints": ["vertex_main"]},
			{"group": 2, "binding": 0, "name": "rows", "buffer": {"type": "read-only-storage", "size": 16}, "entryPoints": ["vertex_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": []}
	}`
	gateTextureDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 64, "members": [{"name": "view_projection", "offset": 0, "size": 64}]}, "entryPoints": ["vertex_main"]},
			{"group": 2, "binding": 0, "name": "mask", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 2, "binding": 1, "name": "mask_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}, {"location": 1, "type": "vec4<f32>"}]}
	}`
	gateComputeDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "parameters", "buffer": {"type": "uniform", "size": 16, "members": [{"name": "count", "offset": 0, "size": 16}]}, "entryPoints": ["step"]},
			{"group": 2, "binding": 0, "name": "values", "buffer": {"type": "storage", "size": 16}, "entryPoints": ["step"]}
		],
		"entryPoints": [{"name": "step", "stage": "compute"}]
	}`
)

// gateRowCount is how many rows of gateVector each list's temporary buffer of
// rows holds, read by gatevertices.wgsl at instance_index.
const gateRowCount = 64

// gatePaddingBlockCount is how many 256-byte blocks the padding before list
// 0's passes given a temporary range of lights grows to before it starts
// again: more than render's TemporaryIdleFrames times the most such passes
// a scenario records, so no range sits where a bind group kept from the
// frames before binds.
const gatePaddingBlockCount = 256

// gateCamera is the gate's shaders' camera uniform block.
type gateCamera struct {
	ViewProjection [16]float32
}

// gateVector is the gate's shaders' blocks of one vec4: gatemesh.wgsl's
// tint and object, gatevertices.wgsl's rows and passreading.wgsl's light
// grid.
type gateVector struct {
	Value [4]float32
}

// gateParameters is gatecompute.wgsl's uniform block of parameters.
type gateParameters struct {
	Count [4]uint32
}

// gateShape is what a draw of the gate draws.
type gateShape int

const (
	// gateShapeMesh draws an indexed mesh with one uniform draw parameter.
	gateShapeMesh gateShape = iota
	// gateShapeVertices draws with no mesh, reading a storage buffer draw
	// parameter at instance_index.
	gateShapeVertices
	// gateShapeTexture draws an indexed mesh with a texture draw parameter.
	gateShapeTexture
)

// gateScenario is the frame each list of the gate records: unless
// drawsOnly is set, a compute pass, a copy and a buffer clear; passCount
// passes into the list's temporary
// textures, the first clearing them, with drawCount draws among them, each
// of shape, the material switched every switchPeriod draws; then a pass
// drawing a temporary mesh, unless drawsOnly is set. List 0 then draws a material changed
// versionCount times between its draws, and records
// temporaryPassParameterCount passes, each given a temporary range as a
// @group(0) storage parameter, which sits where none sat in the frames
// before when temporaryRangeMoves is set, and where it sat the frame before
// otherwise.
type gateScenario struct {
	shape                       gateShape
	drawCount, listCount        int
	passCount, switchPeriod     int
	versionCount                int
	temporaryPassParameterCount int
	temporaryRangeMoves         bool
	drawsOnly                   bool
}

// String answers the scenario's name as a subtest's.
func (s gateScenario) String() string {
	shapeName := [...]string{gateShapeMesh: "mesh", gateShapeVertices: "vertices", gateShapeTexture: "texture"}[s.shape]
	return fmt.Sprintf("%s/draws=%d/lists=%d/passes=%d/switch=%d", shapeName, s.drawCount, s.listCount, s.passCount, s.switchPeriod)
}

// gateScene is what the gate draws with, made in its first frame, beside a
// drawScene, whose indexed mesh it draws.
type gateScene struct {
	drawScene
	meshShader, verticesShader, textureShader, litShader, computeShader render.Shader
	// materials are two materials of each shape, by shape.
	materials         [3][2]render.Material
	versioned, lit    render.Material
	albedo            render.Texture
	masks             [2]render.Texture
	offsets           render.Buffer
	copySource        render.Buffer
	copyDestinations  []render.Buffer
	targetDescriptors []render.TextureDescriptor
	depthDescriptors  []render.TextureDescriptor
	listLabels        []string
	passLabels        []string
	rows              []gateVector
	lights            []gateVector
	padding           []gateVector
	temporaryVertices []float32
}

// gatePlugin is the gate's plugin, listed after render: it binds files as a
// read mount, makes the gate's scene in a task of its first frame, and on
// each beat records scenario's lists, each in a task of its own.
type gatePlugin struct {
	files    fs.FS
	scenario gateScenario
	scene    gateScene
	// sceneScheduled is set once the handler of render.FrameEvent has
	// scheduled the scene's making, in the first frame.
	sceneScheduled bool
	// tint is the value the versioned material's tint was last set to, which
	// list 0's task alone sets, each set a new value, making a version; and
	// paddingCount how many blocks list 0 pads its arena with before the
	// passes given a temporary range of lights, a block more each frame for
	// each such pass, so each range sits where none sat in the frames
	// before.
	tint         float32
	paddingCount int
}

func (*gatePlugin) Name() kernel.PluginName { return "gate plugin" }

func (*gatePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *gatePlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: p.files})
	setup := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, inventory render.Inventory) kernel.NoResult {
		p.makeScene(inventory)
		return kernel.NoResult{}
	})
	record := configurator.RegisterTask2(func(k *kernel.Kernel, listIndex int, inventory render.Inventory, frame render.Frame) kernel.NoResult {
		p.recordList(listIndex, inventory, frame)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		ready := event.Ready
		if !p.sceneScheduled {
			ready = ex.Schedule(setup, kernel.NoArg{}, ready).Handle()
			p.sceneScheduled = true
		}
		out := in
		for listIndex := range p.scenario.listCount {
			out = ex.Join(out, ex.Schedule(record, listIndex, ready).Handle())
		}
		return event, out
	})
	return nil
}

func (*gatePlugin) Start(*kernel.Kernel) error { return nil }

func (*gatePlugin) Stop(*kernel.Kernel) error { return nil }

func (*gatePlugin) Destroy() error { return nil }

// makeScene makes the gate's scene through inventory.
func (p *gatePlugin) makeScene(inventory render.Inventory) {
	scene := &p.scene
	scene.drawScene = makeDrawScene(inventory)
	scene.meshShader = inventory.AcquireShader("gatemesh.wgsl")
	scene.verticesShader = inventory.AcquireShader("gatevertices.wgsl")
	scene.textureShader = inventory.AcquireShader("gatetexture.wgsl")
	scene.litShader = inventory.AcquireShader("passreading.wgsl")
	scene.computeShader = inventory.AcquireShader("gatecompute.wgsl")
	for shape, shader := range [3]render.Shader{scene.meshShader, scene.verticesShader, scene.textureShader} {
		for index := range scene.materials[shape] {
			scene.materials[shape][index] = inventory.MakeMaterial(render.MaterialDescriptor{Label: fmt.Sprintf("material %d of shape %d", index, shape), Shader: shader})
		}
	}
	scene.versioned = inventory.MakeMaterial(render.MaterialDescriptor{Label: "versioned", Shader: scene.meshShader})
	scene.lit = inventory.MakeMaterial(render.MaterialDescriptor{Label: "lit", Shader: scene.litShader})
	scene.albedo = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "albedo", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
	for index := range scene.masks {
		scene.masks[index] = inventory.MakeTexture[byte](render.TextureDescriptor{Label: fmt.Sprintf("mask %d", index), Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil)
	}
	for _, materials := range scene.materials {
		for _, material := range materials {
			inventory.SetMaterialTexture(material, "albedo", scene.albedo)
		}
	}
	inventory.SetMaterialTexture(scene.versioned, "albedo", scene.albedo)
	scene.offsets = inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 4*gateRowCount))
	scene.copySource = inventory.MakeBuffer("copy source", render.BufferUsageFlagCopySrc|render.BufferUsageFlagStorage, make([]uint32, 64))
	for listIndex := range p.scenario.listCount {
		scene.copyDestinations = append(scene.copyDestinations, inventory.MakeBuffer(fmt.Sprintf("copy destination %d", listIndex), render.BufferUsageFlagStorage, make([]uint32, 128)))
		scene.targetDescriptors = append(scene.targetDescriptors, render.TextureDescriptor{Label: fmt.Sprintf("target %d", listIndex), Width: 64, Height: 64, Format: render.TextureFormatRGBA8Unorm})
		scene.depthDescriptors = append(scene.depthDescriptors, render.TextureDescriptor{Label: fmt.Sprintf("depth %d", listIndex), Width: 64, Height: 64, Format: render.TextureFormatDepth32Float})
		scene.listLabels = append(scene.listLabels, fmt.Sprintf("list %d", listIndex))
	}
	for passIndex := range p.scenario.passCount {
		scene.passLabels = append(scene.passLabels, fmt.Sprintf("pass %d", passIndex))
	}
	scene.rows = make([]gateVector, gateRowCount)
	scene.lights = make([]gateVector, 4)
	scene.padding = make([]gateVector, (gatePaddingBlockCount+1)*16)
	scene.temporaryVertices = make([]float32, 12)
}

// recordList records the gate scenario's list of index listIndex into
// frame.
func (p *gatePlugin) recordList(listIndex int, inventory render.Inventory, frame render.Frame) {
	scenario := p.scenario
	scene := &p.scene
	list := frame.BeginCommandList(scene.listLabels[listIndex], render.Order(listIndex+1))
	target := frame.AcquireTemporaryTexture(scene.targetDescriptors[listIndex])
	depth := frame.AcquireTemporaryTexture(scene.depthDescriptors[listIndex])
	rows := list.AcquireTemporaryBuffer(scene.rows)

	if !scenario.drawsOnly {
		compute := list.BeginComputePass("compute")
		compute.SetShader(scene.computeShader, "step")
		compute.SetUniform("parameters", gateParameters{Count: [4]uint32{gateRowCount}})
		compute.SetBuffer("values", rows)
		compute.Dispatch(1, 1, 1)
		compute.End()
		list.CopyBuffer(render.BufferRange{Buffer: scene.copySource, Size: 256}, render.BufferRange{Buffer: scene.copyDestinations[listIndex]})
		list.ClearBuffer(render.BufferRange{Buffer: scene.copyDestinations[listIndex], Offset: 256})
	}

	camera := gateCamera{ViewProjection: [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}}
	drawsPerPass := scenario.drawCount / scenario.passCount
	materials := scene.materials[scenario.shape]
	for passIndex := range scenario.passCount {
		pass := list.BeginPass(render.PassDescriptor{
			Label:        scene.passLabels[passIndex],
			ColorTargets: []render.ColorTarget{{Texture: target, Clear: passIndex == 0}},
			DepthStencil: render.DepthStencilTarget{Texture: depth, Clear: passIndex == 0, ClearDepth: 1},
		})
		pass.SetPassUniform("camera", camera)
		for drawIndex := passIndex * drawsPerPass; drawIndex < (passIndex+1)*drawsPerPass; drawIndex++ {
			if drawIndex == passIndex*drawsPerPass || drawIndex%scenario.switchPeriod == 0 {
				pass.SetMaterial(materials[drawIndex/scenario.switchPeriod%2])
				if scenario.shape != gateShapeVertices {
					pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: scene.offsets}, Layout: offsetsLayout})
				}
			}
			switch scenario.shape {
			case gateShapeMesh:
				pass.SetUniform("object", gateVector{Value: [4]float32{float32(drawIndex)}})
				pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1, FirstInstance: uint32(drawIndex % gateRowCount)})
			case gateShapeVertices:
				pass.SetBuffer("rows", rows)
				pass.DrawVertices(render.PrimitiveTopologyTriangleList, 6, render.DrawArgs{InstanceCount: 1, FirstInstance: uint32(drawIndex % gateRowCount)})
			case gateShapeTexture:
				pass.SetTexture("mask", scene.masks[drawIndex%2])
				pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1, FirstInstance: uint32(drawIndex % gateRowCount)})
			}
		}
		pass.End()
	}
	if scenario.drawsOnly {
		list.End()
		return
	}

	temporaryVertices := list.AcquireTemporaryBuffer(scene.temporaryVertices)
	temporaryMesh := list.AcquireTemporaryMesh(render.MeshDescriptor{
		Label:         "temporary",
		VertexBuffers: []render.VertexBuffer{{Range: temporaryVertices, Layout: positionLayout}},
		Topology:      render.PrimitiveTopologyTriangleStrip,
	})
	pass := list.BeginPass(render.PassDescriptor{Label: "temporary", ColorTargets: []render.ColorTarget{{Texture: target}}})
	pass.SetPassUniform("camera", camera)
	pass.SetMaterial(scene.materials[gateShapeMesh][0])
	pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: scene.offsets}, Layout: offsetsLayout})
	pass.SetUniform("object", gateVector{})
	pass.Draw(temporaryMesh, render.DrawArgs{InstanceCount: 1})
	pass.End()

	if listIndex == 0 {
		p.recordFixedCounts(&list, target, camera, inventory)
	}
	list.End()
}

// recordFixedCounts records into list the scenario's fixed-count
// operations: a pass drawing scene's versioned material once and then once
// after each of versionCount changes, and temporaryPassParameterCount passes
// each given a temporary range of lights as a @group(0) storage parameter.
func (p *gatePlugin) recordFixedCounts(list *render.CommandList, target render.Texture, camera gateCamera, inventory render.Inventory) {
	scene := &p.scene
	pass := list.BeginPass(render.PassDescriptor{Label: "versions", ColorTargets: []render.ColorTarget{{Texture: target}}})
	pass.SetPassUniform("camera", camera)
	pass.SetMaterial(scene.versioned)
	pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: scene.offsets}, Layout: offsetsLayout})
	pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1})
	for range p.scenario.versionCount {
		p.tint++
		inventory.SetMaterialUniform(scene.versioned, "tint", gateVector{Value: [4]float32{p.tint}})
		pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1})
	}
	pass.End()
	if p.scenario.temporaryRangeMoves {
		p.paddingCount = (p.paddingCount + p.scenario.temporaryPassParameterCount) % gatePaddingBlockCount
		list.AcquireTemporaryBuffer(scene.padding[:p.paddingCount*16+16])
	}
	for range p.scenario.temporaryPassParameterCount {
		pass := list.BeginPass(render.PassDescriptor{Label: "lit", ColorTargets: []render.ColorTarget{{Texture: target}}})
		pass.SetPassBuffer("light_grid", list.AcquireTemporaryBuffer(scene.lights))
		pass.SetMaterial(scene.lit)
		pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 1})
		pass.End()
	}
}
