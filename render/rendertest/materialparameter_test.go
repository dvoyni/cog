package rendertest_test

import (
	"bytes"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// The parameter tests' shaders: material.wgsl, whose vertex function reads a
// uniform block of the pass in @group(0), and whose fragment_main reads, in
// @group(1), a texture, a sampler, a uniform block, a depth texture with its
// comparison sampler, a 2D-array texture and an unsigned-integer texture,
// beside a storage buffer only its fragment_lit reads and a storage texture
// no function writes; reading.wgsl, whose
// vertex function reads a storage buffer of @group(1); and passreading.wgsl,
// whose fragment function reads a storage buffer of @group(0).
const (
	materialDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 64}, "entryPoints": ["vertex_main"]},
			{"group": 1, "binding": 0, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 1, "name": "albedo_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 2, "name": "tint", "buffer": {"type": "uniform", "size": 32, "members": [{"name": "color", "offset": 0, "size": 16}, {"name": "strength", "offset": 16, "size": 4}]}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 3, "name": "lights", "buffer": {"type": "read-only-storage", "size": 16}, "entryPoints": ["fragment_lit"]},
			{"group": 1, "binding": 4, "name": "shadow", "texture": {"sampleType": "depth", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 5, "name": "shadow_sampler", "sampler": {"type": "comparison"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 6, "name": "layers", "texture": {"sampleType": "float", "viewDimension": "2d-array"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 7, "name": "indices", "texture": {"sampleType": "uint", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 8, "name": "output", "storageTexture": {"access": "write-only", "format": "rgba8unorm", "viewDimension": "2d"}}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}, {"name": "fragment_lit", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
	readingDeclaration = `{
		"bindings": [
			{"group": 1, "binding": 0, "name": "particles", "buffer": {"type": "read-only-storage", "size": 16}, "entryPoints": ["vertex_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
	passReadingDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "light_grid", "buffer": {"type": "read-only-storage", "size": 16}, "entryPoints": ["fragment_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
)

// tint mirrors material.wgsl's uniform block tint: a vec4<f32> and an f32,
// padded to the block's 32 bytes.
type tint struct {
	Color    [4]float32
	Strength float32
	_        [12]byte
}

// swappedTint is tint with its members swapped, of the block's size but not
// its member offsets.
type swappedTint struct {
	Strength float32
	Color    [4]float32
	_        [12]byte
}

// parameterScene is what the parameter tests bind, beside a drawScene: a
// 4×4 colour texture, a 4×4 colour texture of three layers and two mip
// levels, a 4×4 unsigned-integer texture, a 4×4 depth texture no pass draws
// into, a storage buffer of 32 bytes and one of 8, too small for lights, and
// a material of material.wgsl's fragment_main.
type parameterScene struct {
	drawScene
	shader                             render.Shader
	albedo, layers, indices, shadowMap render.Texture
	lights, smallLights                render.Buffer
	material                           render.Material
}

// makeParameterScene makes a parameterScene through inventory.
func makeParameterScene(inventory render.Inventory) parameterScene {
	scene := parameterScene{
		drawScene:   makeDrawScene(inventory),
		shader:      inventory.AcquireShader("material.wgsl"),
		albedo:      inventory.MakeTexture[byte](render.TextureDescriptor{Label: "albedo", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm}, nil),
		layers:      inventory.MakeTexture[byte](render.TextureDescriptor{Label: "layers", Width: 4, Height: 4, LayerCount: 3, MipLevelCount: 2, Format: render.TextureFormatRGBA8Unorm}, nil),
		indices:     inventory.MakeTexture[byte](render.TextureDescriptor{Label: "indices", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Uint}, nil),
		shadowMap:   inventory.MakeTexture[byte](render.TextureDescriptor{Label: "shadow map", Width: 4, Height: 4, Format: render.TextureFormatDepth32Float}, nil),
		lights:      inventory.MakeBuffer("lights", render.BufferUsageFlagStorage, make([]byte, 32)),
		smallLights: inventory.MakeBuffer("small lights", render.BufferUsageFlagStorage, make([]byte, 8)),
	}
	scene.material = inventory.MakeMaterial(render.MaterialDescriptor{Label: "lit", Shader: scene.shader, FragmentEntryPoint: "fragment_main"})
	return scene
}

// spawnParameterHarness spawns h as spawnDeclaredHarness does, with the
// parameter tests' shaders and no config.
func spawnParameterHarness(t *testing.T, h *rendertest.Harness, frameCount int, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	t.Helper()
	spawnDeclaredHarness(t, h, nil, map[string]string{"material.wgsl": materialDeclaration, "reading.wgsl": readingDeclaration, "passreading.wgsl": passReadingDeclaration}, frameCount, duringFrame)
}

// spawnDeclaredHarness spawns h with config, the draw tests' shaders and
// each shader of declarations mounted beside its binding declaration,
// running duringFrame in each frame's recording task, and steps frameCount
// frames.
func spawnDeclaredHarness(t *testing.T, h *rendertest.Harness, config *render.Config, declarations map[string]string, frameCount int, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	t.Helper()
	files := makeDrawFiles(t)
	for name, declaration := range declarations {
		putFile(t, files, name, []byte("// "+name))
		putFile(t, files, name+".json", []byte(declaration))
	}
	h.Spawn(config, &loaderPlugin{files: files, duringFrame: duringFrame})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range frameCount {
		h.Step()
	}
}

// findParameter answers the parameter named name of draw's material, and
// whether it has one.
func findParameter(draw render.InspectedDraw, name string) (render.InspectedParameter, bool) {
	return findParameterIn(draw.Material.Parameters, name)
}

// findOnlyDraw answers the one draw of each frame's pass labelled label,
// failing the test when a frame has another count.
func findOnlyDraw(t *testing.T, h *rendertest.Harness, label string) []render.InspectedDraw {
	t.Helper()
	var draws []render.InspectedDraw
	for frameIndex, passDraws := range findPassDraws(h, label) {
		if len(passDraws) != 1 {
			t.Fatalf("frame %d: pass %q has %d draws, want one", frameIndex, label, len(passDraws))
		}
		draws = append(draws, passDraws[0])
	}
	return draws
}

func TestAMaterialsValuesLandInTheirBindingsByNameAndANameTheShaderLacksIsIgnored(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	value := tint{Color: [4]float32{1, 0.5, 0.25, 1}, Strength: 2}
	spawnParameterHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeParameterScene(inventory)
		inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
		inventory.SetMaterialSampler(scene.material, "albedo_sampler", render.SamplerDescriptor{MagFilter: render.FilterModeLinear, AddressModeU: render.AddressModeRepeat})
		inventory.SetMaterialUniform(scene.material, "tint", value)
		inventory.SetMaterialBuffer(scene.material, "lights", render.BufferRange{Buffer: scene.lights})
		inventory.SetMaterialTextureView(scene.material, "layers", render.TextureView{Texture: scene.layers, BaseMipLevel: 1, BaseLayer: 1})
		inventory.SetMaterialTexture(scene.material, "output", scene.layers)
		inventory.SetMaterialUniform(scene.material, "missing", value)
		inventory.SetMaterialTexture(scene.material, "missing", scene.albedo)
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	setCount := 0
	for _, command := range findPassCommands(h, "scene") {
		if command.Kind == recording.CommandKindSetBindGroup {
			setCount++
		}
	}
	if setCount != 2 {
		t.Errorf("the pass set %d bind groups for two draws of one material, want 2: @group(0) and @group(1), each once", setCount)
	}
	draw := findPassDraws(h, "scene")[0][1]
	want := []render.InspectedParameter{
		{Name: "albedo", TextureView: render.TextureView{Texture: scene.albedo, MipLevelCount: 1, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}},
		{Name: "albedo_sampler", Sampler: render.SamplerDescriptor{MagFilter: render.FilterModeLinear, AddressModeU: render.AddressModeRepeat}},
		{Name: "tint", Uniform: unsafe.Slice((*byte)(unsafe.Pointer(&value)), unsafe.Sizeof(value))},
		{Name: "lights", Buffer: render.BufferRange{Buffer: scene.lights, Size: 32}},
		{Name: "shadow", Default: true},
		{Name: "shadow_sampler", Default: true},
		{Name: "layers", TextureView: render.TextureView{Texture: scene.layers, BaseMipLevel: 1, MipLevelCount: 1, BaseLayer: 1, LayerCount: 2, Dimension: render.TextureViewDimension2DArray, Aspect: render.TextureAspectAll}},
		{Name: "indices", Default: true},
		{Name: "output", TextureView: render.TextureView{Texture: scene.layers, MipLevelCount: 1, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}},
	}
	if !reflect.DeepEqual(draw.Material.Parameters, want) {
		t.Errorf("the draw bound\n%+v\nwant\n%+v", draw.Material.Parameters, want)
	}
}

func TestAValueThatCannotFillItsBindingIsReportedOncePerShaderAndNameAndDrawsItsDefault(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	var scene parameterScene
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
			inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
			inventory.SetMaterialUniform(scene.material, "tint", tint{})
		} else {
			inventory.SetMaterialSampler(scene.material, "albedo", render.SamplerDescriptor{})
			inventory.SetMaterialUniform(scene.material, "tint", swappedTint{})
		}
		inventory.SetMaterialTextureView(scene.material, "layers", render.TextureView{Texture: scene.layers, Dimension: render.TextureViewDimension2D})
		inventory.SetMaterialTexture(scene.material, "indices", scene.albedo)
		inventory.SetMaterialBuffer(scene.material, "lights", render.BufferRange{Buffer: scene.smallLights})
		inventory.SetMaterialSampler(scene.material, "albedo_sampler", render.SamplerDescriptor{Compare: render.CompareFunctionLess})
		inventory.SetMaterialTextureView(scene.material, "shadow", render.TextureView{Texture: scene.shadowMap, BaseMipLevel: 1})
		inventory.SetMaterialUniform(scene.material, "camera", [16]float32{})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	if count := countReports(h, render.ReportKindParameterRefused); count != 8 {
		t.Errorf("render reported %d refused parameters, want 8: one for each of eight names, though set in two frames", count)
	}
	draws := findOnlyDraw(t, h, "scene")
	for frameIndex, draw := range draws {
		for _, name := range []string{"layers", "indices", "lights", "albedo_sampler", "shadow"} {
			if parameter, _ := findParameter(draw, name); !parameter.Default {
				t.Errorf("frame %d: %q bound %+v, want its default", frameIndex, name, parameter)
			}
		}
	}
	for name, wantDefaults := range map[string][]bool{"albedo": {false, true}, "tint": {false, true}} {
		for frameIndex, draw := range draws {
			if parameter, _ := findParameter(draw, name); parameter.Default != wantDefaults[frameIndex] {
				t.Errorf("frame %d: %q drew its default: %t, want %t: refused only in the second frame", frameIndex, name, parameter.Default, wantDefaults[frameIndex])
			}
		}
	}
}

func TestUnboundBindingsDrawTheDefaultsAndAnUnboundStorageBindingARunningFunctionReadsSkipsTheDraw(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	var scene parameterScene
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
		}
		reading := inventory.MakeMaterial(render.MaterialDescriptor{Label: "reading", Shader: inventory.AcquireShader("reading.wgsl")})
		passReading := inventory.MakeMaterial(render.MaterialDescriptor{Label: "pass reading", Shader: inventory.AcquireShader("passreading.wgsl")})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			for _, material := range []render.Material{scene.material, reading, passReading} {
				pass.SetMaterial(material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			}
		})
		inventory.ReleaseMaterial(reading)
		inventory.ReleaseMaterial(passReading)
	})

	if count := countReports(h, render.ReportKindDrawSkipped); count != 4 {
		t.Errorf("render reported %d skipped draws, want 4: one for each frame's material of reading.wgsl, whose particles a running function reads, and of passreading.wgsl, whose light_grid one does", count)
	}
	for frameIndex, draw := range findOnlyDraw(t, h, "scene") {
		for _, parameter := range draw.Material.Parameters {
			if !parameter.Default {
				t.Errorf("frame %d: %q bound %+v, want its default", frameIndex, parameter.Name, parameter)
			}
		}
		if len(draw.Material.Parameters) != 9 {
			t.Errorf("frame %d: the draw has %d parameters, want 9", frameIndex, len(draw.Material.Parameters))
		}
	}
	calls := h.Calls()
	var samplers []gfx.SamplerDescriptor
	var textures []gfx.TextureFormat
	whiteWritten := false
	farDepthWritten := false
	for _, call := range calls {
		switch arguments := call.Arguments.(type) {
		case recording.CreateSamplerArguments:
			samplers = append(samplers, arguments.Descriptor)
		case recording.CreateTextureArguments:
			if arguments.Descriptor.Label == "render: the default texture" {
				textures = append(textures, arguments.Descriptor.Format)
			}
		case recording.WriteTextureArguments:
			whiteWritten = whiteWritten || bytes.Equal(arguments.Data, bytes.Repeat([]byte{0xff}, 24))
			farDepthWritten = farDepthWritten || bytes.Equal(arguments.Data, bytes.Repeat([]byte{0xff}, 12))
		}
	}
	linear := gfx.SamplerDescriptor{Label: "render: the default sampler", AddressModeU: gfx.AddressModeClampToEdge, AddressModeV: gfx.AddressModeClampToEdge, AddressModeW: gfx.AddressModeClampToEdge, MagFilter: gfx.FilterModeLinear, MinFilter: gfx.FilterModeLinear, MipmapFilter: gfx.FilterModeLinear, LodMaxClamp: 32, Anisotropy: 1}
	lessEqual := linear
	lessEqual.Compare = gfx.CompareFunctionLessEqual
	if !slices.Contains(samplers, linear) || !slices.Contains(samplers, lessEqual) {
		t.Errorf("render made samplers %+v, want a linear one clamping to the edge and a less-equal comparison one", samplers)
	}
	if want := []gfx.TextureFormat{gfx.TextureFormatRGBA8Unorm, gfx.TextureFormatDepth16Unorm, gfx.TextureFormatRGBA8Uint, gfx.TextureFormatRGBA8Unorm}; !slices.Equal(textures, want) {
		t.Errorf("render made default textures of formats %v, want %v", textures, want)
	}
	if !whiteWritten || !farDepthWritten {
		t.Errorf("render wrote the default textures white: %t, and at depth 1.0: %t, want both", whiteWritten, farDepthWritten)
	}
	var cameraOffsets [][]uint32
	for _, command := range findPassCommands(h, "scene") {
		if arguments, ok := command.Arguments.(recording.SetBindGroupArguments); ok && arguments.Index == 0 {
			cameraOffsets = append(cameraOffsets, arguments.DynamicOffsets)
		}
	}
	if len(cameraOffsets) != 1 || !slices.Equal(cameraOffsets[0], []uint32{0}) {
		t.Errorf("the pass bound @group(0) at dynamic offsets %v, want once, at 0, for camera's zeros", cameraOffsets)
	}
}

func TestATextureOrBufferFreedUnderAMaterialUnbindsAtThatFramesEnd(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
			inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
			inventory.SetMaterialBuffer(scene.material, "lights", render.BufferRange{Buffer: scene.lights})
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		if frameIndex == 0 {
			inventory.ReleaseTexture(scene.albedo)
			inventory.ReleaseBuffer(scene.lights)
		}
	})

	draws := findOnlyDraw(t, h, "scene")
	for _, name := range []string{"albedo", "lights"} {
		freedFrameParameter, _ := findParameter(draws[0], name)
		nextFrameParameter, _ := findParameter(draws[1], name)
		if freedFrameParameter.Default || !nextFrameParameter.Default {
			t.Errorf("%q drew its default: %t in the frame it was freed, and %t in the next, want false, then true", name, freedFrameParameter.Default, nextFrameParameter.Default)
		}
	}
	if released := findCallIndices(h.Calls(), recording.CallKindReleaseBindGroup); len(released) == 0 {
		t.Error("render released no bind group, want the material's, which named the freed texture")
	}
}

func TestViewsAreInternedByTextureAndViewFreedWithTheirTextureAndReusedOverAPooledTemporary(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	var temporaries []render.Texture
	spawnParameterHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
		}
		second := inventory.MakeMaterial(render.MaterialDescriptor{Shader: scene.shader, FragmentEntryPoint: "fragment_main"})
		temporary := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "bloom", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm})
		temporaries = append(temporaries, temporary)
		for _, material := range []render.Material{scene.material, second} {
			inventory.SetMaterialTextureView(material, "layers", render.TextureView{Texture: scene.layers, BaseLayer: 1})
			inventory.SetMaterialTexture(material, "albedo", temporary)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.SetMaterial(second)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
		inventory.ReleaseMaterial(second)
		if frameIndex == 2 {
			inventory.ReleaseTexture(scene.layers)
		}
	})

	if temporaries[0] != temporaries[1] || temporaries[1] != temporaries[2] {
		t.Fatalf("the pool handed out temporaries %v, want one texture each frame, for its views to be reused", temporaries)
	}
	calls := h.Calls()
	views, _ := readTextureLevels(calls)
	viewCounts := map[render.Texture]int{}
	for _, level := range views {
		viewCounts[level.texture]++
	}
	for texture, label := range map[render.Texture]string{scene.layers: "layers", temporaries[0]: "the temporary"} {
		if count := viewCounts[texture]; count != 1 {
			t.Errorf("render made %d views of %s over three frames of two materials binding it, want one", count, label)
		}
	}
	releasedLayers := false
	for _, index := range findCallIndices(calls, recording.CallKindReleaseTextureView) {
		releasedLayers = releasedLayers || views[calls[index].Arguments.(gfx.TextureView)].texture == scene.layers
	}
	if !releasedLayers {
		t.Error("render released no view of layers, freed in the last frame, want its view freed with it")
	}
}

func TestATemporaryThePoolFreesUnderAMaterialUnbindsAtThatFramesEnd(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	h.Spawn(&render.Config{TemporaryIdleFrames: 1}, &loaderPlugin{files: files, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
			bloom := frame.AcquireTemporaryTexture(render.TextureDescriptor{Label: "bloom", Width: 4, Height: 4, Format: render.TextureFormatRGBA8Unorm})
			inventory.SetMaterialTexture(scene.material, "albedo", bloom)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for range 3 {
		h.Step()
	}

	// The pool frees the temporary at the end of the second frame, the first
	// it went unused through.
	for frameIndex, wantDefault := range []bool{false, false, true} {
		if albedo, _ := findParameter(findOnlyDraw(t, h, "scene")[frameIndex], "albedo"); albedo.Default != wantDefault {
			t.Errorf("frame %d: albedo drew its default: %t, want %t", frameIndex, albedo.Default, wantDefault)
		}
	}
}

func TestASampledTextureOverlappingATargetOfItsPassIsCaughtWhenBoundAndDrawsItsDefault(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	var scene parameterScene
	var other render.Texture
	spawnParameterHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeParameterScene(inventory)
			other = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "other", Width: 8, Height: 8, Format: render.TextureFormatRGBA8Unorm}, nil)
			inventory.SetMaterialTexture(scene.material, "albedo", scene.color)
			inventory.SetMaterialTexture(scene.material, "shadow", scene.depth)
		}
		list := frame.BeginCommandList("draws", drawOrder)
		for _, descriptor := range []render.PassDescriptor{
			scene.colorPass("into colour"),
			{Label: "into other", ColorTargets: []render.ColorTarget{{Texture: other}}, DepthStencil: render.DepthStencilTarget{Texture: scene.depth, ReadOnly: true}},
		} {
			pass := list.BeginPass(descriptor)
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.End()
		}
		list.End()
	})

	if count := countReports(h, render.ReportKindParameterRefused); count != 2 {
		t.Errorf("render reported %d refused parameters, want 2: albedo and shadow, each once, overlapping the targets of the pass into colour", count)
	}
	for frameIndex, draw := range findOnlyDraw(t, h, "into colour") {
		for _, name := range []string{"albedo", "shadow"} {
			if parameter, _ := findParameter(draw, name); !parameter.Default {
				t.Errorf("frame %d: in the pass into colour, %q bound %+v, want its default", frameIndex, name, parameter)
			}
		}
	}
	for frameIndex, draw := range findOnlyDraw(t, h, "into other") {
		albedo, _ := findParameter(draw, "albedo")
		shadow, _ := findParameter(draw, "shadow")
		if albedo.TextureView.Texture != scene.color || shadow.TextureView.Texture != scene.depth {
			t.Errorf("frame %d: in the pass into other, albedo bound %+v and shadow %+v, want colour, which it does not write, and depth, its read-only target", frameIndex, albedo, shadow)
		}
	}
}

func TestAValueChangedBetweenDrawsMakesANewBindGroupAndTheOldIsReleasedAtTheFramesEnd(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeParameterScene(inventory)
		inventory.SetMaterialTexture(scene.material, "albedo", scene.albedo)
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(scene.material)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		inventory.SetMaterialTexture(scene.material, "albedo", scene.layers)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	var materialGroups []gfx.BindGroup
	for _, command := range findPassCommands(h, "scene") {
		if arguments, ok := command.Arguments.(recording.SetBindGroupArguments); ok && arguments.Index == 1 {
			materialGroups = append(materialGroups, arguments.Group)
		}
	}
	if len(materialGroups) != 2 || materialGroups[0] == materialGroups[1] {
		t.Fatalf("the pass bound @group(1) %v, want two bind groups, one before the change and one after", materialGroups)
	}
	draws := findPassDraws(h, "scene")[0]
	albedoBefore, _ := findParameter(draws[0], "albedo")
	albedoAfter, _ := findParameter(draws[1], "albedo")
	if albedoBefore.TextureView.Texture != scene.albedo || albedoAfter.TextureView.Texture != scene.layers {
		t.Errorf("the draws bound albedo %v, then %v, want albedo, then layers", albedoBefore.TextureView.Texture, albedoAfter.TextureView.Texture)
	}
	calls := h.Calls()
	released := false
	for _, index := range findCallIndices(calls, recording.CallKindReleaseBindGroup) {
		released = released || calls[index].Arguments == any(materialGroups[0])
	}
	if !released {
		t.Error("render did not release the bind group the change replaced, want it released at the frame's end")
	}
}

func TestListsSettingAndDrawingOneMaterialAtOnceBindItsValues(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	var made sync.Once
	var scene parameterScene
	// nextOrder hands each recording task an order of its own.
	var nextOrder atomic.Int32
	files := makeDrawFiles(t)
	putFile(t, files, "material.wgsl", []byte("// material.wgsl"))
	putFile(t, files, "material.wgsl.json", []byte(materialDeclaration))
	h.Spawn(nil, &loaderPlugin{files: files, recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		made.Do(func() { scene = makeParameterScene(inventory) })
		order := nextOrder.Add(1)
		list := frame.BeginCommandList("draws", drawOrder+render.Order(order))
		pass := list.BeginPass(render.PassDescriptor{Label: "scene", ColorTargets: []render.ColorTarget{{Texture: scene.color}}})
		pass.SetMaterial(scene.material)
		for index := range 16 {
			texture := scene.albedo
			if (index+int(order))%2 == 0 {
				texture = scene.layers
			}
			inventory.SetMaterialTexture(scene.material, "albedo", texture)
			inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: float32(index)})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		}
		pass.End()
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	draws := findPassDraws(h, "scene")
	if len(draws) != 2*listCount {
		t.Fatalf("the reader read %d passes, want %d", len(draws), 2*listCount)
	}
	for passIndex, passDraws := range draws {
		for drawIndex, draw := range passDraws {
			if albedo, _ := findParameter(draw, "albedo"); albedo.TextureView.Texture != scene.albedo && albedo.TextureView.Texture != scene.layers {
				t.Errorf("pass %d, draw %d: albedo bound %+v, want one of the textures set", passIndex, drawIndex, albedo)
			}
		}
		if len(passDraws) != 16 {
			t.Errorf("pass %d drew %d times, want 16", passIndex, len(passDraws))
		}
	}
}

func TestAUniformBlockSetAfterABufferBindsTheMaterialsOwnBuffer(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene parameterScene
	spawnParameterHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeParameterScene(inventory)
		block := inventory.MakeBuffer("block", render.BufferUsageFlagUniform, make([]byte, 256))
		inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 5})
		inventory.SetMaterialBuffer(scene.material, "tint", render.BufferRange{Buffer: block})
		inventory.SetMaterialUniform(scene.material, "tint", tint{Strength: 5})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(scene.material)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	tint, _ := findParameter(findOnlyDraw(t, h, "scene")[0], "tint")
	if len(tint.Uniform) < 20 || readFloat32(tint.Uniform[16:]) != 5 {
		t.Errorf("tint bound %+v, want the block set last, of strength 5", tint)
	}
}
