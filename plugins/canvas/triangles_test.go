package canvas_test

import (
	"image/color"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

func TestEachPerFrameMeshIsADrawOfItsOwnThroughTheMeshVertexStageCulledByItsBounds(t *testing.T) {
	// A fill, then a triangle and a quad of 4 vertices indexed by 6, which
	// batch neither with the fill nor with each other, then the triangle
	// moved 500 units left of the view, its own vertices 500 units right of
	// its origin, so its bounds reach into the view, drawn, and the
	// triangle moved 1000 units right, its bounds outside the view, culled.
	triangle := canvas.Triangles[canvas.Vertex]{
		Vertices: []canvas.Vertex{{Position: m.Vec2{}}, {Position: m.Vec2{X: 10}}, {Position: m.Vec2{Y: 10}}},
		Bounds:   m.Box3{Max: m.Vec3{X: 10, Y: 10}},
	}
	quad := canvas.Triangles[canvas.Vertex]{
		Vertices: []canvas.Vertex{{Position: m.Vec2{}}, {Position: m.Vec2{X: 10}}, {Position: m.Vec2{Y: 10}}, {Position: m.Vec2{X: 10, Y: 10}}},
		Indices:  []uint32{0, 1, 2, 2, 1, 3},
		Bounds:   m.Box3{Max: m.Vec3{X: 10, Y: 10}},
	}
	offOrigin := canvas.Triangles[canvas.Vertex]{
		Vertices: []canvas.Vertex{{Position: m.Vec2{X: 500}}, {Position: m.Vec2{X: 510}}, {Position: m.Vec2{X: 500, Y: 10}}},
		Bounds:   m.Box3{Min: m.Vec3{X: 500}, Max: m.Vec3{X: 510, Y: 10}},
	}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
		list.DrawTriangles(canvas.DrawTrianglesOptions{}, triangle)
		list.DrawTriangles(canvas.DrawTrianglesOptions{}, quad)
		list.DrawTriangles(canvas.DrawTrianglesOptions{Transform: m.Translation2x3(-500, 0)}, offOrigin)
		list.DrawTriangles(canvas.DrawTrianglesOptions{Transform: m.Translation2x3(1000, 0)}, triangle)
	})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	wantKinds := []canvas.EntryKind{canvas.EntryKindShape, canvas.EntryKindTriangles, canvas.EntryKindTriangles, canvas.EntryKindTriangles}
	if len(batches) != len(wantKinds) {
		t.Fatalf("the frame drew %d batches, %+v, want %d: the fill's, and each mesh's in view alone", len(batches), batches, len(wantKinds))
	}
	for index, drawnBatch := range batches {
		if len(drawnBatch.Entries) != 1 || drawnBatch.Entries[0].Kind != wantKinds[index] || drawnBatch.InstanceCount != 1 {
			t.Errorf("batch %d holds %+v, %d instances, want one entry of kind %d, one instance", index, drawnBatch.Entries, drawnBatch.InstanceCount, wantKinds[index])
		}
	}
	if origin := batches[3].Entries[0].TrianglesOptions.Transform; origin != m.Translation2x3(-500, 0) {
		t.Errorf("the last batch drew the triangles moved by %v, want those whose bounds reach into the view from 500 units left, the far ones culled", origin)
	}
	lists := findCanvasLists(frame.Render)
	if len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != len(wantKinds) {
		t.Fatalf("canvas's lists are %+v, want one pass of %d draws", lists, len(wantKinds))
	}
	draws := lists[0].Passes[0].Draws
	if entryPoint := draws[0].Material.VertexEntryPoint; entryPoint != "canvas_sprite_vertex" {
		t.Errorf("the fill drew through %q, want canvas_sprite_vertex", entryPoint)
	}
	for index, draw := range draws[1:] {
		if draw.Material.VertexEntryPoint != "canvas_mesh_vertex" || draw.InstanceCount != 1 {
			t.Errorf("mesh %d drew %d instances through %q, want one through canvas_mesh_vertex", index, draw.InstanceCount, draw.Material.VertexEntryPoint)
		}
	}
	if draws[1].VertexCount != 3 || draws[1].IndexCount != 0 || draws[2].IndexCount != 6 {
		t.Errorf("the triangle drew %d vertices and %d indices, and the quad %d indices, want 3 and none, and 6", draws[1].VertexCount, draws[1].IndexCount, draws[2].IndexCount)
	}
}

func TestTrianglesOfZeroBoundsAreRefusedAndReportedOnce(t *testing.T) {
	unbounded := canvas.Triangles[canvas.Vertex]{Vertices: []canvas.Vertex{{}, {Position: m.Vec2{X: 10}}, {Position: m.Vec2{Y: 10}}}}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindBoundsMissing)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	record := func(list canvas.DrawList) {
		list.DrawTriangles(canvas.DrawTrianglesOptions{}, unbounded)
		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
	}

	h.Record(record)
	h.Step()
	h.Record(record)
	h.Step()

	for index, frame := range h.Frames() {
		batches := frame.Cameras[0].Batches
		if len(batches) != 1 || len(batches[0].Entries) != 1 || batches[0].Entries[0].Kind != canvas.EntryKindShape {
			t.Errorf("frame %d drew batches %+v, want one, of the fill alone", index, batches)
		}
	}
	if count := countReports(h, canvas.ReportKindBoundsMissing); count != 1 {
		t.Errorf("two frames of triangles of zero bounds reported %d refusals, want one, for the draw list", count)
	}
}

func TestTrianglesOfNoVerticesRecordNothingAndReportNothingWhateverTheirBounds(t *testing.T) {
	// An empty trail's bounds, meshfile.CalculateBounds's of no vertices,
	// are zero, which is no mistake; one given bounds draws nothing either.
	// The harness fails on any report.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawTriangles(canvas.DrawTrianglesOptions{}, canvas.Triangles[canvas.Vertex]{})
		list.DrawTriangles(canvas.DrawTrianglesOptions{}, canvas.Triangles[canvas.Vertex]{Bounds: m.Box3{Max: m.Vec3{X: 10, Y: 10}}})
		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
	})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 1 || len(batches[0].Entries) != 1 || batches[0].Entries[0].Kind != canvas.EntryKindShape {
		t.Errorf("the frame drew batches %+v, want one, of the fill alone", batches)
	}
}

func TestAPerFrameTrailFadesToTransparentAlongItsVerticesColours(t *testing.T) {
	// A trail of three pairs of vertices, a strip from x -100 to 60 and y
	// -10 to 10, its first two pairs of no colour, white, its last faded,
	// {1, 1, 1, 0}, tinted orange over black: solid to x -20, half faded at
	// 20, and nearly gone at 60. Its vertices, reused once it is recorded,
	// are moved 40 units down and drawn again as a triangle list of 12
	// indices, so both trails show only if each was copied as recorded.
	faded := m.Color{R: 1, G: 1, B: 1, A: 0}
	vertices := []canvas.Vertex{
		{Position: m.Vec2{X: -100, Y: -10}}, {Position: m.Vec2{X: -100, Y: 10}},
		{Position: m.Vec2{X: -20, Y: -10}}, {Position: m.Vec2{X: -20, Y: 10}},
		{Position: m.Vec2{X: 60, Y: -10}, Color: faded}, {Position: m.Vec2{X: 60, Y: 10}, Color: faded},
	}
	options := canvas.DrawTrianglesOptions{Tint: orange}
	picture := captureDrawing(t, func(list canvas.DrawList) {
		trail := append([]canvas.Vertex(nil), vertices...)
		list.DrawTriangles(options, canvas.Triangles[canvas.Vertex]{Vertices: trail, Topology: meshfile.TopologyTriangleStrip, Bounds: meshfile.CalculateBounds(trail, canvas.Vertex.Position3)})
		for index := range trail {
			trail[index].Position.Y += 40
		}
		list.DrawTriangles(options, canvas.Triangles[canvas.Vertex]{Vertices: trail, Indices: []uint32{0, 1, 2, 2, 1, 3, 2, 3, 4, 4, 3, 5}, Bounds: meshfile.CalculateBounds(trail, canvas.Vertex.Position3)})
	})

	for _, y := range []float32{0, 40} {
		picture.expect(t, []probe{
			{"the trail's solid part", m.Vec2{X: -60, Y: y}, orangeShown},
			{"left of the trail", m.Vec2{X: -110, Y: y}, opaqueBlack},
			{"right of the trail", m.Vec2{X: 70, Y: y}, opaqueBlack},
			{"beside the trail", m.Vec2{X: -60, Y: y + 20}, opaqueBlack},
		})
		// Half of orange, (255, 85, 0), over black.
		if half := picture.at(m.Vec2{X: 20, Y: y}); !isNearColour(half, color.NRGBA{R: 128, G: 43, A: 255}, 3) {
			t.Errorf("the trail half faded, at y %v, is %v, want half of orange over black, (128, 43, 0)", y, half)
		}
		if end := picture.at(m.Vec2{X: 59, Y: y}); end.R > 10 || end.G > 10 {
			t.Errorf("the trail's faded end, at y %v, is %v, want nearly black", y, end)
		}
	}
}

func TestATerrainPolygonFillsWithATilingImageAndOneThatDoesNotTileContinuesItsEdge(t *testing.T) {
	// The tile of 4 texels, red at its top left, green at its top right,
	// blue at its bottom left and white at its bottom right, wrapping along
	// both axes, fills a pentagon, a fan of three triangles, whose UVs are
	// its positions over 16 units, a tile each, read nearest: each point
	// shows the quadrant of its tile its UV falls in, whatever tile, and
	// outside the pentagon is black. The same tile, wrapping along neither
	// axis, fills a quad from (40, -40) to (136, 56) whose UVs run 0 to 3:
	// past 1, it continues its edge, its right column's green and white and
	// its bottom row's blue and white.
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	pentagon := []m.Vec2{{X: -150, Y: -90}, {X: -40, Y: -100}, {X: -10, Y: -10}, {X: -50, Y: 100}, {X: -140, Y: 70}}
	var terrain []canvas.Vertex
	for _, position := range pentagon {
		terrain = append(terrain, canvas.Vertex{Position: position, UV: position.DivS(16)})
	}
	var plain []canvas.Vertex
	for _, corner := range []m.Vec2{{}, {X: 1}, {Y: 1}, {X: 1, Y: 1}} {
		plain = append(plain, canvas.Vertex{Position: m.Vec2{X: 40, Y: -40}.Add(corner.MulS(96)), UV: corner.MulS(3)})
	}
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.DrawList) {
		list.DrawTriangles(canvas.DrawTrianglesOptions{Image: canvas.ImageFromFile(tilePath, canvas.ImageOptions{Wrap: canvas.WrapBoth}), Filter: canvas.FilterNearest},
			canvas.Triangles[canvas.Vertex]{Vertices: terrain, Indices: []uint32{0, 1, 2, 0, 2, 3, 0, 3, 4}, Bounds: meshfile.CalculateBounds(terrain, canvas.Vertex.Position3)})
		list.DrawTriangles(canvas.DrawTrianglesOptions{Image: canvas.ImageFromFile(tilePath), Filter: canvas.FilterNearest},
			canvas.Triangles[canvas.Vertex]{Vertices: plain, Topology: meshfile.TopologyTriangleStrip, Bounds: meshfile.CalculateBounds(plain, canvas.Vertex.Position3)})
	}, &fileMounter{files: files})

	picture.expect(t, []probe{
		{"a tile's red near the origin", m.Vec2{X: -60, Y: 4}, opaqueRed},
		{"a tile's green near the origin", m.Vec2{X: -52, Y: 4}, opaqueGreen},
		{"a tile's blue near the origin", m.Vec2{X: -60, Y: 12}, opaqueBlue},
		{"a tile's white up and right", m.Vec2{X: -36, Y: -20}, opaqueWhite},
		{"a far tile's green", m.Vec2{X: -100, Y: -60}, opaqueGreen},
		{"a far tile's white", m.Vec2{X: -116, Y: 44}, opaqueWhite},
		{"right of the pentagon", m.Vec2{X: -5, Y: 60}, opaqueBlack},
		{"above the pentagon", m.Vec2{X: -100, Y: -110}, opaqueBlack},
		{"the plain quad's red", m.Vec2{X: 48, Y: -32}, opaqueRed},
		{"the plain quad's right edge continued", m.Vec2{X: 108, Y: -32}, opaqueGreen},
		{"the plain quad's corner continued", m.Vec2{X: 108, Y: 44}, opaqueWhite},
		{"the plain quad's bottom edge continued", m.Vec2{X: 48, Y: 44}, opaqueBlue},
	})
}

// isNearColour answers whether got is within tolerance of want in each
// channel.
func isNearColour(got, want color.NRGBA, tolerance int) bool {
	near := func(a, b uint8) bool { return int(a)-int(b) <= tolerance && int(b)-int(a) <= tolerance }
	return near(got.R, want.R) && near(got.G, want.G) && near(got.B, want.B) && near(got.A, want.A)
}
