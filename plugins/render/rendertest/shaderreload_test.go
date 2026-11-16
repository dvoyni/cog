package rendertest_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

const (
	// shadedDeclaration declares a shader whose material binds a texture,
	// its sampler and three uniform blocks, with two fragment functions, and
	// reshadedDeclaration the same shader after an edit: the tint block
	// moved first, the sampler, the wind block and the outline function
	// gone, and the fog block grown.
	shadedDeclaration = `{
		"bindings": [
			{"group": 1, "binding": 0, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 1, "name": "albedo_sampler", "sampler": {"type": "filtering"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 2, "name": "tint", "buffer": {"type": "uniform", "size": 32, "members": [{"name": "color", "offset": 0, "size": 16}, {"name": "strength", "offset": 16, "size": 4}]}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 3, "name": "fog", "buffer": {"type": "uniform", "size": 16}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 4, "name": "wind", "buffer": {"type": "uniform", "size": 16}, "entryPoints": ["fragment_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}, {"name": "fragment_outline", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
	reshadedDeclaration = `{
		"bindings": [
			{"group": 1, "binding": 0, "name": "tint", "buffer": {"type": "uniform", "size": 32, "members": [{"name": "color", "offset": 0, "size": 16}, {"name": "strength", "offset": 16, "size": 4}]}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 1, "name": "albedo", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["fragment_main"]},
			{"group": 1, "binding": 2, "name": "fog", "buffer": {"type": "uniform", "size": 32}, "entryPoints": ["fragment_main"]}
		],
		"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
		"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]}
	}`
)

// countMadePipelines answers how many pipelines each frame h inspected made.
func countMadePipelines(h *rendertest.Harness) []int {
	var counts []int
	for _, inspection := range h.Frames() {
		counts = append(counts, len(inspection.MadePipelines))
	}
	return counts
}

func TestChangingAnIncludedFileReloadsEveryVariantIncludingItAtTheNextFramesStartAndNoOther(t *testing.T) {
	h := rendertest.NewHarness(t)
	memory := filesystems.NewMemoryFS()
	putFile(t, memory, "lit.wgsl", []byte("#include ./common.wgsl\nfn lit() {}\n"))
	putFile(t, memory, "lit.wgsl.json", []byte(plainDeclaration))
	putFile(t, memory, "unlit.wgsl", []byte("fn unlit() {}\n"))
	putFile(t, memory, "unlit.wgsl.json", []byte(plainDeclaration))
	putFile(t, memory, "common.wgsl", []byte("fn common() {}\n"))
	files := &openCountingFS{files: memory}
	shadows := render.ShaderDefine{Name: "SHADOWS"}
	var lit, shadowedLit, unlit render.Shader
	var unlitOpenCount int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex == 0 {
				lit = inventory.AcquireShader("lit.wgsl")
				shadowedLit = inventory.AcquireShader("lit.wgsl", shadows)
				unlit = inventory.AcquireShader("unlit.wgsl")
			}
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			switch frameIndex {
			case 0:
				putFile(t, memory, "common.wgsl", []byte("fn common() { return; }\n"))
			case 1:
				putFile(t, memory, "shadow.wgsl", []byte("fn shadow() {}\n"))
				putFile(t, memory, "lit.wgsl", []byte("#include ./common.wgsl\n#include ./shadow.wgsl\nfn lit() {}\n"))
			case 2:
				putFile(t, memory, "shadow.wgsl", []byte("fn shadow() { return; }\n"))
			}
			openCount := files.countOpens("unlit.wgsl")
			inventory.ReloadChanged()
			unlitOpenCount += files.countOpens("unlit.wgsl") - openCount
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}

	calls := h.Calls()
	shaderCalls := readShaderCalls(calls)
	for _, want := range []struct {
		what         string
		shader       render.Shader
		createdCount int
	}{
		{what: "the variant including the changed files", shader: lit, createdCount: 4},
		{what: "its variant with SHADOWS", shader: shadowedLit, createdCount: 4},
		{what: "the shader including nothing changed", shader: unlit, createdCount: 1},
	} {
		created := shaderCalls.find(recording.CallKindCreateShaderModule, want.shader)
		var releasedPresentCounts []int
		for _, released := range shaderCalls.find(recording.CallKindReleaseShaderModule, want.shader) {
			releasedPresentCounts = append(releasedPresentCounts, released.presentCount)
		}
		if wantReleased := []int{1, 2, 3}[:want.createdCount-1]; len(created) != want.createdCount || !slices.Equal(releasedPresentCounts, wantReleased) {
			t.Errorf("%s made %d modules and released them after %v presents, want %d made and each but the last released after %v: each reload swapped in at the next frame's start, an include a reload added watched from then on", want.what, len(created), releasedPresentCounts, want.createdCount, wantReleased)
		}
	}
	shadowedPreprocessCount := 0
	for _, index := range findCallIndices(calls, recording.CallKindPreprocessShader) {
		if arguments := calls[index].Arguments.(recording.PreprocessShaderArguments); arguments.Path == "lit.wgsl" && slices.Equal(arguments.Defines, []gfx.ShaderDefine{gfx.ShaderDefine(shadows)}) {
			shadowedPreprocessCount++
		}
	}
	if shadowedPreprocessCount != 4 {
		t.Errorf("render preprocessed lit.wgsl with SHADOWS %d times, want 4: each reload with the variant's defines", shadowedPreprocessCount)
	}
	if unlitOpenCount != 0 {
		t.Errorf("ReloadChanged opened the unchanged unlit.wgsl %d times, want none", unlitOpenCount)
	}
}

func TestAReloadedShadersPipelinesAreMadeAgainAtTheirNextDrawAndItsMaterialsBindTheirValuesByName(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	h.ExpectReport(render.ReportKindMaterialRefused)
	files := makeDrawFiles(t)
	putFile(t, files, "shaded.wgsl", []byte("fn shaded() {}\n"))
	putFile(t, files, "shaded.wgsl.json", []byte(shadedDeclaration))
	albedoPNG, _ := makePNG(t, 4, 4, 255)
	wideAlbedoPNG, _ := makePNG(t, 8, 4, 255)
	putFile(t, files, "albedo.png", albedoPNG)
	value := tint{Color: [4]float32{1, 0.5, 0.25, 1}, Strength: 2}
	fog := [4]float32{1, 2, 3, 4}
	var scene drawScene
	var albedo render.Texture
	var materials [3]render.Material
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeDrawScene(inventory)
				shader := inventory.AcquireShader("shaded.wgsl")
				albedo = inventory.AcquireTexture("albedo.png", render.TextureOptions{})
				for index, fragmentEntryPoint := range []string{"fragment_main", "fragment_main", "fragment_outline"} {
					materials[index] = inventory.MakeMaterial(render.MaterialDescriptor{Shader: shader, FragmentEntryPoint: fragmentEntryPoint})
					inventory.SetMaterialTexture(materials[index], "albedo", albedo)
					inventory.SetMaterialSampler(materials[index], "albedo_sampler", render.SamplerDescriptor{MagFilter: render.FilterModeLinear})
					inventory.SetMaterialUniform(materials[index], "tint", value)
					inventory.SetMaterialUniform(materials[index], "fog", fog)
					inventory.SetMaterialUniform(materials[index], "wind", [4]float32{})
				}
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				for _, material := range materials {
					pass.SetMaterial(material)
					pass.Draw(scene.indexedMesh, render.DrawArgs{})
				}
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex < 3 {
				// The edit, undone, then made again.
				declaration := []string{reshadedDeclaration, shadedDeclaration, reshadedDeclaration}[frameIndex]
				putFile(t, files, "shaded.wgsl", []byte(strings.Repeat("\n", frameIndex+1)+"fn shaded() {}\n"))
				putFile(t, files, "shaded.wgsl.json", []byte(declaration))
				if frameIndex == 0 {
					// The texture, reloaded at another size beside the edit, refills
					// the materials it resolves and leaves the outline one refused.
					putFile(t, files, "albedo.png", wideAlbedoPNG)
				}
				inventory.ReloadChanged()
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Step()
	}

	draws := findPassDraws(h, "scene")
	if drawCounts := []int{len(draws[0]), len(draws[1]), len(draws[2]), len(draws[3])}; !slices.Equal(drawCounts, []int{3, 2, 3, 2}) {
		t.Fatalf("the frames drew %v draws, want [3 2 3 2]: the outline material refused while its function is gone", drawCounts)
	}
	if made := countMadePipelines(h); !slices.Equal(made, []int{2, 1, 2, 1}) {
		t.Errorf("the frames made %v pipelines, want [2 1 2 1]: the reloaded shader's pipelines made again at their next draws", made)
	}
	calls := h.Calls()
	if released := findCallIndices(calls, recording.CallKindReleaseRenderPipeline); len(released) != 5 {
		t.Errorf("render released %d pipelines, want 5: every one of each module replaced", len(released))
	}
	for _, frameIndex := range []int{1, 3} {
		for index, draw := range draws[frameIndex] {
			var names []string
			for _, parameter := range draw.Material.Parameters {
				names = append(names, parameter.Name)
			}
			if !slices.Equal(names, []string{"tint", "albedo", "fog"}) {
				t.Errorf("frame %d's draw %d bound %v, want the edited shader's bindings in their order, those it lacks gone", frameIndex, index, names)
			}
			if parameter, _ := findParameter(draw, "tint"); parameter.Default || !bytes.Equal(parameter.Uniform, unsafe.Slice((*byte)(unsafe.Pointer(&value)), unsafe.Sizeof(value))) {
				t.Errorf("frame %d's draw %d bound tint as %+v, want the bytes it was set to, at its new binding", frameIndex, index, parameter)
			}
			if parameter, _ := findParameter(draw, "albedo"); parameter.Default || parameter.TextureView.Texture != albedo {
				t.Errorf("frame %d's draw %d bound albedo as %+v, want the texture it was set to", frameIndex, index, parameter)
			}
			if parameter, _ := findParameter(draw, "fog"); !parameter.Default {
				t.Errorf("frame %d's draw %d bound fog as %+v, want its default: its block grew past the value", frameIndex, index, parameter)
			}
		}
	}
	for index, draw := range draws[2] {
		if parameter, _ := findParameter(draw, "fog"); parameter.Default || !bytes.Equal(parameter.Uniform, unsafe.Slice((*byte)(unsafe.Pointer(&fog)), unsafe.Sizeof(fog))) {
			t.Errorf("the third frame's draw %d bound fog as %+v, want the bytes it was set to: it fits again after the edit is undone", index, parameter)
		}
	}
	var releaseCounts []int
	for _, index := range findCallIndices(calls, recording.CallKindCreateBuffer) {
		if arguments := calls[index].Arguments.(recording.CreateBufferArguments); strings.HasSuffix(arguments.Descriptor.Label, "uniform block wind") {
			releaseCounts = append(releaseCounts, len(findBufferCalls(calls, recording.CallKindReleaseBuffer, arguments.Buffer)))
		}
	}
	if slices.Sort(releaseCounts); !slices.Equal(releaseCounts, []int{0, 1, 1}) {
		t.Errorf("the three wind blocks' buffers were released %v times, want once each but the outline material's: the edited shader lacks the block, and a refused material keeps its values", releaseCounts)
	}
	if refused := countReports(h, render.ReportKindParameterRefused); refused != 2 {
		t.Errorf("render reported %d refused values, want two: fog, once for the shader and its name at each edit, though two materials set it", refused)
	}
	if refused := countReports(h, render.ReportKindMaterialRefused); refused != 2 {
		t.Errorf("render reported %d refused materials, want two: the outline material at each edit", refused)
	}
}

func TestAShaderThatFailsToReloadKeepsDrawingWithItsLastGoodModuleReportedOncePerChange(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	files := makeDrawFiles(t)
	putFile(t, files, "lit.wgsl", []byte("fn lit() {}\n"))
	putFile(t, files, "lit.wgsl.json", []byte(plainDeclaration))
	var scene drawScene
	var lit render.Shader
	var material render.Material
	var reportCounts []int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeDrawScene(inventory)
				lit = inventory.AcquireShader("lit.wgsl")
				material = inventory.MakeMaterial(render.MaterialDescriptor{Shader: lit})
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetMaterial(material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			switch frameIndex {
			case 0:
				putFile(t, files, "lit.wgsl", []byte("#include ./extra.wgsl\nfn lit() {}\n"))
				inventory.ReloadChanged()
			case 1:
				inventory.ReloadChanged()
				putFile(t, files, "extra.wgsl", []byte("fn extra() {}\n"))
				inventory.ReloadChanged()
			case 2:
				putFile(t, files, "extra.wgsl", []byte("fn extra() { return; }\n"))
				inventory.ReloadChanged()
			}
			reportCounts = append(reportCounts, countReports(h, render.ReportKindLoadFailed))
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.AnswerNext(recording.CallKindCreateShaderModule, recording.Answer{Error: errors.New("invalid WGSL")})
	for range 3 {
		h.Step()
	}

	if !slices.Equal(reportCounts[:3], []int{1, 2, 2}) {
		t.Errorf("render had reported %v failed loads after each frame's reloads, want [1 2 2]: the include it lacks, then the include appearing and failing to compile, once each", reportCounts)
	}
	if draws := findPassDraws(h, "scene"); len(draws) != 4 || slices.ContainsFunc(draws, func(frameDraws []render.InspectedDraw) bool { return len(frameDraws) != 1 }) {
		t.Errorf("the frames drew %v, want one draw in each: a failed reload draws with the module the shader had", draws)
	}
	if made := countMadePipelines(h); !slices.Equal(made, []int{1, 0, 0, 1}) {
		t.Errorf("the frames made %v pipelines, want [1 0 0 1]: none until a reload succeeds", made)
	}
	if released := readShaderCalls(h.Calls()).find(recording.CallKindReleaseShaderModule, lit); len(released) != 1 || released[0].presentCount != 3 {
		t.Errorf("the shader's module was released %d times, want once, at the fourth frame's start", len(released))
	}
}

func TestAMaterialOfAShaderThatFailedAtItsAcquireDrawsOnceAReloadCompilesIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	h.ExpectReport(render.ReportKindDrawSkipped)
	files := makeDrawFiles(t)
	putFile(t, files, "lit.wgsl", []byte("#include ./common.wgsl\nfn lit() {}\n"))
	putFile(t, files, "lit.wgsl.json", []byte(plainDeclaration))
	var scene drawScene
	var material render.Material
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = makeDrawScene(inventory)
				material = inventory.MakeMaterial(render.MaterialDescriptor{Shader: inventory.AcquireShader("lit.wgsl")})
			}
			recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
				pass.SetMaterial(material)
				pass.Draw(scene.indexedMesh, render.DrawArgs{})
			})
		},
		afterFrame: func(frameIndex int, inventory render.Inventory) {
			if frameIndex == 0 {
				putFile(t, files, "common.wgsl", []byte("fn common() {}\n"))
				inventory.ReloadChanged()
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	if draws := findPassDraws(h, "scene"); len(draws) != 2 || len(draws[0]) != 0 || len(draws[1]) != 1 {
		t.Errorf("the frames drew %v, want none, then one: the include the shader lacked appearing reloads it", draws)
	}
	if kinds := makeReportKinds(h); !slices.Equal(kinds, []render.ReportKind{render.ReportKindLoadFailed, render.ReportKindDrawSkipped}) {
		t.Errorf("render reported %v, want the failed acquire and the skipped draw", kinds)
	}
}
