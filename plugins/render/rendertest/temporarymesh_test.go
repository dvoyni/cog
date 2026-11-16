package rendertest_test

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestListsAcquiringTemporaryMeshesAndSettingInstanceBuffersAtOnceDrawWithTheirOwnLayouts(t *testing.T) {
	const listCount = 4
	h := rendertest.NewHarness(t)
	var made sync.Once
	var scene drawScene
	var plain, sprites render.Material
	// nextOrder hands each recording task an order of its own.
	var nextOrder atomic.Int32
	h.Spawn(nil, &loaderPlugin{files: makeDrawFiles(t), recordingTaskCount: listCount, duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		made.Do(func() {
			scene = makeDrawScene(inventory)
			plain = inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
			sprites = inventory.MakeMaterial(render.MaterialDescriptor{Label: "sprites", Shader: inventory.AcquireShader("sprites.wgsl")})
		})
		order := nextOrder.Add(1)
		// Each list's vertices and rows are of strides of its own, so the
		// lists intern layouts at once.
		padding := 4 * uint64(order%listCount)
		list := frame.BeginCommandList("draws", drawOrder+render.Order(order))
		vertices := list.AcquireTemporaryBuffer(make([]byte, 4*(12+padding)))
		rows := list.AcquireTemporaryBuffer(make([]byte, 4*(16+padding)))
		pass := list.BeginPass(render.PassDescriptor{Label: "scene", ColorTargets: []render.ColorTarget{{Texture: scene.color}}})
		for range 16 {
			mesh := list.AcquireTemporaryMesh(render.MeshDescriptor{VertexBuffers: []render.VertexBuffer{{Range: vertices, Layout: render.VertexLayout{Stride: 12 + padding, Attributes: positionLayout.Attributes}}}})
			pass.SetMaterial(plain)
			pass.Draw(mesh, render.DrawArgs{})
			pass.SetMaterial(sprites)
			pass.SetInstanceBuffer(0, render.InstanceBuffer{Range: rows, Layout: render.VertexLayout{Stride: 16 + padding, Attributes: []render.VertexAttribute{{Format: render.VertexFormatFloat32x4}}}})
			pass.DrawVertices(0, 3, render.DrawArgs{InstanceCount: 4})
		}
		pass.End()
		list.End()
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	for passIndex, passDraws := range findPassDraws(h, "scene") {
		if len(passDraws) != 32 {
			t.Errorf("pass %d drew %d times, want 32", passIndex, len(passDraws))
		}
	}
	var strides []uint64
	for _, pipeline := range findCreatedPipelines(h.Calls()) {
		for _, layout := range pipeline.Vertex.Buffers {
			strides = append(strides, layout.ArrayStride)
		}
	}
	slices.Sort(strides)
	if want := []uint64{12, 16, 16, 20, 20, 24, 24, 28}; !slices.Equal(strides, want) {
		t.Errorf("the pipelines read strides %v, want %v: a mesh pipeline and an instanced one for each list's layouts, over both frames", strides, want)
	}
}

func TestTemporaryMeshesOverSubRangesDrawEachCommandsIndicesAndVerticesAndShareAMadeMeshsPipeline(t *testing.T) {
	h := rendertest.NewHarness(t)
	var scene drawScene
	var vertexRange, indexRange render.BufferRange
	spawnDrawHarness(t, h, 1, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		scene = makeDrawScene(inventory)
		material := inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		list := frame.BeginCommandList("draws", drawOrder)
		// The frame's vertices and indices, as a Dear ImGui-style UI writes
		// them: two commands of four vertices and six indices each.
		vertexRange = list.AcquireTemporaryBuffer(make([]float32, 24))
		indexRange = list.AcquireTemporaryBuffer([]uint16{0, 1, 2, 2, 1, 3, 0, 1, 2, 2, 1, 3})
		var meshes []render.Mesh
		for command := range uint64(2) {
			meshes = append(meshes, list.AcquireTemporaryMesh(render.MeshDescriptor{
				Label:         "command",
				VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: vertexRange.Buffer, Offset: vertexRange.Offset + command*48, Size: 48}, Layout: positionLayout}},
				Indices:       render.IndexBuffer{Range: render.BufferRange{Buffer: indexRange.Buffer, Offset: indexRange.Offset + command*12, Size: 12}, Format: render.IndexFormatUint16},
			}))
		}
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(material)
		for _, mesh := range meshes {
			pass.Draw(mesh, render.DrawArgs{})
		}
		pass.Draw(scene.indexedMesh, render.DrawArgs{})
		pass.End()
		list.End()
	})

	calls := h.Calls()
	if pipelines := findCreatedPipelines(calls); len(pipelines) != 1 {
		t.Errorf("render made %d pipelines for two temporary meshes and a made one of one vertex layout, want one", len(pipelines))
	}
	chunk := findGfxBuffer(t, calls, vertexRange.Buffer)
	draw := recording.Command{Kind: recording.CommandKindDrawIndexed, Arguments: gfx.DrawIndexedArgs{IndexCount: 6, InstanceCount: 1}}
	var want []recording.Command
	for command := range uint64(2) {
		want = append(want,
			recording.Command{Kind: recording.CommandKindSetVertexBuffer, Arguments: recording.SetVertexBufferArguments{Buffer: chunk, Offset: vertexRange.Offset + command*48}},
			recording.Command{Kind: recording.CommandKindSetIndexBuffer, Arguments: recording.SetIndexBufferArguments{Buffer: chunk, Format: gfx.IndexFormatUint16, Offset: indexRange.Offset + command*12}},
			draw)
	}
	commands := findPassCommands(h, "scene")
	var drawn []recording.Command
	for _, command := range commands {
		if command.Kind != recording.CommandKindSetPipeline && command.Kind != recording.CommandKindSetBindGroup {
			drawn = append(drawn, command)
		}
	}
	if len(drawn) < len(want) || !reflect.DeepEqual(drawn[:len(want)], want) {
		t.Errorf("the temporary meshes' draws recorded\n%+v\nwant\n%+v", drawn, want)
	}
}

func TestATemporaryMeshReturnsToThePoolWhenTheFrameEndsAndADrawOfItAfterIsSkippedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindDrawSkipped)
	var scene drawScene
	var material render.Material
	var kept, fresh render.Mesh
	spawnDrawHarness(t, h, 3, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 0 {
			scene = makeDrawScene(inventory)
			material = inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})
		}
		list := frame.BeginCommandList("draws", drawOrder)
		descriptor := render.MeshDescriptor{Label: "temporary", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: scene.vertices}, Layout: positionLayout}}}
		mesh := list.AcquireTemporaryMesh(descriptor)
		switch frameIndex {
		case 0:
			kept = mesh
		case 1:
			fresh = mesh
		}
		pass := list.BeginPass(scene.colorPass("scene"))
		pass.SetMaterial(material)
		pass.Draw(kept, render.DrawArgs{})
		pass.End()
		list.End()
	})

	if fresh == kept || fresh == (render.Mesh{}) {
		t.Errorf("the second frame's temporary mesh is %v, the first's %v, want a handle of its own", fresh, kept)
	}
	if count := countReports(h, render.ReportKindDrawSkipped); count != 1 {
		t.Errorf("render reported %d skipped draws over two frames drawing a temporary mesh of the frame before, want one", count)
	}
	for frameIndex, draws := range findPassDraws(h, "scene") {
		want := 0
		if frameIndex == 0 {
			want = 1
		}
		if len(draws) != want {
			t.Errorf("frame %d drew %d draws, want %d: the temporary mesh in its own frame alone", frameIndex, len(draws), want)
		}
	}
}

func TestAcquiringATemporaryMeshAllocatesNothingOnceWarm(t *testing.T) {
	h := rendertest.NewHarness(t)
	// Each warm frame acquires meshCount+1 meshes, AllocsPerRun's warm-up
	// and its meshCount runs, which the first frame acquires at once. A
	// count per acquire, rounded down, leaves out what the kernel's other
	// jobs or the runtime allocate meanwhile.
	const meshCount = 100
	const frameCount = 4
	var allocations [frameCount]float64
	spawnDrawHarness(t, h, frameCount, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		vertices := inventory.MakeBuffer("vertices", render.BufferUsageFlagVertex, make([]float32, 12))
		descriptor := render.MeshDescriptor{Label: "temporary", VertexBuffers: []render.VertexBuffer{{Range: render.BufferRange{Buffer: vertices}, Layout: positionLayout}}}
		list := frame.BeginCommandList("draws", drawOrder)
		acquire := func() { list.AcquireTemporaryMesh(descriptor) }
		if frameIndex == 0 {
			for range meshCount + 1 {
				acquire()
			}
		} else {
			allocations[frameIndex] = testing.AllocsPerRun(meshCount, acquire)
		}
		list.End()
		inventory.ReleaseBuffer(vertices)
	})

	for frameIndex, count := range allocations[1:] {
		if count != 0 {
			t.Errorf("frame %d: acquiring a temporary mesh allocated %v times once warm, want none", frameIndex+1, count)
		}
	}
}
