package rendertest_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// offsetsLayout is a layout of one vec4<f32> at location 1, and weightsLayout
// one of an f32 at location 2: instanced.wgsl's per-instance inputs.
var (
	offsetsLayout = render.VertexLayout{Stride: 16, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x4, Location: 1}}}
	weightsLayout = render.VertexLayout{Stride: 4, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32, Location: 2}}}
)

// findDrawVertexBuffers answers, for each draw among commands, the vertex
// buffers it binds, set before it at the slots its pipeline, made among
// calls, reads, in slot order.
func findDrawVertexBuffers(calls []recording.Call, commands []recording.Command) [][]recording.SetVertexBufferArguments {
	slotCounts := map[gfx.RenderPipeline]int{}
	for _, index := range findCallIndices(calls, recording.CallKindCreateRenderPipeline) {
		arguments := calls[index].Arguments.(recording.CreateRenderPipelineArguments)
		slotCounts[arguments.Pipeline] = len(arguments.Descriptor.Vertex.Buffers)
	}
	var draws [][]recording.SetVertexBufferArguments
	var bound [8]recording.SetVertexBufferArguments
	slotCount := 0
	for _, command := range commands {
		switch command.Kind {
		case recording.CommandKindSetPipeline:
			slotCount = slotCounts[command.Arguments.(gfx.RenderPipeline)]
		case recording.CommandKindSetVertexBuffer:
			arguments := command.Arguments.(recording.SetVertexBufferArguments)
			bound[arguments.Slot] = arguments
		case recording.CommandKindDraw, recording.CommandKindDrawIndexed:
			draws = append(draws, slices.Clone(bound[:slotCount]))
		}
	}
	return draws
}

// findGfxBuffer answers the gfx buffer behind buffer among calls.
func findGfxBuffer(t *testing.T, calls []recording.Call, buffer render.Buffer) gfx.Buffer {
	t.Helper()
	created, ok := readBufferCalls(calls).findCreated[recording.CreateBufferArguments](buffer)
	if !ok {
		t.Fatalf("gfx made no buffer, or several, behind %v", buffer)
	}
	return created.Buffer
}

func TestInstanceBuffersBindAfterTheMeshsVertexBuffersSteppedPerInstanceInSlotOrderUntilTheMaterialChanges(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	var offsets, weights render.Buffer
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		offsets = inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 16))
		weights = inventory.MakeBuffer("weights", render.BufferUsageFlagVertex, make([]float32, 4))
		instanced := inventory.MakeMaterial(render.MaterialDescriptor{Label: "instanced", Shader: inventory.AcquireShader("instanced.wgsl")})
		plain := inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(instanced)
			pass.SetInstanceBuffer(2, render.InstanceBuffer{Range: render.BufferRange{Buffer: weights}, Layout: weightsLayout})
			pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets, Offset: 16}, Layout: offsetsLayout})
			pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 3})
			pass.Draw(scene.indexedMesh, render.DrawArgs{InstanceCount: 2})
			pass.SetMaterial(plain)
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	calls := h.Calls()
	vertices := findGfxBuffer(t, calls, scene.vertices)
	instanced := []recording.SetVertexBufferArguments{
		{Slot: 0, Buffer: vertices},
		{Slot: 1, Buffer: findGfxBuffer(t, calls, offsets), Offset: 16},
		{Slot: 2, Buffer: findGfxBuffer(t, calls, weights)},
	}
	want := [][]recording.SetVertexBufferArguments{instanced, instanced, {{Slot: 0, Buffer: vertices}}}
	if draws := findDrawVertexBuffers(calls, findPassCommands(h, "scene")); !reflect.DeepEqual(draws, want) {
		t.Errorf("the draws set vertex buffers\n%+v\nwant\n%+v: the mesh's, then the instance buffers in slot order, none once the material changed", draws, want)
	}
	pipelines := findCreatedPipelines(calls)
	if len(pipelines) != 2 {
		t.Fatalf("render made %d pipelines, want two: the instanced material's and the plain one's", len(pipelines))
	}
	var stepModes [][]gfx.VertexStepMode
	for _, pipeline := range pipelines {
		var modes []gfx.VertexStepMode
		for _, layout := range pipeline.Vertex.Buffers {
			modes = append(modes, layout.StepMode)
		}
		stepModes = append(stepModes, modes)
	}
	wantModes := [][]gfx.VertexStepMode{{gfx.VertexStepModeVertex, gfx.VertexStepModeInstance, gfx.VertexStepModeInstance}, {gfx.VertexStepModeVertex}}
	if !reflect.DeepEqual(stepModes, wantModes) {
		t.Errorf("the pipelines stepped their vertex buffers %v, want %v", stepModes, wantModes)
	}
	if layout := pipelines[0].Vertex.Buffers[1]; layout.ArrayStride != 16 || !slices.Equal(layout.Attributes, []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x4, ShaderLocation: 1}}) {
		t.Errorf("the first instance buffer's layout is %+v, want offsetsLayout's", layout)
	}
}

func TestInstanceBuffersLastUntilThePassEndsAndJoinThePipelineKeyByValue(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		offsets := inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 16))
		weights := inventory.MakeBuffer("weights", render.BufferUsageFlagVertex, make([]float32, 4))
		instanced := inventory.MakeMaterial(render.MaterialDescriptor{Label: "instanced", Shader: inventory.AcquireShader("instanced.wgsl")})
		plain := inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		list := frame.BeginCommandList("draws", drawOrder)
		pass := list.BeginPass(scene.colorPass("first"))
		pass.SetMaterial(instanced)
		pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets}, Layout: offsetsLayout})
		pass.SetInstanceBuffer(1, render.InstanceBuffer{Range: render.BufferRange{Buffer: weights}, Layout: weightsLayout})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		// Another range of the same layouts draws with the same pipeline.
		pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets, Offset: 32}, Layout: offsetsLayout})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		// A slot's layout of another stride draws with a pipeline of its own.
		pass.SetInstanceBuffer(1, render.InstanceBuffer{Range: render.BufferRange{Buffer: weights}, Layout: render.VertexLayout{Stride: 8, Attributes: weightsLayout.Attributes}})
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		pass = list.BeginPass(scene.makeKeptPass("second"))
		pass.SetMaterial(plain)
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	if pipelines := findCreatedPipelines(h.Calls()); len(pipelines) != 3 {
		t.Errorf("render made %d pipelines, want three: one for the first two instanced draws, one for the third's stride, one for the plain draw", len(pipelines))
	}
	if draws := findDrawVertexBuffers(h.Calls(), findPassCommands(h, "second")); len(draws) != 1 || len(draws[0]) != 1 {
		t.Errorf("the second pass's draw set vertex buffers %+v, want the mesh's alone", draws)
	}
}

func TestAnInstanceBufferPastTheLastSlotOrMisalignedIsRefusedAndReported(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindInstanceBufferRefused)
	var scene drawScene
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		offsets := inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 16))
		plain := inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(plain)
			pass.SetInstanceBuffer(8, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets}, Layout: offsetsLayout})
			pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets, Offset: 2}, Layout: offsetsLayout})
			pass.SetInstanceBuffer(1, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets}, Layout: render.VertexLayout{Stride: 6, Attributes: offsetsLayout.Attributes}})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	if count := countReports(h, render.ReportKindInstanceBufferRefused); count != 3 {
		t.Errorf("render reported %d refused instance buffers, want three: one past the last slot, one at an odd offset, one of an odd stride", count)
	}
	if draws := findDrawVertexBuffers(h.Calls(), findPassCommands(h, "scene")); len(draws) != 1 || len(draws[0]) != 1 {
		t.Errorf("the draw set vertex buffers %+v, want the mesh's alone", draws)
	}
}

func TestADrawWhoseInstanceBufferWasFreedIsSkippedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	var scene drawScene
	var offsets, weights render.Buffer
	var instanced render.Material
	spawnDrawHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			offsets = inventory.MakeBuffer("offsets", render.BufferUsageFlagVertex, make([]float32, 16))
			weights = inventory.MakeBuffer("weights", render.BufferUsageFlagVertex, make([]float32, 4))
			instanced = inventory.MakeMaterial(render.MaterialDescriptor{Label: "instanced", Shader: inventory.AcquireShader("instanced.wgsl")})
			inventory.ReleaseBuffer(weights)
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(instanced)
			pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: offsets}, Layout: offsetsLayout})
			pass.SetInstanceBuffer(1, render.InstanceBuffer{Range: render.BufferRange{Buffer: weights}, Layout: weightsLayout})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
			pass.Draw(scene.indexedMesh, render.DrawArgs{})
		})
	})

	if count := countReports(h, render.ReportKindDrawSkipped); count != 1 {
		t.Errorf("render reported %d skipped draws over three frames of two draws whose instance buffer was freed, want one", count)
	}
	for frameIndex, draws := range findPassDraws(h, "scene")[1:] {
		if len(draws) != 0 {
			t.Errorf("frame %d drew %+v, want its draws skipped", frameIndex+1, draws)
		}
	}
}
