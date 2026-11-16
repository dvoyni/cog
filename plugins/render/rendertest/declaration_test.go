package rendertest_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// litDeclaration declares a shader of every kind of binding, whose variant
// with INSTANCED and SKINNED=4 declares a binding of its own and a second
// vertex input.
const litDeclaration = `{
	"bindings": [
		{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 80, "members": [{"name": "view", "offset": 0, "size": 64}, {"name": "time", "offset": 64, "size": 4}]}, "entryPoints": ["vertex_main", "fragment_main"]},
		{"group": 1, "binding": 0, "name": "shadows", "texture": {"sampleType": "depth", "viewDimension": "2d-array", "multisampled": true}, "entryPoints": ["fragment_main"]},
		{"group": 1, "binding": 1, "name": "shadow_sampler", "sampler": {"type": "comparison"}},
		{"group": 2, "binding": 0, "name": "output", "storageTexture": {"access": "read-write", "format": "r32float", "viewDimension": "3d"}, "entryPoints": ["fragment_main"]}
	],
	"entryPoints": [{"name": "vertex_main", "stage": "vertex"}, {"name": "fragment_main", "stage": "fragment"}],
	"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}]},
	"variants": [
		{
			"defines": {"INSTANCED": "", "SKINNED": "4"},
			"bindings": [{"group": 2, "binding": 1, "name": "joints", "buffer": {"type": "read-only-storage", "size": 64}, "entryPoints": ["vertex_main"]}],
			"entryPoints": [{"name": "vertex_main", "stage": "vertex"}],
			"vertexInputs": {"vertex_main": [{"location": 0, "type": "vec3<f32>"}, {"location": 1, "type": "vec4<u32>"}]}
		}
	]
}`

// litBindings and litEntryPoints are the bindings and entry points
// litDeclaration declares, and skinnedBindings and skinnedEntryPoints those of
// its variant.
var (
	litEntryPoints     = []gfx.ShaderEntryPoint{{Name: "vertex_main", Stage: gfx.ShaderStageFlagVertex}, {Name: "fragment_main", Stage: gfx.ShaderStageFlagFragment}}
	skinnedEntryPoints = []gfx.ShaderEntryPoint{{Name: "vertex_main", Stage: gfx.ShaderStageFlagVertex}}

	litBindings = []gfx.ShaderBinding{
		{Name: "camera", Group: 0, Binding: 0, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 80}, Members: []gfx.ShaderStructMember{{Name: "view", Offset: 0, Size: 64}, {Name: "time", Offset: 64, Size: 4}}, EntryPoints: []string{"vertex_main", "fragment_main"}},
		{Name: "shadows", Group: 1, Binding: 0, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeDepth, ViewDimension: gfx.TextureViewDimension2DArray, Multisampled: true}, EntryPoints: []string{"fragment_main"}},
		{Name: "shadow_sampler", Group: 1, Binding: 1, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeComparison}},
		{Name: "output", Group: 2, Binding: 0, StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessReadWrite, Format: gfx.TextureFormatR32Float, ViewDimension: gfx.TextureViewDimension3D}, EntryPoints: []string{"fragment_main"}},
	}
	skinnedBindings = []gfx.ShaderBinding{
		{Name: "joints", Group: 2, Binding: 1, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage, MinBindingSize: 64}, EntryPoints: []string{"vertex_main"}},
	}
)

func TestAShadersReflectionIsAnsweredFromTheDeclarationBesideItForTheVariantOfItsDefines(t *testing.T) {
	files := fstest.MapFS{"lit.wgsl": {Data: []byte("// lit")}, "lit.wgsl.json": {Data: []byte(litDeclaration)}}
	tests := []struct {
		name            string
		defines         []gfx.ShaderDefine
		entryPoint      string
		wantBindings    []gfx.ShaderBinding
		wantEntryPoints []gfx.ShaderEntryPoint
		// wantPipelineError is what the error of a pipeline feeding
		// location 0 alone holds, empty for none.
		wantPipelineError string
	}{
		{name: "no defines", wantBindings: litBindings, wantEntryPoints: litEntryPoints},
		{name: "no defines, the entry point named", entryPoint: "vertex_main", wantBindings: litBindings, wantEntryPoints: litEntryPoints},
		{name: "the variant's defines in another order", defines: []gfx.ShaderDefine{{Name: "SKINNED", Value: "4"}, {Name: "INSTANCED"}}, wantBindings: skinnedBindings, wantEntryPoints: skinnedEntryPoints, wantPipelineError: "location 1 feeds the shader's input vec4<u32>"},
		{name: "a define set with no variant of its own", defines: []gfx.ShaderDefine{{Name: "INSTANCED"}}, wantBindings: litBindings, wantEntryPoints: litEntryPoints},
		{name: "an entry point the declaration lacks", entryPoint: "vertex_skinned", wantBindings: litBindings, wantEntryPoints: litEntryPoints, wantPipelineError: `lit.wgsl declares no vertex entry point "vertex_skinned"`},
	}
	h := rendertest.NewHarness(t)
	bindings := make([][]gfx.ShaderBinding, len(tests))
	entryPoints := make([][]gfx.ShaderEntryPoint, len(tests))
	pipelineErrors := make([]error, len(tests))
	h.Spawn(nil, &firstFramePlugin[kernel.Read[gfx.Device]]{onFirstFrame: func(_ *kernel.Kernel, deviceHandle kernel.Read[gfx.Device]) {
		device := deviceHandle.Get()
		// Every variant is preprocessed before any is compiled, as parallel
		// jobs may, their texts the same.
		texts := make([]string, len(tests))
		for index, test := range tests {
			var err error
			if texts[index], err = device.PreprocessShader(files, "lit.wgsl", test.defines); err != nil {
				t.Error(err)
			}
		}
		for index, test := range tests {
			module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: test.name, WGSL: texts[index]})
			if err != nil {
				t.Error(err)
			}
			if bindings[index], err = device.ShaderModuleBindings(module); err != nil {
				t.Errorf("%s: %v", test.name, err)
			}
			if entryPoints[index], err = device.ShaderModuleEntryPoints(module); err != nil {
				t.Errorf("%s: %v", test.name, err)
			}
			_, pipelineErrors[index] = createPipeline(device, module, test.entryPoint)
		}
	}})

	h.Step()

	for index, test := range tests {
		if fmt.Sprint(bindings[index]) != fmt.Sprint(test.wantBindings) {
			t.Errorf("%s: the reflection answered bindings\n%+v\nwant\n%+v", test.name, bindings[index], test.wantBindings)
		}
		if fmt.Sprint(entryPoints[index]) != fmt.Sprint(test.wantEntryPoints) {
			t.Errorf("%s: the reflection answered entry points %+v, want %+v", test.name, entryPoints[index], test.wantEntryPoints)
		}
		if test.wantPipelineError == "" && pipelineErrors[index] != nil || test.wantPipelineError != "" && (pipelineErrors[index] == nil || !strings.Contains(pipelineErrors[index].Error(), test.wantPipelineError)) {
			t.Errorf("%s: a pipeline feeding location 0 alone answered %v, want an error holding %q, empty for none", test.name, pipelineErrors[index], test.wantPipelineError)
		}
	}
}

func TestAReflectionAnswerQueuedWithAnswerNextWinsOverTheDeclarationOnce(t *testing.T) {
	files := fstest.MapFS{"lit.wgsl": {Data: []byte("// lit")}, "lit.wgsl.json": {Data: []byte(litDeclaration)}}
	queuedBindings := []gfx.ShaderBinding{{Name: "queued", Group: 0, Binding: 3, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}}}
	h := rendertest.NewHarness(t)
	h.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: queuedBindings})
	h.AnswerNext(recording.CallKindReflectVertexInputs, recording.Answer{VertexInputs: []gfx.VertexInput{{Location: 3, Type: gfx.ScalarTypeUint32, Components: 1}}})
	var bindings [2][]gfx.ShaderBinding
	var pipelineErrors [2]error
	h.Spawn(nil, &firstFramePlugin[kernel.Read[gfx.Device]]{onFirstFrame: func(_ *kernel.Kernel, deviceHandle kernel.Read[gfx.Device]) {
		device := deviceHandle.Get()
		module := compileShader(t, device, files)
		for index := range 2 {
			bindings[index], _ = device.ShaderModuleBindings(module)
			_, pipelineErrors[index] = createPipeline(device, module, "")
		}
	}})

	h.Step()

	if want := [2][]gfx.ShaderBinding{queuedBindings, litBindings}; fmt.Sprint(bindings) != fmt.Sprint(want) {
		t.Errorf("two reflections of a module's bindings answered\n%+v\nwant\n%+v\nthe answer queued, then the declaration's", bindings, want)
	}
	if !errors.Is(pipelineErrors[0], gfx.ErrVertexMismatch) || !strings.Contains(pipelineErrors[0].Error(), "location 3") || pipelineErrors[1] != nil {
		t.Errorf("two pipelines feeding location 0 alone answered %v, want an error naming location 3, which the inputs queued read, then none", pipelineErrors)
	}
}

func TestAShaderCompiledWithNoDeclarationOrOneThatFailsToParseFailsTheTestNamingItsPath(t *testing.T) {
	for _, test := range []struct {
		name        string
		declaration string
		// preprocessed is whether the module is made from the text
		// PreprocessShader answered, rather than WGSL of its own.
		preprocessed bool
		wantFailure  string
	}{
		{name: "no declaration", preprocessed: true, wantFailure: "lit.wgsl was compiled with no binding declaration beside it, lit.wgsl.json"},
		{name: "a field the format lacks", declaration: `{"binding": []}`, preprocessed: true, wantFailure: `lit.wgsl.json: json: unknown field "binding"`},
		{name: "a value no gfx value spells", declaration: `{"bindings": [{"name": "camera", "buffer": {"type": "uniforms"}}]}`, preprocessed: true, wantFailure: `lit.wgsl.json: binding "camera": "uniforms" is no buffer type`},
		{name: "two kinds", declaration: `{"bindings": [{"name": "camera", "buffer": {"type": "uniform"}, "sampler": {"type": "filtering"}}]}`, preprocessed: true, wantFailure: `lit.wgsl.json: binding "camera": declares 2 of buffer, sampler, texture and storageTexture`},
		{name: "a vertex input of no type", declaration: `{"vertexInputs": {"vertex_main": [{"location": 2, "type": "vec5<f32>"}]}}`, preprocessed: true, wantFailure: `lit.wgsl.json: vertex entry point "vertex_main": location 2: "vec5<f32>" is no vector type`},
		{name: "WGSL the harness did not preprocess", declaration: "{}", wantFailure: `shader module "raw" was made from WGSL the harness did not preprocess`},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{"lit.wgsl": {Data: []byte("// lit")}}
			if test.declaration != "" {
				files["lit.wgsl.json"] = &fstest.MapFile{Data: []byte(test.declaration)}
			}
			fake := &fakeTest{TB: t}
			h := rendertest.NewHarness(fake)
			h.Spawn(nil, &firstFramePlugin[kernel.Read[gfx.Device]]{onFirstFrame: func(_ *kernel.Kernel, deviceHandle kernel.Read[gfx.Device]) {
				device := deviceHandle.Get()
				if test.preprocessed {
					compileShader(t, device, files)
				} else if _, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "raw", WGSL: "// lit"}); err != nil {
					t.Error(err)
				}
			}})

			h.Step()

			if failures := fake.takeFailures(); len(failures) != 1 || !strings.Contains(failures[0], test.wantFailure) {
				t.Errorf("the harness failed the test with %q, want one failure holding %q", failures, test.wantFailure)
			}
		})
	}
}

// compileShader preprocesses lit.wgsl in files with no defines and makes
// its module, failing t on an error.
func compileShader(t *testing.T, device gfx.Device, files fstest.MapFS) gfx.ShaderModule {
	text, err := device.PreprocessShader(files, "lit.wgsl", nil)
	if err != nil {
		t.Error(err)
	}
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "lit.wgsl", WGSL: text})
	if err != nil {
		t.Error(err)
	}
	return module
}

// createPipeline makes a render pipeline of module's vertex entry point
// named entryPoint, an empty name its only one, its buffers feeding location
// 0 alone, with a vec3<f32>, and its fragment entry point drawing into no
// target.
func createPipeline(device gfx.Device, module gfx.ShaderModule, entryPoint string) (gfx.RenderPipeline, error) {
	buffers := []gfx.VertexBufferLayout{{ArrayStride: 12, Attributes: []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x3, ShaderLocation: 0}}}}
	return device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
		Vertex:      gfx.VertexState{Module: module, EntryPoint: entryPoint, Buffers: buffers},
		Multisample: gfx.MultisampleState{Count: 1, Mask: ^uint64(0)},
		Fragment:    gfx.FragmentState{Module: module, EntryPoint: "fragment_main"},
	})
}
