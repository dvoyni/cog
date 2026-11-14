package rendertest_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/filesystems"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// everyStage is the visibility of a binding every stage may see, and
// noVertexStage that of one the vertex stage may not: a storage buffer or a
// storage texture a shader may write.
const (
	everyStage    = gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment | gfx.ShaderStageFlagCompute
	noVertexStage = gfx.ShaderStageFlagFragment | gfx.ShaderStageFlagCompute
)

func TestAcquiresOfOnePathWithTheSameDefinesInAnyOrderShareOneVariantCompiledAtOnceWithThemSortedByName(t *testing.T) {
	h := rendertest.NewHarness(t)
	files := filesystems.NewMemoryFS()
	putFile(t, files, "shaders/lit.wgsl", []byte("// lit"))
	// shadowed is the first frame's acquire of the lit shader with shadows
	// and instancing, reordered one of the same defines in another order, a
	// name given twice keeping its last value, instanced the acquire with
	// instancing alone, and plain the one with no defines; compiledAtOnce is
	// whether shadowed's module was made when its acquire returned.
	var shadowed, reordered, instanced, plain render.Shader
	var compiledAtOnce bool
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				shadowed = inventory.AcquireShader("shaders/lit.wgsl", render.ShaderDefine{Name: "SHADOWS", Value: "1"}, render.ShaderDefine{Name: "INSTANCED"})
				_, compiledAtOnce = readShaderCalls(h.Calls()).findCreated[recording.CreateShaderModuleArguments](shadowed)
				reordered = inventory.AcquireShader("shaders/lit.wgsl", render.ShaderDefine{Name: "INSTANCED"}, render.ShaderDefine{Name: "SHADOWS", Value: "0"}, render.ShaderDefine{Name: "SHADOWS", Value: "1"})
				instanced = inventory.AcquireShader("shaders/lit.wgsl", render.ShaderDefine{Name: "INSTANCED"})
				plain = inventory.AcquireShader("shaders/lit.wgsl")
			case 1:
				for _, shader := range []render.Shader{shadowed, reordered, instanced, plain} {
					inventory.ReleaseShader(shader)
				}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	if reordered != shadowed {
		t.Errorf("an acquire listing the same defines in another order answered %v, want %v, the first's", reordered, shadowed)
	}
	if variants := []render.Shader{shadowed, instanced, plain}; slices.Contains(variants, render.Shader{}) || instanced == shadowed || plain == shadowed || plain == instanced {
		t.Errorf("acquires of three define sets answered %v, want three variants", variants)
	}
	if !compiledAtOnce {
		t.Error("the variant's module was not made when its acquire returned")
	}
	calls := h.Calls()
	var preprocessedDefines [][]gfx.ShaderDefine
	for _, index := range findCallIndices(calls, recording.CallKindPreprocessShader) {
		preprocessedDefines = append(preprocessedDefines, calls[index].Arguments.(recording.PreprocessShaderArguments).Defines)
	}
	if want := [][]gfx.ShaderDefine{{{Name: "INSTANCED"}, {Name: "SHADOWS", Value: "1"}}, {{Name: "INSTANCED"}}, nil}; fmt.Sprint(preprocessedDefines) != fmt.Sprint(want) {
		t.Errorf("render preprocessed with defines %v, want %v: each variant once, its defines sorted by name", preprocessedDefines, want)
	}
	shaderCalls := readShaderCalls(calls)
	for _, shader := range []render.Shader{shadowed, instanced, plain} {
		if frameIndex, freed := shaderCalls.findFreeingFrame(shader); !freed || frameIndex != 1 {
			t.Errorf("variant %v was freed: %v, at the end of frame %d, want once, at the end of frame 1, where its last acquire was released", shader, freed, frameIndex)
		}
	}
}

func TestAShadersGroupLayoutsHoldEveryBindingItDeclaresSharedWithOtherShadersAndFreedWithTheLast(t *testing.T) {
	h := rendertest.NewHarness(t)
	files := filesystems.NewMemoryFS()
	for _, name := range []string{"lit", "unlit", "flat", "plain"} {
		putFile(t, files, "shaders/"+name+".wgsl", []byte("// "+name))
	}
	camera := gfx.ShaderBinding{Name: "camera", Group: 0, Binding: 0, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 64}}
	lights := gfx.ShaderBinding{Name: "lights", Group: 0, Binding: 1, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage, MinBindingSize: 32}}
	object := gfx.ShaderBinding{Name: "object", Group: 2, Binding: 0, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 64}}
	litBindings := []gfx.ShaderBinding{
		camera,
		lights,
		{Name: "albedo", Group: 1, Binding: 0, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D}},
		{Name: "linear", Group: 1, Binding: 1, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
		object,
		{Name: "instances", Group: 2, Binding: 1, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage, MinBindingSize: 16}},
		{Name: "output", Group: 2, Binding: 2, StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatRGBA8Unorm, ViewDimension: gfx.TextureViewDimension2D}},
	}
	// unlit and flat declare the same bindings, out of their numbers' order,
	// and none in group 1; plain declares none.
	unlitBindings := []gfx.ShaderBinding{object, lights, camera}
	for _, bindings := range [][]gfx.ShaderBinding{litBindings, unlitBindings, unlitBindings, nil} {
		h.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: bindings})
	}
	var lit, unlit, flat, plain render.Shader
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				lit = inventory.AcquireShader("shaders/lit.wgsl")
				unlit = inventory.AcquireShader("shaders/unlit.wgsl")
				flat = inventory.AcquireShader("shaders/flat.wgsl")
				plain = inventory.AcquireShader("shaders/plain.wgsl")
			case 1:
				inventory.ReleaseShader(lit)
				inventory.ReleaseShader(flat)
			case 2:
				inventory.ReleaseShader(unlit)
				inventory.ReleaseShader(plain)
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()
	liveAfterFirstRelease := readLiveLayouts(h.Calls())
	h.Step()

	calls := h.Calls()
	var groupDescriptors []gfx.BindGroupLayoutDescriptor
	groupIndices := map[gfx.BindGroupLayout]int{}
	for _, index := range findCallIndices(calls, recording.CallKindCreateBindGroupLayout) {
		created := calls[index].Arguments.(recording.CreateBindGroupLayoutArguments)
		groupIndices[created.Layout] = len(groupDescriptors)
		created.Descriptor.Label = ""
		groupDescriptors = append(groupDescriptors, created.Descriptor)
	}
	litGroups := []gfx.BindGroupLayoutDescriptor{
		{Entries: []gfx.BindGroupLayoutEntry{
			{Binding: 0, Visibility: everyStage, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 64}},
			{Binding: 1, Visibility: everyStage, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage, MinBindingSize: 32}},
		}},
		{Entries: []gfx.BindGroupLayoutEntry{
			{Binding: 0, Visibility: everyStage, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D}},
			{Binding: 1, Visibility: everyStage, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
		}},
		{Entries: []gfx.BindGroupLayoutEntry{
			{Binding: 0, Visibility: everyStage, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 64}},
			{Binding: 1, Visibility: noVertexStage, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage, HasDynamicOffset: true, MinBindingSize: 16}},
			{Binding: 2, Visibility: noVertexStage, StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatRGBA8Unorm, ViewDimension: gfx.TextureViewDimension2D}},
		}},
	}
	unlitGroups := []gfx.BindGroupLayoutDescriptor{
		{},
		{Entries: []gfx.BindGroupLayoutEntry{
			{Binding: 0, Visibility: everyStage, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 64}},
		}},
	}
	if want := slices.Concat(litGroups, unlitGroups); fmt.Sprint(groupDescriptors) != fmt.Sprint(want) {
		t.Errorf("render made group layouts\n%+v\nwant\n%+v\nlit's three, each of every binding of its group, sorted by number, uniform blocks of groups 0 and 2 and storage buffers of group 2 at dynamic offsets, then unlit's empty group 1 and its group 2, its group 0 lit's, and none for flat", groupDescriptors, want)
	}
	var pipelineGroups [][]int
	for _, index := range findCallIndices(calls, recording.CallKindCreatePipelineLayout) {
		var groups []int
		for _, layout := range calls[index].Arguments.(recording.CreatePipelineLayoutArguments).Descriptor.BindGroupLayouts {
			groups = append(groups, groupIndices[layout])
		}
		pipelineGroups = append(pipelineGroups, groups)
	}
	if want := [][]int{{0, 1, 2}, {0, 3, 4}, nil}; fmt.Sprint(pipelineGroups) != fmt.Sprint(want) {
		t.Errorf("render made pipeline layouts of the group layouts made %v, want %v: lit's, unlit's, which flat shares, and plain's of no groups", pipelineGroups, want)
	}
	if want := (layoutSet{groupCount: 3, pipelineCount: 2}); liveAfterFirstRelease != want {
		t.Errorf("once lit and flat were freed, %+v layouts were live, want %+v: unlit's and plain's, lit's group 0 among them", liveAfterFirstRelease, want)
	}
	if liveLayouts := readLiveLayouts(calls); liveLayouts != (layoutSet{}) {
		t.Errorf("once every shader was freed, %+v layouts were live, want none", liveLayouts)
	}
}

func TestAShaderDeclaringGroup3OrPastTheDevicesLimitsIsRefusedAndReportedOncePerKey(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindShaderRefused)
	buffers := func(group, first, count uint32, bindingType gfx.BufferBindingType) []gfx.ShaderBinding {
		var bindings []gfx.ShaderBinding
		for binding := first; binding < first+count; binding++ {
			bindings = append(bindings, gfx.ShaderBinding{Name: fmt.Sprintf("b%d_%d", group, binding), Group: group, Binding: binding, Buffer: gfx.BufferBindingLayout{Type: bindingType, MinBindingSize: 16}})
		}
		return bindings
	}
	shaders := []struct {
		name     string
		bindings []gfx.ShaderBinding
		refused  bool
	}{
		{name: "group 3", bindings: buffers(3, 0, 1, gfx.BufferBindingTypeUniform), refused: true},
		{name: "9 dynamic uniform blocks", bindings: slices.Concat(buffers(0, 0, 5, gfx.BufferBindingTypeUniform), buffers(2, 0, 4, gfx.BufferBindingTypeUniform)), refused: true},
		{name: "5 dynamic storage buffers", bindings: buffers(2, 0, 5, gfx.BufferBindingTypeReadOnlyStorage), refused: true},
		{name: "9 storage buffers in a stage", bindings: buffers(0, 0, 9, gfx.BufferBindingTypeReadOnlyStorage), refused: true},
		// at every limit has 8 dynamic uniform blocks, 4 dynamic storage
		// buffers, and 8 storage buffers in the fragment stage.
		{name: "at every limit", bindings: slices.Concat(
			buffers(0, 0, 4, gfx.BufferBindingTypeUniform), buffers(0, 4, 4, gfx.BufferBindingTypeReadOnlyStorage),
			buffers(1, 0, 4, gfx.BufferBindingTypeUniform),
			buffers(2, 0, 4, gfx.BufferBindingTypeUniform), buffers(2, 4, 4, gfx.BufferBindingTypeStorage))},
	}
	files := filesystems.NewMemoryFS()
	for _, shader := range shaders {
		putFile(t, files, shader.name+".wgsl", []byte("// "+shader.name))
		h.AnswerNext(recording.CallKindReflectBindings, recording.Answer{Bindings: shader.bindings})
	}
	acquiredShaders := make([][2]render.Shader, len(shaders))
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex == 0 {
				for index, shader := range shaders {
					acquiredShaders[index] = [2]render.Shader{inventory.AcquireShader(shader.name + ".wgsl"), inventory.AcquireShader(shader.name + ".wgsl")}
				}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	for index, shader := range shaders {
		if acquiredShaders[index][0] == (render.Shader{}) || acquiredShaders[index][1] != acquiredShaders[index][0] {
			t.Errorf("two acquires of the shader of %s answered %v, want one shader", shader.name, acquiredShaders[index])
		}
	}
	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindShaderRefused}, 4); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: each refused shader once", kinds, want)
	}
	calls := h.Calls()
	if created := findCallIndices(calls, recording.CallKindCreatePipelineLayout); len(created) != 1 {
		t.Errorf("render made %d pipeline layouts, want one, of the shader at every limit", len(created))
	}
	shaderCalls := readShaderCalls(calls)
	for index, shader := range shaders {
		released := len(shaderCalls.find(recording.CallKindReleaseShaderModule, acquiredShaders[index][0])) == 1
		if released != shader.refused {
			t.Errorf("the module of the shader of %s was released at once: %v, want %v", shader.name, released, shader.refused)
		}
	}
	if liveLayouts, want := readLiveLayouts(calls), (layoutSet{groupCount: 3, pipelineCount: 1}); liveLayouts != want {
		t.Errorf("%+v layouts were live, want %+v: those of the shader at every limit alone", liveLayouts, want)
	}
}

func TestAShaderThatFailsToPreprocessOrCompileIsReportedOncePerKeyUntilFreed(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindLoadFailed)
	files := fstest.MapFS{"broken.wgsl": {Data: []byte("fn (")}}
	h.AnswerNext(recording.CallKindCreateShaderModule, recording.Answer{Error: errors.New("invalid WGSL")})
	// missing is the first frame's shader of a missing file, and broken that
	// of a file that fails to compile.
	var missing, broken render.Shader
	var reportCountAfterTheFirstAcquire int
	h.Spawn(nil, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			switch frameIndex {
			case 0:
				missing = inventory.AcquireShader("missing.wgsl")
				reportCountAfterTheFirstAcquire = len(h.Reports())
				inventory.ReleaseShader(inventory.AcquireShader("missing.wgsl"))
				inventory.ReleaseShader(missing)
				broken = inventory.AcquireShader("broken.wgsl")
				inventory.ReleaseShader(inventory.AcquireShader("broken.wgsl"))
			case 1:
				inventory.ReleaseShader(inventory.AcquireShader("missing.wgsl"))
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	if reportCountAfterTheFirstAcquire != 1 {
		t.Errorf("render had logged %d reports when the acquire of a missing file returned, want 1", reportCountAfterTheFirstAcquire)
	}
	if missing == (render.Shader{}) || broken == (render.Shader{}) {
		t.Errorf("the shaders that failed are %v and %v, want shaders, whose draws are skipped", missing, broken)
	}
	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindLoadFailed}, 3); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: the missing file once in each frame, as it was freed between them, and the broken one once", kinds, want)
	}
	if created := findCallIndices(h.Calls(), recording.CallKindCreatePipelineLayout); len(created) != 0 {
		t.Errorf("render made %d pipeline layouts for shaders that failed, want none", len(created))
	}
}

func TestParallelAcquiresOfOneVariantShareOneShaderReleasingEveryOtherModuleCompiled(t *testing.T) {
	const acquireCount = 32
	h := rendertest.NewHarness(t)
	// mutex guards acquiredShaders, every shader the tasks acquired.
	var mutex sync.Mutex
	var acquiredShaders []render.Shader
	h.Spawn(nil, &loaderPlugin{
		files:              &overlappingFS{FS: fstest.MapFS{"lit.wgsl": {Data: []byte("// lit")}}, name: "lit.wgsl"},
		recordingTaskCount: 2,
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			for range acquireCount {
				switch frameIndex {
				case 0:
					shader := inventory.AcquireShader("lit.wgsl", render.ShaderDefine{Name: "SHADOWS"})
					mutex.Lock()
					acquiredShaders = append(acquiredShaders, shader)
					mutex.Unlock()
				case 1:
					mutex.Lock()
					shader := acquiredShaders[0]
					mutex.Unlock()
					inventory.ReleaseShader(shader)
				}
			}
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	afterAcquires := h.Calls()
	h.Step()
	h.Step()

	mutex.Lock()
	defer mutex.Unlock()
	if len(acquiredShaders) != 2*acquireCount || slices.ContainsFunc(acquiredShaders, func(shader render.Shader) bool { return shader != acquiredShaders[0] }) {
		t.Fatalf("two tasks acquiring one variant %d times each got %v, want one shader every time", acquireCount, acquiredShaders)
	}
	createdCount := len(findCallIndices(afterAcquires, recording.CallKindCreateShaderModule))
	releasedCount := len(findCallIndices(afterAcquires, recording.CallKindReleaseShaderModule))
	if createdCount-releasedCount != 1 {
		t.Errorf("after the acquires render had made %d modules and released %d, want one live", createdCount, releasedCount)
	}
	if liveLayouts := readLiveLayouts(afterAcquires); liveLayouts != (layoutSet{pipelineCount: 1}) {
		t.Errorf("after the acquires %+v layouts were live, want one pipeline layout of no groups", liveLayouts)
	}
	if frameIndex, freed := readShaderCalls(h.Calls()).findFreeingFrame(acquiredShaders[0]); !freed || frameIndex != 1 {
		t.Errorf("the shader was freed: %v, at the end of frame %d, want once, at the end of the frame of its last release, 1", freed, frameIndex)
	}
}

func TestAReleaseOfAShaderNoAcquireHoldsIsRefusedAndReported(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindReleaseRefused)
	h.Spawn(nil, &loaderPlugin{
		files: fstest.MapFS{"lit.wgsl": {Data: []byte("// lit")}},
		duringFrame: func(frameIndex int, inventory render.Inventory, _ render.Frame) {
			if frameIndex > 0 {
				return
			}
			lit := inventory.AcquireShader("lit.wgsl")
			inventory.ReleaseShader(lit)
			inventory.ReleaseShader(lit)
			inventory.ReleaseShader(render.Shader{})
		},
	})

	h.Step()
	h.Step()

	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindReleaseRefused}, 2); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v: a second release of a shader acquired once, and a release of the zero Shader", kinds, want)
	}
}

// layoutSet counts layouts: group layouts and pipeline layouts.
type layoutSet struct {
	groupCount, pipelineCount int
}

// readLiveLayouts answers how many group layouts and pipeline layouts calls
// made and did not release.
func readLiveLayouts(calls []recording.Call) layoutSet {
	return layoutSet{
		groupCount:    len(findCallIndices(calls, recording.CallKindCreateBindGroupLayout)) - len(findCallIndices(calls, recording.CallKindReleaseBindGroupLayout)),
		pipelineCount: len(findCallIndices(calls, recording.CallKindCreatePipelineLayout)) - len(findCallIndices(calls, recording.CallKindReleasePipelineLayout)),
	}
}
