package rendertest_test

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// parametersDeclaration declares the pass and draw parameter tests' shader,
// parameters.wgsl: in @group(0), a camera's uniform block its vertex
// function reads, a storage buffer no function reads, and an environment
// texture with its sampler; in @group(1), an albedo texture; and in
// @group(2), an object's uniform block, a storage buffer of rows no function
// reads, and a mask texture array with its sampler.
const parametersDeclaration = `{
	"bindings": [
		{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 64}, "entryPoints": ["vertex_main"]},
		{"group": 0, "binding": 1, "name": "light_grid", "buffer": {"type": "read-only-storage", "size": 16}},
		{"group": 0, "binding": 2, "name": "environment", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
		{"group": 0, "binding": 3, "name": "environment_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]},
		{"group": 1, "binding": 0, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
		{"group": 2, "binding": 0, "name": "object", "buffer": {"type": "uniform", "size": 16}, "entryPoints": ["vertex_main"]},
		{"group": 2, "binding": 1, "name": "rows", "buffer": {"type": "read-only-storage", "size": 16}},
		{"group": 2, "binding": 2, "name": "mask", "texture": {"sampleType": "float", "viewDimension": "2d-array"}, "entryPoints": ["fragment_main"]},
		{"group": 2, "binding": 3, "name": "mask_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]}
	],
	"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
	"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
}`

// passParameterScene is what the pass and draw parameter tests draw, beside
// a drawScene: two materials of parameters.wgsl, a 4×4 colour texture, one
// of three layers, and a storage buffer of 32 bytes.
type passParameterScene struct {
	drawScene
	shader          render.Shader
	material, other render.Material
	albedo, layers  render.Texture
	lightGrid       render.Buffer
}

// makePassParameterScene makes a passParameterScene through inventory.
func makePassParameterScene(inventory render.Inventory) passParameterScene {
	scene := passParameterScene{
		drawScene: makeDrawScene(inventory),
		shader:    inventory.AcquireShader("parameters.wgsl"),
		albedo:    inventory.MakeTexture[byte](render.TextureDescriptor{Label: "albedo", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil),
		layers:    inventory.MakeTexture[byte](render.TextureDescriptor{Label: "layers", Width: 4, Height: 4, LayerCount: 3, Format: render.TextureFormatRGBA8Unorm}, nil),
		lightGrid: inventory.MakeBuffer("light grid", render.BufferUsageFlagStorage, make([]byte, 32)),
	}
	scene.material = inventory.MakeMaterial(render.MaterialDescriptor{Label: "lit", Shader: scene.shader})
	scene.other = inventory.MakeMaterial(render.MaterialDescriptor{Label: "other", Shader: scene.shader})
	return scene
}

// spawnPassParameterHarness spawns h as spawnDeclaredHarness does, with
// parameters.wgsl and passreading.wgsl.
func spawnPassParameterHarness(t *testing.T, h *rendertest.Harness, config *render.Config, frameCount int, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	t.Helper()
	spawnDeclaredHarness(t, h, config, map[string]string{"parameters.wgsl": parametersDeclaration, "passreading.wgsl": passReadingDeclaration}, frameCount, duringFrame)
}

// findEveryPassCommands answers the commands of each pass labelled label gfx
// submitted, frame after frame, as findPassCommands answers the first's.
func findEveryPassCommands(h *rendertest.Harness, label string) [][]recording.Command {
	var passes [][]recording.Command
	for _, frame := range h.RecordedFrames() {
		for _, buffer := range readSubmittedBuffers(frame) {
			for _, command := range buffer.Commands {
				if descriptor, ok := command.Arguments.(gfx.RenderPassDescriptor); ok && descriptor.Label == label {
					passes = append(passes, command.Commands)
				}
			}
		}
	}
	return passes
}

// findSetBindGroups answers the arguments of every SetBindGroup of index
// group among commands.
func findSetBindGroups(commands []recording.Command, group uint32) []recording.SetBindGroupArguments {
	var found []recording.SetBindGroupArguments
	for _, command := range commands {
		if arguments, ok := command.Arguments.(recording.SetBindGroupArguments); ok && arguments.Index == group {
			found = append(found, arguments)
		}
	}
	return found
}

// findCreatedBindGroups answers the bind groups calls created labelled
// label, by the frame each was created in: how many submits came before it.
func findCreatedBindGroups(calls []recording.Call, label string) map[gfx.BindGroup]int {
	created := map[gfx.BindGroup]int{}
	submitCount := 0
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateBindGroupArguments); ok && arguments.Descriptor.Label == label {
			created[arguments.Group] = submitCount
		}
		if call.Kind == recording.CallKindSubmit {
			submitCount++
		}
	}
	return created
}

// findReleaseFrame answers how many submits came before the call of kind
// releasing object, and false when none did.
func findReleaseFrame(calls []recording.Call, kind recording.CallKind, object any) (int, bool) {
	submitCount := 0
	for _, call := range calls {
		switch {
		case call.Kind == kind && call.Arguments == object:
			return submitCount, true
		case call.Kind == recording.CallKindSubmit:
			submitCount++
		}
	}
	return 0, false
}

// parameterBindGroupLabel labels the bind groups of pass and draw parameters
// render shares.
const parameterBindGroupLabel = "render: a bind group of pass or draw parameters"

func TestAPassesParametersAreOneBindGroupAndDrawParametersRebindAtNewOffsetsWithoutNewBindGroups(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	cameras := [][16]float32{{1, 2, 3, 4}, {5, 6, 7, 8}}
	objects := [][4]float32{{1}, {2}, {3}, {4}}
	spawnPassParameterHarness(t, h, nil, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetPassUniform("camera", cameras[frameIndex])
			pass.SetPassTexture("environment", scene.albedo)
			pass.SetPassSampler("environment_sampler", render.SamplerDescriptor{MagFilter: render.FilterModeLinear})
			pass.SetMaterial(scene.material)
			for index, object := range objects {
				if index == 2 {
					pass.SetMaterial(scene.other)
				}
				pass.SetUniform("object", object)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		})
	})

	passCommands := findEveryPassCommands(h, "scene")
	for frameIndex, commands := range passCommands {
		passGroups := findSetBindGroups(commands, 0)
		drawGroups := findSetBindGroups(commands, 2)
		if !reflect.DeepEqual(drawGroups, findSetBindGroups(passCommands[0], 2)) {
			t.Errorf("frame %d set @group(2) %+v, want the first frame's, its list's chunk emptied at the frame's end", frameIndex, drawGroups)
		}
		if len(passGroups) != 1 {
			t.Errorf("frame %d: the pass set @group(0) %d times for four draws of two materials, want once", frameIndex, len(passGroups))
		}
		if len(drawGroups) != len(objects) {
			t.Fatalf("frame %d: the pass set @group(2) %d times, want %d: once for each draw, at its offset", frameIndex, len(drawGroups), len(objects))
		}
		for index, set := range drawGroups {
			if set.Group != drawGroups[0].Group || len(set.DynamicOffsets) != 2 || set.DynamicOffsets[0]%256 != 0 || (index > 0 && set.DynamicOffsets[0] == drawGroups[index-1].DynamicOffsets[0]) {
				t.Errorf("frame %d: draw %d set @group(2) %+v, want the first draw's bind group at a new offset, a multiple of 256", frameIndex, index, set)
			}
		}
	}
	created := findCreatedBindGroups(h.Calls(), parameterBindGroupLabel)
	if len(created) != 2 {
		t.Errorf("render made %d bind groups of pass or draw parameters, want 2: the pass's and the draws'", len(created))
	}
	for group, frameIndex := range created {
		if frameIndex != 0 {
			t.Errorf("render made bind group %v in frame %d, want every one made in the first frame and shared by the second", group, frameIndex)
		}
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		for index, draw := range draws {
			want := []render.InspectedParameter{
				{Name: "object", Group: 2, Binding: 0, Uniform: unsafe.Slice((*byte)(unsafe.Pointer(&objects[index])), 16)},
				{Name: "rows", Group: 2, Binding: 1, Default: true},
				{Name: "mask", Group: 2, Binding: 2, Default: true},
				{Name: "mask_sampler", Group: 2, Binding: 3, Default: true},
			}
			if !reflect.DeepEqual(draw.DrawParameters, want) {
				t.Errorf("frame %d: draw %d bound draw parameters\n%+v\nwant\n%+v", frameIndex, index, draw.DrawParameters, want)
			}
			wantPass := []render.InspectedParameter{
				{Name: "camera", Group: 0, Binding: 0, Uniform: unsafe.Slice((*byte)(unsafe.Pointer(&cameras[frameIndex])), 64)},
				{Name: "light_grid", Group: 0, Binding: 1, Default: true},
				{Name: "environment", Group: 0, Binding: 2, TextureView: render.TextureView{Texture: scene.albedo, MipLevelCount: 1, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}},
				{Name: "environment_sampler", Group: 0, Binding: 3, Sampler: render.SamplerDescriptor{MagFilter: render.FilterModeLinear}},
			}
			if !reflect.DeepEqual(draw.PassParameters, wantPass) {
				t.Errorf("frame %d: draw %d bound pass parameters\n%+v\nwant\n%+v", frameIndex, index, draw.PassParameters, wantPass)
			}
		}
	}
}

func TestADrawParameterLastsUntilSetAgainTheMaterialChangesOrThePassEnds(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	camera := [16]float32{5}
	first := [4]float32{1}
	second := [4]float32{2}
	spawnPassParameterHarness(t, h, nil, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makePassParameterScene(inventory)
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetPassUniform("camera", camera)
		pass.SetMaterial(scene.material)
		pass.SetUniform("object", first)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetUniform("object", second)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetMaterial(scene.material)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetUniform("object", first)
		pass.End()
		next := list.BeginPass(scene.makeKeptPass("next"))
		next.SetMaterial(scene.material)
		next.Draw(scene.indexedMesh, render.DrawArgs{})
		next.End()
		list.End()
	})

	objectBytes := func(value [4]float32) []byte { return unsafe.Slice((*byte)(unsafe.Pointer(&value)), 16) }
	draws := slices.Concat(findPassDraws(h, "scene")[0], findPassDraws(h, "next")[0])
	want := []render.InspectedParameter{
		{Name: "object", Group: 2, Binding: 0, Uniform: objectBytes(first)},
		{Name: "object", Group: 2, Binding: 0, Uniform: objectBytes(first)},
		{Name: "object", Group: 2, Binding: 0, Uniform: objectBytes(second)},
		{Name: "object", Group: 2, Binding: 0, Default: true},
		{Name: "object", Group: 2, Binding: 0, Default: true},
	}
	if len(draws) != len(want) {
		t.Fatalf("the passes drew %d times, want %d", len(draws), len(want))
	}
	for index, draw := range draws {
		if object, _ := findParameterIn(draw.DrawParameters, "object"); !reflect.DeepEqual(object, want[index]) {
			t.Errorf("draw %d bound %+v, want %+v", index, object, want[index])
		}
	}
	for index, draw := range draws[:4] {
		if camera, _ := findParameterIn(draw.PassParameters, "camera"); camera.Default {
			t.Errorf("draw %d bound the camera's default, want the pass parameter, set before it and lasting past SetMaterial", index)
		}
	}
	if camera, _ := findParameterIn(draws[4].PassParameters, "camera"); !camera.Default {
		t.Errorf("the next pass's draw bound camera %+v, want its default: a pass parameter ends with its pass", camera)
	}
}

func TestAfterSetMaterialADrawParameterSetAgainBindsItsNewValueAndOneNotSetAgainItsDefault(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	first := [4]float32{1}
	second := [4]float32{2}
	spawnPassParameterHarness(t, h, nil, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makePassParameterScene(inventory)
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		for _, material := range []render.Material{scene.material, scene.other, scene.material} {
			pass.SetMaterial(material)
			if material == scene.other {
				pass.SetUniform("object", second)
			} else {
				pass.SetUniform("object", first)
				pass.SetTexture("mask", scene.layers)
			}
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
		list.End()
	})

	objectBytes := func(value [4]float32) []byte { return unsafe.Slice((*byte)(unsafe.Pointer(&value)), 16) }
	draws := findPassDraws(h, "scene")[0]
	wantObjects := [][]byte{objectBytes(first), objectBytes(second), objectBytes(first)}
	wantMaskDefaults := []bool{false, true, false}
	if len(draws) != len(wantObjects) {
		t.Fatalf("the pass drew %d times, want %d", len(draws), len(wantObjects))
	}
	for index, draw := range draws {
		if object, _ := findParameterIn(draw.DrawParameters, "object"); !bytes.Equal(object.Uniform, wantObjects[index]) {
			t.Errorf("draw %d bound object %+v, want the bytes %v, set again after SetMaterial", index, object, wantObjects[index])
		}
		if mask, _ := findParameterIn(draw.DrawParameters, "mask"); mask.Default != wantMaskDefaults[index] {
			t.Errorf("draw %d bound mask %+v, want it default %v: set again after SetMaterial, or not", index, mask, wantMaskDefaults[index])
		}
	}
}

func TestSetMaterialResetsMoreThan64DrawParameters(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	spawnPassParameterHarness(t, h, nil, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makePassParameterScene(inventory)
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(scene.material)
		pass.SetUniform("object", [4]float32{1})
		for index := range 64 {
			// Names the shader lacks, ignored by its draws.
			pass.SetSampler(fmt.Sprintf("unused %d", index), render.SamplerDescriptor{})
		}
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetMaterial(scene.material)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	draws := findPassDraws(h, "scene")[0]
	if len(draws) != 2 {
		t.Fatalf("the pass drew %d times, want 2", len(draws))
	}
	for index, wantDefault := range []bool{false, true} {
		if object, _ := findParameterIn(draws[index].DrawParameters, "object"); object.Default != wantDefault {
			t.Errorf("draw %d bound object %+v, want it default %v: 65 draw parameters set, then SetMaterial", index, object, wantDefault)
		}
	}
}

func TestTexturesAndViewsAsPassAndDrawParametersAreRefusedAndCaughtAsAMaterialsAre(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	var scene passParameterScene
	spawnPassParameterHarness(t, h, nil, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
		}
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetPassTextureView("environment", render.TextureView{Texture: scene.layers, BaseLayer: 2, Dimension: render.TextureViewDimension2D})
		pass.SetPassTexture("albedo", scene.albedo)
		pass.SetPassUniform("missing", [4]float32{})
		pass.SetMaterial(scene.material)
		pass.SetTextureView("mask", render.TextureView{Texture: scene.layers, BaseLayer: 1})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetTextureView("mask", render.TextureView{Texture: scene.layers, Dimension: render.TextureViewDimensionCube})
		pass.SetUniform("camera", [16]float32{})
		pass.SetUniform("object", [2]float32{})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		overlapping := list.BeginPass(scene.makeKeptPass("overlapping"))
		overlapping.SetPassTexture("environment", scene.color)
		overlapping.SetMaterial(scene.material)
		overlapping.SetTextureView("mask", render.TextureView{Texture: scene.color})
		overlapping.Draw(scene.indexedMesh, render.DrawArgs{})
		overlapping.End()
		list.End()
	})

	if count := countReports(h, render.ReportKindParameterRefused); count != 5 {
		t.Errorf("render reported %d refused parameters, want 5: albedo and camera outside their groups, the cube view of mask, an object of 8 bytes, and the colour target as environment, each once though drawn in two frames", count)
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		environment, _ := findParameterIn(draws[0].PassParameters, "environment")
		wantEnvironment := render.TextureView{Texture: scene.layers, MipLevelCount: 1, BaseLayer: 2, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}
		if environment.TextureView != wantEnvironment {
			t.Errorf("frame %d: environment bound %+v, want %+v", frameIndex, environment, wantEnvironment)
		}
		mask, _ := findParameterIn(draws[0].DrawParameters, "mask")
		wantMask := render.TextureView{Texture: scene.layers, MipLevelCount: 1, BaseLayer: 1, LayerCount: 2, Dimension: render.TextureViewDimension2DArray, Aspect: render.TextureAspectAll}
		if mask.TextureView != wantMask {
			t.Errorf("frame %d: the first draw's mask bound %+v, want %+v", frameIndex, mask, wantMask)
		}
		if mask, _ := findParameterIn(draws[1].DrawParameters, "mask"); !mask.Default {
			t.Errorf("frame %d: the second draw's mask bound %+v, want its default for a cube view of a 2D-array binding", frameIndex, mask)
		}
		if object, _ := findParameterIn(draws[1].DrawParameters, "object"); !object.Default {
			t.Errorf("frame %d: the second draw's object bound %+v, want its default for a value of 8 bytes", frameIndex, object)
		}
		if albedo, _ := findParameter(draws[0], "albedo"); !albedo.Default {
			t.Errorf("frame %d: albedo bound %+v, want the material's default: a pass parameter does not bind @group(1)", frameIndex, albedo)
		}
	}
	for frameIndex, draws := range findPassDraws(h, "overlapping") {
		environment, _ := findParameterIn(draws[0].PassParameters, "environment")
		mask, _ := findParameterIn(draws[0].DrawParameters, "mask")
		if !environment.Default || !mask.Default {
			t.Errorf("frame %d: in a pass writing colour, environment bound %+v and mask %+v, want both their defaults", frameIndex, environment, mask)
		}
	}
}

func TestAMadeBufferAsAPassStorageParameterReusesThePassesBindGroupFromFrameToFrameUntilItIsFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	var reading render.Material
	spawnPassParameterHarness(t, h, nil, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
			reading = inventory.MakeMaterial(render.MaterialDescriptor{Label: "reading", Shader: inventory.AcquireShader("passreading.wgsl")})
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetPassBuffer("light_grid", render.BufferRange{Buffer: scene.lightGrid})
			pass.SetMaterial(reading)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		if frameIndex == 2 {
			inventory.ReleaseBuffer(scene.lightGrid)
		}
	})

	var passGroups []gfx.BindGroup
	for _, commands := range findEveryPassCommands(h, "scene") {
		for _, set := range findSetBindGroups(commands, 0) {
			passGroups = append(passGroups, set.Group)
		}
	}
	if len(passGroups) != 3 || passGroups[1] != passGroups[0] || passGroups[2] != passGroups[0] {
		t.Errorf("the passes bound @group(0) %v, want one bind group in each of three frames, the first frame's", passGroups)
	}
	calls := h.Calls()
	created := findCreatedBindGroups(calls, parameterBindGroupLabel)
	if len(created) != 1 {
		t.Errorf("render made %d bind groups of pass parameters, want one, made in the first frame", len(created))
	}
	for group := range created {
		if frameIndex, ok := findReleaseFrame(calls, recording.CallKindReleaseBindGroup, group); !ok || frameIndex != 3 {
			t.Errorf("the pass's bind group was released after %d submits (%t), want after 3: at the end of the frame its buffer was freed", frameIndex, ok)
		}
	}
	for _, draws := range findPassDraws(h, "scene") {
		if grid, _ := findParameterIn(draws[0].PassParameters, "light_grid"); grid.Buffer != (render.BufferRange{Buffer: scene.lightGrid, Size: 32}) {
			t.Errorf("light_grid bound %+v, want the light grid's 32 bytes", grid)
		}
	}
}

func TestABindGroupOfPassParametersNamingAFreedTextureIsReleasedAtThatFramesEnd(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	spawnPassParameterHarness(t, h, nil, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			if frameIndex < 2 {
				pass.SetPassTexture("environment", scene.albedo)
			}
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		if frameIndex == 1 {
			inventory.ReleaseTexture(scene.albedo)
		}
	})

	calls := h.Calls()
	created := findCreatedBindGroups(calls, parameterBindGroupLabel)
	if len(created) != 1 {
		t.Fatalf("render made %d bind groups of pass parameters, want one, shared by the first two frames", len(created))
	}
	for group := range created {
		if frameIndex, ok := findReleaseFrame(calls, recording.CallKindReleaseBindGroup, group); !ok || frameIndex != 2 {
			t.Errorf("the pass's bind group was released after %d submits (%t), want after 2: at the end of the frame its texture was freed", frameIndex, ok)
		}
	}
}

func TestATemporaryBufferAsAPassStorageParameterCostsItsPassABindGroupInEachFrameItMoves(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene passParameterScene
	var reading, other render.Material
	var ranges []render.BufferRange
	spawnPassParameterHarness(t, h, nil, 4, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makePassParameterScene(inventory)
			shader := inventory.AcquireShader("passreading.wgsl")
			reading = inventory.MakeMaterial(render.MaterialDescriptor{Label: "reading", Shader: shader})
			other = inventory.MakeMaterial(render.MaterialDescriptor{Label: "other", Shader: shader})
		}
		list := frame.BeginCommandList("draws", drawOrder)
		for range min(frameIndex, 2) {
			list.AcquireTemporaryBuffer([]uint32{0})
		}
		grid := list.AcquireTemporaryBuffer([]uint32{uint32(frameIndex), 1, 2, 3})
		ranges = append(ranges, grid)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetPassBuffer("light_grid", grid)
		pass.SetMaterial(reading)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.SetMaterial(other)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	createdFrames := map[int]int{}
	for _, frameIndex := range findCreatedBindGroups(h.Calls(), parameterBindGroupLabel) {
		createdFrames[frameIndex]++
	}
	if !reflect.DeepEqual(createdFrames, map[int]int{0: 1, 1: 1, 2: 1}) {
		t.Errorf("render made bind groups naming the temporary buffer in frames %v, want one in each of the three frames it moved in, and none in the last, where it stayed", createdFrames)
	}
	for frameIndex, commands := range findEveryPassCommands(h, "scene") {
		if sets := findSetBindGroups(commands, 0); len(sets) != 1 {
			t.Errorf("frame %d: the pass set @group(0) %d times for draws of two materials of one shader, want once", frameIndex, len(sets))
		}
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if grid, _ := findParameterIn(draws[1].PassParameters, "light_grid"); grid.Buffer != ranges[frameIndex] {
			t.Errorf("frame %d: light_grid bound %+v, want %+v, the frame's temporary buffer", frameIndex, grid.Buffer, ranges[frameIndex])
		}
	}
}

func TestListsSettingPassAndDrawParametersAtOnceBindTheirOwnValues(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	var made sync.Once
	var scene passParameterScene
	var nextOrder atomic.Int32
	files := makeDrawFiles(t)
	putFile(t, files, "parameters.wgsl", []byte("// parameters.wgsl"))
	putFile(t, files, "parameters.wgsl.json", []byte(parametersDeclaration))
	h.Spawn(&render.Config{TemporaryChunkSize: 1024}, &loaderPlugin{files: files, recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		made.Do(func() { scene = makePassParameterScene(inventory) })
		order := nextOrder.Add(1)
		list := frame.BeginCommandList("draws", drawOrder+render.Order(order))
		pass := list.BeginPass(render.PassDescriptor{Label: "scene", ColorTargets: []render.ColorTarget{{Texture: scene.color}}})
		pass.SetPassUniform("camera", [16]float32{float32(order)})
		pass.SetMaterial(scene.material)
		for index := range 16 {
			pass.SetUniform("object", [4]float32{float32(order), float32(index)})
			pass.SetBuffer("rows", list.AcquireTemporaryBuffer([]float32{float32(order), float32(index), 0, 0}))
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	passes := findPassDraws(h, "scene")
	if len(passes) != 2*listCount {
		t.Fatalf("the reader read %d passes, want %d", len(passes), 2*listCount)
	}
	for passIndex, draws := range passes {
		camera, _ := findParameterIn(draws[0].PassParameters, "camera")
		order := readFloat32(camera.Uniform)
		for drawIndex, draw := range draws {
			object, _ := findParameterIn(draw.DrawParameters, "object")
			if want := [4]float32{order, float32(drawIndex)}; len(object.Uniform) != 16 || *(*[4]float32)(unsafe.Pointer(&object.Uniform[0])) != want {
				t.Errorf("pass %d, draw %d: object bound %v, want %v, its own list's value", passIndex, drawIndex, object.Uniform, want)
			}
		}
		if len(draws) != 16 {
			t.Errorf("pass %d drew %d times, want 16", passIndex, len(draws))
		}
	}
}

// findParameterIn answers the parameter named name among parameters, and
// whether it is there.
func findParameterIn(parameters []render.InspectedParameter, name string) (render.InspectedParameter, bool) {
	index := slices.IndexFunc(parameters, func(parameter render.InspectedParameter) bool { return parameter.Name == name })
	if index < 0 {
		return render.InspectedParameter{}, false
	}
	return parameters[index], true
}

// readFloat32 answers the float32 bytes begins with, or zero for fewer
// than four bytes.
func readFloat32(bytes []byte) float32 {
	if len(bytes) < 4 {
		return 0
	}
	return *(*float32)(unsafe.Pointer(&bytes[0]))
}
