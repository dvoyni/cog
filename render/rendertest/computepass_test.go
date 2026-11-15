package rendertest_test

import (
	"reflect"
	"strconv"
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

// The compute pass tests' shaders: cull.wgsl's one compute function, cull,
// reads a camera's uniform block in @group(0), a storage buffer of objects
// in @group(1) and writes one of visible objects in @group(2), beside a
// uniform block of limits it does not read; kernels.wgsl's two,
// clear_counts and count, write a storage buffer of counts, and count reads
// a camera's uniform block; and blur.wgsl's one, blur, samples a source
// texture and writes an output storage texture.
const (
	cullDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 16, "members": [{"name": "position", "offset": 0, "size": 16}]}, "entryPoints": ["cull"]},
			{"group": 1, "binding": 0, "name": "objects", "buffer": {"type": "read-only-storage", "size": 16}, "entryPoints": ["cull"]},
			{"group": 2, "binding": 0, "name": "visible", "buffer": {"type": "storage", "size": 4}, "entryPoints": ["cull"]},
			{"group": 2, "binding": 1, "name": "limits", "buffer": {"type": "uniform", "size": 16}}
		],
		"entryPoints": [{"name": "cull", "stage": "compute"}]
	}`
	kernelsDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "camera", "buffer": {"type": "uniform", "size": 16, "members": [{"name": "position", "offset": 0, "size": 16}]}, "entryPoints": ["count"]},
			{"group": 0, "binding": 1, "name": "counts", "buffer": {"type": "storage", "size": 16}, "entryPoints": ["clear_counts", "count"]}
		],
		"entryPoints": [{"name": "clear_counts", "stage": "compute"}, {"name": "count", "stage": "compute"}]
	}`
	blurDeclaration = `{
		"bindings": [
			{"group": 0, "binding": 0, "name": "source", "texture": {"sampleType": "float", "viewDimension": "2d"}, "entryPoints": ["blur"]},
			{"group": 0, "binding": 1, "name": "output", "storageTexture": {"access": "write-only", "format": "rgba8unorm", "viewDimension": "2d"}, "entryPoints": ["blur"]}
		],
		"entryPoints": [{"name": "blur", "stage": "compute"}]
	}`
)

// cameraBlock is cull.wgsl's and kernels.wgsl's camera uniform block.
type cameraBlock struct {
	Position [4]float32
}

// spawnComputeHarness spawns h as spawnDeclaredHarness does, with the
// compute pass tests' shaders.
func spawnComputeHarness(t *testing.T, h *rendertest.Harness, frameCount int, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) {
	t.Helper()
	spawnDeclaredHarness(t, h, nil, map[string]string{"cull.wgsl": cullDeclaration, "kernels.wgsl": kernelsDeclaration, "blur.wgsl": blurDeclaration}, frameCount, duringFrame)
}

// recordComputePass records into frame, in a command list at drawOrder, a
// compute pass labelled label whose dispatches dispatch records.
func recordComputePass(frame render.Frame, label string, dispatch func(pass *render.ComputePass)) {
	list := frame.BeginCommandList("compute", drawOrder)
	pass := list.BeginComputePass(label)
	dispatch(&pass)
	pass.End()
	list.End()
}

// findDispatches answers the dispatches of each compute pass labelled label
// h's reader read, frame after frame.
func findDispatches(h *rendertest.Harness, label string) [][]render.InspectedDispatch {
	var dispatches [][]render.InspectedDispatch
	for _, inspection := range h.Frames() {
		for _, list := range inspection.CommandLists {
			for _, pass := range list.ComputePasses {
				if pass.Label == label {
					dispatches = append(dispatches, pass.Dispatches)
				}
			}
		}
	}
	return dispatches
}

// findComputePassBindGroups answers the bind groups each compute pass gfx
// submitted set, frame after frame.
func findComputePassBindGroups(h *rendertest.Harness) [][]recording.SetBindGroupArguments {
	var passes [][]recording.SetBindGroupArguments
	for _, frame := range h.RecordedFrames() {
		for _, buffer := range readSubmittedBuffers(frame) {
			for _, command := range buffer.Commands {
				if command.Kind != recording.CommandKindBeginComputePass {
					continue
				}
				var groups []recording.SetBindGroupArguments
				for _, passCommand := range command.Commands {
					if passCommand.Kind == recording.CommandKindSetBindGroup {
						groups = append(groups, passCommand.Arguments.(recording.SetBindGroupArguments))
					}
				}
				passes = append(passes, groups)
			}
		}
	}
	return passes
}

// findCreatedComputePipelines answers the descriptor of each compute
// pipeline gfx made, in order.
func findCreatedComputePipelines(calls []recording.Call) []gfx.ComputePipelineDescriptor {
	var descriptors []gfx.ComputePipelineDescriptor
	for _, index := range findCallIndices(calls, recording.CallKindCreateComputePipeline) {
		descriptors = append(descriptors, calls[index].Arguments.(recording.CreateComputePipelineArguments).Descriptor)
	}
	return descriptors
}

func TestADispatchBindsItsParametersByNameInAnyGroupWithUniformBlocksFromTheArenaUntilSetShaderResetsThem(t *testing.T) {
	h := rendertest.NewHarness(t)
	var cull, kernels render.Shader
	var objects, visible render.Buffer
	cameras := []cameraBlock{{Position: [4]float32{1, 2, 3, 4}}, {Position: [4]float32{5, 6, 7, 8}}}
	spawnComputeHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			cull = inventory.AcquireShader("cull.wgsl")
			kernels = inventory.AcquireShader("kernels.wgsl")
			objects = inventory.MakeBuffer("objects", render.BufferUsageFlagStorage, make([]float32, 8))
			visible = inventory.MakeBuffer("visible", render.BufferUsageFlagStorage, make([]uint32, 128))
		}
		recordComputePass(frame, "cull", func(pass *render.ComputePass) {
			pass.SetShader(cull, "")
			pass.SetUniform("camera", cameras[0])
			pass.SetBuffer("objects", render.BufferRange{Buffer: objects})
			pass.SetBuffer("visible", render.BufferRange{Buffer: visible, Size: 8})
			pass.Dispatch(4, 1, 1)
			pass.SetUniform("camera", cameras[1])
			pass.SetBuffer("visible", render.BufferRange{Buffer: visible, Offset: 256, Size: 8})
			pass.Dispatch(8, 2, 1)
			pass.SetShader(kernels, "count")
			pass.SetBuffer("counts", render.BufferRange{Buffer: visible, Size: 16})
			pass.Dispatch(1, 1, 1)
		})
	})

	makeCameraBytes := func(camera *cameraBlock) []byte { return unsafe.Slice((*byte)(unsafe.Pointer(camera)), 16) }
	want := []render.InspectedDispatch{
		{Shader: cull, EntryPoint: "cull", Parameters: []render.InspectedParameter{
			{Name: "camera", Group: 0, Binding: 0, Uniform: makeCameraBytes(&cameras[0])},
			{Name: "objects", Group: 1, Binding: 0, Buffer: render.BufferRange{Buffer: objects, Size: 32}},
			{Name: "visible", Group: 2, Binding: 0, Buffer: render.BufferRange{Buffer: visible, Size: 8}},
			{Name: "limits", Group: 2, Binding: 1, Default: true},
		}, X: 4, Y: 1, Z: 1},
		{Shader: cull, EntryPoint: "cull", Parameters: []render.InspectedParameter{
			{Name: "camera", Group: 0, Binding: 0, Uniform: makeCameraBytes(&cameras[1])},
			{Name: "objects", Group: 1, Binding: 0, Buffer: render.BufferRange{Buffer: objects, Size: 32}},
			{Name: "visible", Group: 2, Binding: 0, Buffer: render.BufferRange{Buffer: visible, Offset: 256, Size: 8}},
			{Name: "limits", Group: 2, Binding: 1, Default: true},
		}, X: 8, Y: 2, Z: 1},
		{Shader: kernels, EntryPoint: "count", Parameters: []render.InspectedParameter{
			{Name: "camera", Group: 0, Binding: 0, Default: true},
			{Name: "counts", Group: 0, Binding: 1, Buffer: render.BufferRange{Buffer: visible, Size: 16}},
		}, X: 1, Y: 1, Z: 1},
	}
	for frameIndex, dispatches := range findDispatches(h, "cull") {
		if !reflect.DeepEqual(dispatches, want) {
			t.Errorf("frame %d dispatched\n%+v\nwant\n%+v", frameIndex, dispatches, want)
		}
	}
	passes := findComputePassBindGroups(h)
	if len(passes) != 2 || !reflect.DeepEqual(passes[0], passes[1]) {
		t.Errorf("the compute passes set bind groups %+v, want the second frame's the first's, cached by what they bind", passes)
	}
}

func TestAnEmptyEntryPointPicksTheShadersOnlyComputeFunctionAndAMissingOrAmbiguousOneIsReportedOnceAndSkipped(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPipelineFailed)
	var cull, kernels render.Shader
	var counts, objects render.Buffer
	spawnComputeHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			cull = inventory.AcquireShader("cull.wgsl")
			kernels = inventory.AcquireShader("kernels.wgsl")
			counts = inventory.MakeBuffer("counts", render.BufferUsageFlagStorage, make([]uint32, 4))
			objects = inventory.MakeBuffer("objects", render.BufferUsageFlagStorage, make([]uint32, 4))
		}
		recordComputePass(frame, "kernels", func(pass *render.ComputePass) {
			pass.SetShader(kernels, "")
			pass.SetBuffer("counts", render.BufferRange{Buffer: counts})
			pass.Dispatch(1, 1, 1)
			pass.Dispatch(1, 1, 1)
			pass.SetShader(kernels, "missing")
			pass.SetBuffer("counts", render.BufferRange{Buffer: counts})
			pass.Dispatch(1, 1, 1)
			pass.SetShader(kernels, "clear_counts")
			pass.SetBuffer("counts", render.BufferRange{Buffer: counts})
			pass.Dispatch(2, 1, 1)
			pass.SetShader(cull, "")
			pass.SetBuffer("objects", render.BufferRange{Buffer: objects})
			pass.SetBuffer("visible", render.BufferRange{Buffer: counts})
			pass.Dispatch(3, 1, 1)
		})
	})

	if count := countReports(h, render.ReportKindPipelineFailed); count != 2 {
		t.Errorf("render reported %d failed pipelines over three frames, want two: the ambiguous entry point and the missing one, once each", count)
	}
	for frameIndex, dispatches := range findDispatches(h, "kernels") {
		var ran []string
		for _, dispatch := range dispatches {
			ran = append(ran, dispatch.EntryPoint)
		}
		if want := []string{"clear_counts", "cull"}; !reflect.DeepEqual(ran, want) {
			t.Errorf("frame %d dispatched %q, want %q: the ambiguous and missing entry points skipped, and cull.wgsl's only function picked", frameIndex, ran, want)
		}
	}
}

func TestAComputePipelineIsKeyedByItsVariantAndItsEntryPointAndFreedWithItsShader(t *testing.T) {
	h := rendertest.NewHarness(t)
	var kernels, wide render.Shader
	var counts render.Buffer
	spawnComputeHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0:
			kernels = inventory.AcquireShader("kernels.wgsl")
			wide = inventory.AcquireShader("kernels.wgsl", render.ShaderDefine{Name: "WIDE", Value: "1"})
			counts = inventory.MakeBuffer("counts", render.BufferUsageFlagStorage, make([]uint32, 4))
		case 2:
			return
		}
		recordComputePass(frame, "kernels", func(pass *render.ComputePass) {
			for _, run := range []struct {
				shader     render.Shader
				entryPoint string
			}{{kernels, "count"}, {kernels, "clear_counts"}, {kernels, "count"}, {wide, "count"}} {
				pass.SetShader(run.shader, run.entryPoint)
				pass.SetBuffer("counts", render.BufferRange{Buffer: counts})
				pass.Dispatch(1, 1, 1)
			}
		})
		if frameIndex == 1 {
			inventory.ReleaseShader(kernels)
			inventory.ReleaseShader(wide)
		}
	})

	calls := h.Calls()
	if released := len(findCallIndices(calls, recording.CallKindReleaseComputePipeline)); released != 3 {
		t.Errorf("render released %d compute pipelines once their shaders were freed, want all three", released)
	}
	pipelines := findCreatedComputePipelines(calls)
	if len(pipelines) != 3 {
		t.Fatalf("render made %d compute pipelines over two frames, want three: count and clear_counts of the shader, and count of its variant", len(pipelines))
	}
	if pipelines[0].Module == pipelines[2].Module || pipelines[0].EntryPoint != "count" || pipelines[1].EntryPoint != "clear_counts" || pipelines[2].EntryPoint != "count" {
		t.Errorf("render made compute pipelines %+v, want count and clear_counts of one module, then count of another", pipelines)
	}
	if dispatches := findDispatches(h, "kernels"); len(dispatches) != 2 || len(dispatches[1]) != 4 {
		t.Errorf("the frames dispatched %+v, want four dispatches in each of two", dispatches)
	}
}

func TestAComputePassBindsTextureViewsAndAStorageTextureBindingSeesOneMipLevel(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindParameterRefused)
	h.ExpectReport(render.ReportKindDispatchSkipped)
	var blur render.Shader
	var source, image render.Texture
	spawnComputeHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			blur = inventory.AcquireShader("blur.wgsl")
			source = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "source", Width: 4, Height: 4, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm}, nil)
			image = inventory.MakeTexture[byte](render.TextureDescriptor{Label: "image", Width: 4, Height: 4, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm}, nil)
		}
		recordComputePass(frame, "blur", func(pass *render.ComputePass) {
			pass.SetShader(blur, "")
			pass.SetTextureView("source", render.TextureView{Texture: source, BaseMipLevel: 1})
			pass.SetTexture("output", image)
			pass.Dispatch(1, 1, 1)
			pass.SetTextureView("output", render.TextureView{Texture: image, BaseMipLevel: 1})
			pass.Dispatch(2, 1, 1)
			pass.SetTextureView("output", render.TextureView{Texture: image, BaseMipLevel: 2})
			pass.Dispatch(3, 1, 1)
		})
	})

	if count := countReports(h, render.ReportKindParameterRefused); count != 1 {
		t.Errorf("render reported %d refused parameters over two frames, want one: the view of two mip levels bound for storage", count)
	}
	if count := countReports(h, render.ReportKindDispatchSkipped); count != 1 {
		t.Errorf("render reported %d skipped dispatches over two frames, want one: the dispatch whose output drew its default", count)
	}
	makeView := func(texture render.Texture, baseMipLevel, mipLevelCount uint32) render.TextureView {
		return render.TextureView{Texture: texture, BaseMipLevel: baseMipLevel, MipLevelCount: mipLevelCount, LayerCount: 1, Dimension: render.TextureViewDimension2D, Aspect: render.TextureAspectAll}
	}
	want := []render.InspectedDispatch{
		{Shader: blur, EntryPoint: "blur", Parameters: []render.InspectedParameter{{Name: "source", Group: 0, Binding: 0, TextureView: makeView(source, 1, 2)}, {Name: "output", Group: 0, Binding: 1, TextureView: makeView(image, 0, 1)}}, X: 1, Y: 1, Z: 1},
		{Shader: blur, EntryPoint: "blur", Parameters: []render.InspectedParameter{{Name: "source", Group: 0, Binding: 0, TextureView: makeView(source, 1, 2)}, {Name: "output", Group: 0, Binding: 1, TextureView: makeView(image, 2, 1)}}, X: 3, Y: 1, Z: 1},
	}
	for frameIndex, dispatches := range findDispatches(h, "blur") {
		if !reflect.DeepEqual(dispatches, want) {
			t.Errorf("frame %d dispatched\n%+v\nwant\n%+v", frameIndex, dispatches, want)
		}
	}
}

func TestADispatchWithNoShaderWhoseShaderIsFreedOrWhoseParametersSetShaderResetIsSkippedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDispatchSkipped)
	var cull, kernels render.Shader
	var counts render.Buffer
	spawnComputeHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			cull = inventory.AcquireShader("cull.wgsl")
			inventory.ReleaseShader(cull)
			kernels = inventory.AcquireShader("kernels.wgsl")
			counts = inventory.MakeBuffer("counts", render.BufferUsageFlagStorage, make([]uint32, 4))
		}
		recordComputePass(frame, "skips", func(pass *render.ComputePass) {
			pass.Dispatch(1, 1, 1)
			pass.Dispatch(1, 1, 1)
			if frameIndex > 0 {
				pass.SetShader(cull, "")
				pass.Dispatch(1, 1, 1)
			}
			pass.SetShader(kernels, "count")
			pass.SetBuffer("counts", render.BufferRange{Buffer: counts})
			pass.Dispatch(2, 1, 1)
			// Selecting the same function again resets its parameters, so the
			// counts it writes are unbound.
			pass.SetShader(kernels, "count")
			pass.Dispatch(3, 1, 1)
		})
	})

	if count := countReports(h, render.ReportKindDispatchSkipped); count != 3 {
		t.Errorf("render reported %d skipped dispatches over three frames, want three: one with no shader set, one of the freed shader, one of count with its counts reset", count)
	}
	for frameIndex, dispatches := range findDispatches(h, "skips") {
		if len(dispatches) != 1 || dispatches[0].X != 2 {
			t.Errorf("frame %d dispatched %+v, want the one dispatch of count with its counts bound", frameIndex, dispatches)
		}
	}
}

func TestListsDispatchingAndCopyingAtOnceMakeAndShareComputePipelinesAndBindTheirOwnParameters(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	var made sync.Once
	var kernels render.Shader
	var culls [listCount]render.Shader
	var objects, visible [listCount]render.Buffer
	// nextOrder hands each recording task an order of its own.
	var nextOrder atomic.Int32
	files := makeDrawFiles(t)
	for name, declaration := range map[string]string{"cull.wgsl": cullDeclaration, "kernels.wgsl": kernelsDeclaration} {
		putFile(t, files, name, []byte("// "+name))
		putFile(t, files, name+".json", []byte(declaration))
	}
	h.Spawn(nil, &loaderPlugin{files: files, recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		made.Do(func() {
			kernels = inventory.AcquireShader("kernels.wgsl")
			for index := range objects {
				// Each list dispatches a variant of its own, so the lists make
				// compute pipelines at once.
				culls[index] = inventory.AcquireShader("cull.wgsl", render.ShaderDefine{Name: "LIST", Value: strconv.Itoa(index)})
				objects[index] = inventory.MakeBuffer("objects", render.BufferUsageFlagStorage|render.BufferUsageFlagCopySrc, make([]float32, 64))
				visible[index] = inventory.MakeBuffer("visible", render.BufferUsageFlagStorage, make([]uint32, 4))
			}
		})
		order := nextOrder.Add(1)
		index := order % listCount
		list := frame.BeginCommandList("compute", drawOrder+render.Order(order))
		pass := list.BeginComputePass("cull")
		for dispatch := range 16 {
			pass.SetShader(culls[index], "")
			pass.SetUniform("camera", cameraBlock{Position: [4]float32{float32(order), float32(dispatch)}})
			pass.SetBuffer("objects", render.BufferRange{Buffer: objects[index]})
			pass.SetBuffer("visible", render.BufferRange{Buffer: visible[index]})
			pass.Dispatch(uint32(dispatch+1), 1, 1)
			pass.SetShader(kernels, "count")
			pass.SetBuffer("counts", render.BufferRange{Buffer: visible[index]})
			pass.Dispatch(1, 1, 1)
		}
		pass.End()
		list.CopyBuffer(render.BufferRange{Buffer: objects[index], Size: 16}, render.BufferRange{Buffer: visible[index]})
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	for passIndex, dispatches := range findDispatches(h, "cull") {
		if len(dispatches) != 32 {
			t.Errorf("compute pass %d dispatched %d times, want 32", passIndex, len(dispatches))
		}
	}
	if pipelines := findCreatedComputePipelines(h.Calls()); len(pipelines) != listCount+1 {
		t.Errorf("render made %d compute pipelines, want %d: cull's of each list's variant, and count's, which every list shares", len(pipelines), listCount+1)
	}
	for _, inspection := range h.Frames() {
		copies := 0
		for _, list := range inspection.CommandLists {
			copies += len(list.Copies)
		}
		if copies != listCount {
			t.Errorf("a frame copied %d times, want %d, once for each list", copies, listCount)
		}
	}
}
