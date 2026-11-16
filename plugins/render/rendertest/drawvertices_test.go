package rendertest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestDrawVerticesDrawsWithNoVertexBufferUnderAPipelineOfItsTopologyAndItsFirstInstance(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	var shader render.Shader
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		shader = inventory.AcquireShader("fullscreen.wgsl")
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "fullscreen", Shader: shader})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.DrawVertices(0, 3, render.DrawArgs{InstanceCount: 2, FirstInstance: 5})
			pass.DrawVertices(render.PrimitiveTopologyLineList, 4, render.DrawArgs{})
		})
	})

	var kinds []recording.CommandKind
	var draws []gfx.DrawArgs
	for _, command := range findPassCommands(h, "scene") {
		kinds = append(kinds, command.Kind)
		if arguments, ok := command.Arguments.(gfx.DrawArgs); ok {
			draws = append(draws, arguments)
		}
	}
	wantKinds := []recording.CommandKind{recording.CommandKindSetPipeline, recording.CommandKindDraw, recording.CommandKindSetPipeline, recording.CommandKindDraw}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Errorf("the pass recorded %v, want %v: a pipeline for each topology and no vertex buffer", kinds, wantKinds)
	}
	wantDraws := []gfx.DrawArgs{{VertexCount: 3, InstanceCount: 2, FirstInstance: 5}, {VertexCount: 4, InstanceCount: 1}}
	if !reflect.DeepEqual(draws, wantDraws) {
		t.Errorf("the pass drew %+v, want %+v", draws, wantDraws)
	}
	var topologies []gfx.PrimitiveTopology
	for _, pipeline := range findCreatedPipelines(h.Calls()) {
		topologies = append(topologies, pipeline.Primitive.Topology)
		if len(pipeline.Vertex.Buffers) != 0 {
			t.Errorf("a pipeline of DrawVertices has vertex buffers %+v, want none", pipeline.Vertex.Buffers)
		}
	}
	if want := []gfx.PrimitiveTopology{gfx.PrimitiveTopologyTriangleList, gfx.PrimitiveTopologyLineList}; !reflect.DeepEqual(topologies, want) {
		t.Errorf("the pipelines' topologies are %v, want %v: zero a triangle list", topologies, want)
	}
	material := makePlainMaterial(shader)
	want := []render.InspectedDraw{{Material: material, VertexCount: 3, InstanceCount: 2, FirstInstance: 5}, {Material: material, VertexCount: 4, InstanceCount: 1}}
	if inspected := findPassDraws(h, "scene")[0]; !reflect.DeepEqual(inspected, want) {
		t.Errorf("the reader read draws\n%+v\nwant\n%+v", inspected, want)
	}
}

func TestDrawVerticesBindsInstanceBuffersFromTheFirstSlot(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	var rows render.Buffer
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		rows = inventory.MakeBuffer("rows", render.BufferUsageFlagVertex, make([]float32, 16))
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "sprites", Shader: inventory.AcquireShader("sprites.wgsl")})
		layout := render.VertexLayout{Stride: 16, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x4}}}
		// A mesh of the same layout, read per vertex, is drawn first.
		mesh := inventory.MakeMesh(render.MeshDescriptor{VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: rows}, Layout: layout}}})
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.Draw(mesh, render.DrawArgs{})
			pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: render.BufferRange{Buffer: rows}, Layout: layout})
			pass.DrawVertices(render.PrimitiveTopologyTriangleStrip, 4, render.DrawArgs{InstanceCount: 4})
		})
	})

	calls := h.Calls()
	rowsBinding := []recording.SetVertexBufferArguments{{Slot: 0, Buffer: findGfxBuffer(t, calls, rows)}}
	want := [][]recording.SetVertexBufferArguments{rowsBinding, rowsBinding}
	if draws := findDrawVertexBuffers(calls, findPassCommands(h, "scene")); !reflect.DeepEqual(draws, want) {
		t.Errorf("the draws set vertex buffers %+v, want %+v", draws, want)
	}
	var stepModes []gfx.VertexStepMode
	for _, pipeline := range findCreatedPipelines(calls) {
		for _, layout := range pipeline.Vertex.Buffers {
			stepModes = append(stepModes, layout.StepMode)
		}
	}
	if want := []gfx.VertexStepMode{gfx.VertexStepModeVertex, gfx.VertexStepModeInstance}; !reflect.DeepEqual(stepModes, want) {
		t.Errorf("the pipelines stepped their vertex buffers %v, want %v: the mesh's per vertex, then the instance buffer of the same layout per instance", stepModes, want)
	}
}

func TestAShaderReadingAPerVertexInputDrawnWithDrawVerticesIsRefusedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPipelineFailed)
	var scene drawScene
	var material render.Material
	spawnDrawHarness(t, h, 2, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			material = inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		}
		recordPass(frame, scene.colorPass("scene"), func(pass *render.Pass) {
			pass.SetMaterial(material)
			pass.DrawVertices(0, 3, render.DrawArgs{})
			pass.DrawVertices(0, 3, render.DrawArgs{})
		})
	})

	if count := countReports(h, render.ReportKindPipelineFailed); count != 1 {
		t.Errorf("render reported %d failed pipelines over two frames of two draws, want one", count)
	}
	if reports := h.Reports(); len(reports) == 0 || !errors.Is(reports[0], gfx.ErrVertexMismatch) {
		t.Errorf("render reported %v, want gfx's ErrVertexMismatch", reports)
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		if len(draws) != 0 {
			t.Errorf("frame %d drew %+v, want its draws skipped", frameIndex, draws)
		}
	}
}
