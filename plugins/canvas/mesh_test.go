package canvas_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/render"
)

func TestAMeshFilePutIntoAMemoryMountDrawsAtItsFirstDrawAndAForestOfItIsOneDraw(t *testing.T) {
	// A forest of 40 trees, a quad of 4 vertices indexed by 6, recorded in
	// one call across the view, and one more tree 1000 units right, outside
	// it, culled by its bounds; then a rock, a triangle of another file,
	// which is a draw of its own.
	memory := filesystems.NewMemoryFS()
	putMeshFile(t, memory, "meshes/tree.mesh", makeSquareVertices(10), squareIndices, meshfile.TopologyTriangleList)
	putMeshFile(t, memory, "meshes/rock.mesh", triangleVertices, nil, meshfile.TopologyTriangleList)
	tree := canvas.MeshFileFromFile("meshes/tree.mesh")
	rock := canvas.MeshFileFromFile("meshes/rock.mesh")
	var forest []canvas.Mesh
	for index := range 40 {
		forest = append(forest, canvas.Mesh{File: tree, Transform: m.Translation2x3(float32(-150+7*index), 0)})
	}
	forest = append(forest, canvas.Mesh{File: tree, Transform: m.Translation2x3(1000, 0)})
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: memory})
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(func(list canvas.CommandList) {
		list.DrawMeshes(forest)
		list.DrawMesh(canvas.Mesh{File: rock, Transform: m.Translation2x3(0, 50)})
	})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 2 || len(batches[0].Entries) != 40 || batches[0].InstanceCount != 40 || len(batches[1].Entries) != 1 || batches[1].InstanceCount != 1 {
		t.Fatalf("the frame drew batches %+v, want the forest's 40 trees in view as one batch, then the rock's", batches)
	}
	for index, entry := range batches[0].Entries {
		if entry.Kind != canvas.EntryKindMesh || entry.Mesh.File != tree {
			t.Errorf("the forest's entry %d is %+v, want a mesh of the tree", index, entry)
		}
	}
	if entry := batches[1].Entries[0]; entry.Kind != canvas.EntryKindMesh || entry.Mesh.File != rock {
		t.Errorf("the last batch holds %+v, want the rock", entry)
	}
	lists := findCameraLists(frame.Render)
	if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 2 {
		t.Fatalf("canvas's lists are %+v, want one pass of two draws", lists)
	}
	draws := lists[0].Passes[0].Draws
	for index, draw := range draws {
		if draw.Material.VertexEntryPoint != "canvas_mesh_vertex" {
			t.Errorf("draw %d drew through %q, want canvas_mesh_vertex", index, draw.Material.VertexEntryPoint)
		}
	}
	if draws[0].InstanceCount != 40 || draws[0].IndexCount != 6 {
		t.Errorf("the forest drew %d instances of %d indices, want 40 of 6", draws[0].InstanceCount, draws[0].IndexCount)
	}
	if draws[1].InstanceCount != 1 || draws[1].VertexCount != 3 || draws[1].IndexCount != 0 {
		t.Errorf("the rock drew %d instances of %d vertices and %d indices, want one of 3 and none", draws[1].InstanceCount, draws[1].VertexCount, draws[1].IndexCount)
	}
}

func TestAMissingOrMalformedMeshFileDrawsNothingReportedOnceUntilPutAgainAndUnloaded(t *testing.T) {
	// A file of bytes that are no mesh file, and a path with no file, each
	// drawn every frame beside a fill and a mesh of the zero MeshFile:
	// neither draws, each reported once, and the zero MeshFile draws
	// nothing, reporting nothing. At the third frame both are put as a
	// triangle, and the missing one's path is unloaded: it draws, and the
	// malformed one, not unloaded, still does not. At the fourth, the
	// malformed bytes are put back and its path unloaded: it fails again,
	// reported again.
	memory := filesystems.NewMemoryFS()
	putFile(t, memory, "meshes/broken.mesh", []byte("no mesh file"))
	broken := canvas.MeshFileFromFile("meshes/broken.mesh")
	later := canvas.MeshFileFromFile("meshes/later.mesh")
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		switch frameIndex {
		case 2:
			putMeshFile(t, memory, "meshes/broken.mesh", triangleVertices, nil, meshfile.TopologyTriangleList)
			putMeshFile(t, memory, "meshes/later.mesh", triangleVertices, nil, meshfile.TopologyTriangleList)
			assets.Unload("meshes/later.mesh")
		case 3:
			putFile(t, memory, "meshes/broken.mesh", []byte("no mesh file"))
			assets.Unload("meshes/broken.mesh")
		}
	}}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMeshFileNotLoaded)
	h.Spawn(nil, nil, &fileMounter{files: memory}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 4 {
		h.Record(func(list canvas.CommandList) {
			list.DrawMesh(canvas.Mesh{File: broken})
			list.DrawMesh(canvas.Mesh{File: later})
			list.DrawMesh(canvas.Mesh{})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Layer: 1})
		})
		h.Step()
	}

	frames := h.Frames()
	for index, frame := range frames[:2] {
		batches := frame.Cameras[0].Batches
		if len(batches) != 1 || batches[0].Entries[0].Kind != canvas.EntryKindShape {
			t.Errorf("frame %d drew batches %+v, want the fill's alone", index, batches)
		}
	}
	for index, frame := range frames[2:] {
		batches := frame.Cameras[0].Batches
		if len(batches) != 2 || batches[0].Entries[0].Mesh.File != later || batches[1].Entries[0].Kind != canvas.EntryKindShape {
			t.Errorf("frame %d, after the missing file's path was unloaded, drew batches %+v, want its mesh, then the fill", index+2, batches)
		}
	}
	var reported []string
	for _, report := range h.Reports() {
		if report.Kind == canvas.ReportKindMeshFileNotLoaded {
			reported = append(reported, report.Err.Error())
		}
	}
	if len(reported) != 3 || !strings.Contains(reported[0], "broken.mesh") || !strings.Contains(reported[1], "later.mesh") || !strings.Contains(reported[2], "broken.mesh") {
		t.Errorf("the files reported %q, want one report of each path, then the malformed one's again once unloaded", reported)
	}
}

func TestAMeshFileOfZeroBoundsDrawsNothingReportedOnce(t *testing.T) {
	// A triangle's file whose bounds, at bytes 32 to 56 of its header, a
	// tool wrote as zero, which meshfile.Encode refuses to.
	data, err := meshfile.Encode(triangleVertices, nil, meshfile.TopologyTriangleList, meshfile.CalculateBounds(triangleVertices, canvas.Vertex.Position3))
	if err != nil {
		t.Fatalf("encoding the triangle: %v", err)
	}
	clear(data[32:56])
	memory := filesystems.NewMemoryFS()
	putFile(t, memory, "meshes/unbounded.mesh", data)
	unbounded := canvas.MeshFileFromFile("meshes/unbounded.mesh")
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMeshFileNotLoaded)
	h.Spawn(nil, nil, &fileMounter{files: memory})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 2 {
		h.Record(func(list canvas.CommandList) {
			list.DrawMesh(canvas.Mesh{File: unbounded})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
		})
		h.Step()
	}

	for index, frame := range h.Frames() {
		if batches := frame.Cameras[0].Batches; len(batches) != 1 || batches[0].Entries[0].Kind != canvas.EntryKindShape {
			t.Errorf("frame %d drew batches %+v, want the fill's alone", index, batches)
		}
	}
	if count := countReports(h, canvas.ReportKindMeshFileNotLoaded); count != 1 {
		t.Errorf("two frames of the file of zero bounds reported %d times, want once", count)
	}
}

func TestPuttingAMeshFileAgainAndUnloadingItsPathDrawsTheNewOne(t *testing.T) {
	// A triangle drawn, then put again as a quad indexed by 6, a terrain
	// chunk deformed, and its path unloaded at the second frame, which
	// draws the quad.
	memory := filesystems.NewMemoryFS()
	putMeshFile(t, memory, "meshes/chunk.mesh", triangleVertices, nil, meshfile.TopologyTriangleList)
	chunk := canvas.MeshFileFromFile("meshes/chunk.mesh")
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 1 {
			putMeshFile(t, memory, "meshes/chunk.mesh", makeSquareVertices(10), squareIndices, meshfile.TopologyTriangleList)
			assets.Unload("meshes/")
		}
	}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: memory}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 2 {
		h.Record(func(list canvas.CommandList) {
			list.DrawMesh(canvas.Mesh{File: chunk})
		})
		h.Step()
	}

	for index, want := range []struct{ vertexCount, indexCount uint32 }{{3, 0}, {0, 6}} {
		lists := findCameraLists(h.Frames()[index].Render)
		if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 1 {
			t.Fatalf("frame %d's canvas lists are %+v, want one pass of one draw", index, lists)
		}
		draw := lists[0].Passes[0].Draws[0]
		if draw.IndexCount != want.indexCount || want.indexCount == 0 && draw.VertexCount != want.vertexCount {
			t.Errorf("frame %d drew %d vertices and %d indices, want %d vertices and %d indices", index, draw.VertexCount, draw.IndexCount, want.vertexCount, want.indexCount)
		}
	}
}

func TestAMeshFileSamplesItsImageAndItsTranslationSnapsUnderAPixelPerfectCamera(t *testing.T) {
	// 80×60 art pixels expanded into the window of 320×240 points, four
	// points a unit, cleared to black: a white square of 2.5 units at
	// (10.3, -5.6) lands at (10, -6), its translation rounded, and spans
	// 2.5 units from there, its vertices unsnapped, to (12.5, -3.5). A
	// square of 16 units at (-36, -10), of the quadrants' image read
	// nearest, shows its red, blue and white quadrants 8 units each.
	memory := filesystems.NewMemoryFS()
	putMeshFile(t, memory, "meshes/square.mesh", makeSquareVertices(2.5), nil, meshfile.TopologyTriangleStrip)
	putMeshFile(t, memory, "meshes/picture.mesh", makeSquareVertices(16), squareIndices, meshfile.TopologyTriangleList)
	putFile(t, memory, quadrantsPath, makeQuadrantsFiles(t)[quadrantsPath].Data)
	square := canvas.MeshFileFromFile("meshes/square.mesh")
	framed := canvas.MeshFileFromFile("meshes/picture.mesh")
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.CommandList) {
		list.SetCamera(canvas.Camera{Size: m.Vec2{X: 80, Y: 60}, PixelPerfect: true, Clear: true, ClearColor: m.Color{A: 1}})
		list.DrawMesh(canvas.Mesh{File: square, Transform: m.Translation2x3(10.3, -5.6)})
		list.DrawMesh(canvas.Mesh{File: framed, Image: canvas.ImageFromFile(quadrantsPath), Filter: canvas.FilterNearest, Transform: m.Translation2x3(-36, -10)})
	}, &fileMounter{files: memory})

	// Each edge is probed half a point either side, in the window's points
	// from its centre.
	picture.expect(t, []probe{
		{"inside its left edge", m.Vec2{X: 40.5, Y: -19}, opaqueWhite},
		{"outside its left edge", m.Vec2{X: 39.5, Y: -19}, opaqueBlack},
		{"inside its right edge", m.Vec2{X: 49.5, Y: -19}, opaqueWhite},
		{"outside its right edge", m.Vec2{X: 50.5, Y: -19}, opaqueBlack},
		{"inside its top edge", m.Vec2{X: 45, Y: -23.5}, opaqueWhite},
		{"outside its top edge", m.Vec2{X: 45, Y: -24.5}, opaqueBlack},
		{"inside its bottom edge", m.Vec2{X: 45, Y: -14.5}, opaqueWhite},
		{"outside its bottom edge", m.Vec2{X: 45, Y: -13.5}, opaqueBlack},
		{"the image's red quadrant", m.Vec2{X: -128, Y: -24}, opaqueRed},
		{"the image's blue quadrant", m.Vec2{X: -128, Y: 8}, opaqueBlue},
		{"the image's white quadrant", m.Vec2{X: -96, Y: 8}, opaqueWhite},
	})
}

func TestASwayingGrassMeshInItsOwnLayoutDrawsThroughItsMaterialsVertexStage(t *testing.T) {
	// A blade 10 units wide and 40 tall about the origin, its root at the
	// bottom swaying 0 and its tip at the top 1, drawn green by the grass
	// material with a wind of 30: its tip leans 30 units right, so near
	// the tip it covers x from 23.5 to 33.5 and leaves where the upright
	// blade stood, and near the root it has barely moved.
	mounter := newMaterialMounter(t)
	mounter.files.(fstest.MapFS)["meshes/grass.mesh"] = &fstest.MapFile{Data: encodeMeshFile(t, "meshes/grass.mesh", grassBlade)}
	grass := canvas.MeshFileFromFile("meshes/grass.mesh")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{grassMaterialDescriptor},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			if frame == 0 {
				assets.SetMaterialUniform(materials[0], "grass_wind", [4]float32{30, 0, 0, 0})
			}
			list.DrawMesh(canvas.Mesh{File: grass, Material: materials[0], Tint: m.Color{G: 1, A: 1}})
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.CommandList) {}, recorder, mounter)

	picture.expect(t, []probe{
		{"near the swayed tip", m.Vec2{X: 28.5, Y: -18}, opaqueGreen},
		{"where the upright tip stood", m.Vec2{X: 0, Y: -18}, opaqueBlack},
		{"near the root", m.Vec2{X: 1.5, Y: 18}, opaqueGreen},
		{"right of the root", m.Vec2{X: 10, Y: 18}, opaqueBlack},
	})
}

func TestAMeshOfItsOwnLayoutDrawnWithAFragmentOnlyMaterialIsRefusedReportedOnce(t *testing.T) {
	// The grass blade's file and per-frame triangles of its vertices, each
	// drawn over two frames with the default material, which has no vertex
	// stage to read them, then with the grass material, which has: the
	// first two are skipped and reported once for the material, and the
	// grass material's draw through its own vertex stage.
	mounter := newMaterialMounter(t)
	mounter.files.(fstest.MapFS)["meshes/grass.mesh"] = &fstest.MapFile{Data: encodeMeshFile(t, "meshes/grass.mesh", grassBlade)}
	grass := canvas.MeshFileFromFile("meshes/grass.mesh")
	recorder := &materialRecorder{
		descriptors: []canvas.MaterialDescriptor{grassMaterialDescriptor},
		record: func(list canvas.CommandList, assets canvas.Assets, materials []canvas.Material, frame int) {
			list.DrawMesh(canvas.Mesh{File: grass})
			list.DrawTriangles(canvas.DrawTrianglesOptions{}, grassBlade)
			list.DrawMesh(canvas.Mesh{File: grass, Material: materials[0]})
			list.DrawTriangles(canvas.DrawTrianglesOptions{Material: materials[0]}, grassBlade)
		},
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindVertexStageMissing)
	h.Spawn(nil, nil, recorder, mounter)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 2 {
		h.Step()
	}

	for index, frame := range h.Frames() {
		batches := frame.Cameras[0].Batches
		if len(batches) != 2 || batches[0].Material != recorder.madeMaterials()[0] || batches[0].Entries[0].Kind != canvas.EntryKindMesh || batches[1].Entries[0].Kind != canvas.EntryKindTriangles {
			t.Errorf("frame %d drew batches %+v, want the grass material's mesh and triangles alone", index, batches)
			continue
		}
		lists := findCameraLists(frame.Render)
		if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 2 {
			t.Errorf("frame %d's canvas lists are %+v, want one pass of two draws", index, lists)
			continue
		}
		for drawIndex, draw := range lists[0].Passes[0].Draws {
			if draw.Material.VertexEntryPoint != "grass_vertex" {
				t.Errorf("frame %d's draw %d drew through %q, want grass_vertex", index, drawIndex, draw.Material.VertexEntryPoint)
			}
		}
	}
	if count := countReports(h, canvas.ReportKindVertexStageMissing); count != 1 {
		t.Errorf("two frames of the default material's mesh and triangles of the grass's layout reported %d times, want once", count)
	}
}

// grassMaterialDescriptor is the grass material's, drawing through its
// shader's own vertex stage, grass_vertex.
var grassMaterialDescriptor = canvas.MaterialDescriptor{Label: "grass", Shader: grassShaderPath, VertexEntryPoint: "grass_vertex"}

// grassVertex is the vertex of the grass test's own layout: its position,
// then how far it sways, 0 at the root and 1 at the tip.
type grassVertex struct {
	Position m.Vec2
	Sway     float32
}

// VertexLayout answers grassVertex's layout: its position at location 0
// and its sway at 1, as the grass shader's GrassVertex reads them.
func (grassVertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 12, Attributes: []meshfile.Attribute{
		{Format: meshfile.FormatFloat32x2, Offset: 0, Location: 0},
		{Format: meshfile.FormatFloat32, Offset: 8, Location: 1},
	}}
}

// position3 answers the vertex's position with a zero z.
func (v grassVertex) position3() m.Vec3 {
	return m.Vec3{X: v.Position.X, Y: v.Position.Y}
}

// grassBladeVertices are a blade 10 units wide and 40 tall about the
// origin, its tip's corners, at the top, swaying 1 and its root's 0, in
// makeSquareVertices's order, and grassBlade their triangles, drawn by
// squareIndices within their bounds.
var (
	grassBladeVertices = []grassVertex{
		{Position: m.Vec2{X: -5, Y: -20}, Sway: 1},
		{Position: m.Vec2{X: 5, Y: -20}, Sway: 1},
		{Position: m.Vec2{X: -5, Y: 20}},
		{Position: m.Vec2{X: 5, Y: 20}},
	}
	grassBlade = canvas.Triangles[grassVertex]{Vertices: grassBladeVertices, Indices: squareIndices, Topology: meshfile.TopologyTriangleList, Bounds: meshfile.CalculateBounds(grassBladeVertices, grassVertex.position3)}
)

// triangleVertices are a triangle's, of legs 10 units along x and y from
// the origin.
var triangleVertices = []canvas.Vertex{{}, {Position: m.Vec2{X: 10}}, {Position: m.Vec2{Y: 10}}}

// squareIndices draw makeSquareVertices's corners as a triangle list.
var squareIndices = []uint32{0, 1, 2, 2, 1, 3}

// makeSquareVertices answers the corners of a square of side units from the
// origin, top left, top right, bottom left and bottom right, its UVs over
// the whole image: a strip, or a list of indices 0, 1, 2, 2, 1, 3.
func makeSquareVertices(side float32) []canvas.Vertex {
	var vertices []canvas.Vertex
	for _, corner := range []m.Vec2{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1}} {
		vertices = append(vertices, canvas.Vertex{Position: corner.MulS(side), UV: corner})
	}
	return vertices
}

// makeSquareTriangles answers a list of the triangles of
// makeSquareVertices' square of side units, indexed by squareIndices,
// within their bounds.
func makeSquareTriangles(side float32) canvas.Triangles[canvas.Vertex] {
	vertices := makeSquareVertices(side)
	return canvas.Triangles[canvas.Vertex]{Vertices: vertices, Indices: squareIndices, Topology: meshfile.TopologyTriangleList, Bounds: meshfile.CalculateBounds(vertices, canvas.Vertex.Position3)}
}

// putMeshFile puts into memory, at path, the mesh file of vertices, indexed
// by indices, nil for none, within their bounds.
func putMeshFile(t testing.TB, memory *filesystems.MemoryFS, path string, vertices []canvas.Vertex, indices []uint32, topology meshfile.Topology) {
	t.Helper()
	triangles := canvas.Triangles[canvas.Vertex]{Vertices: vertices, Indices: indices, Topology: topology, Bounds: meshfile.CalculateBounds(vertices, canvas.Vertex.Position3)}
	putFile(t, memory, path, encodeMeshFile(t, path, triangles))
}

// encodeMeshFile answers the mesh file of triangles, for path.
func encodeMeshFile[V meshfile.VertexType](t testing.TB, path string, triangles canvas.Triangles[V]) []byte {
	t.Helper()
	data, err := meshfile.Encode(triangles.Vertices, triangles.Indices, triangles.Topology, triangles.Bounds)
	if err != nil {
		t.Fatalf("encoding %s: %v", path, err)
	}
	return data
}

// putFile puts data into memory at path.
func putFile(t testing.TB, memory *filesystems.MemoryFS, path string, data []byte) {
	t.Helper()
	if err := memory.Put(path, data); err != nil {
		t.Fatalf("putting %s: %v", path, err)
	}
}
