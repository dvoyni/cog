package canvas_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

func TestAnInspectedCameraListsTheEntriesOfItsLayersItCulledAndOneCullingThemAllIsInspectedWithNoBatches(t *testing.T) {
	// The default camera's view is the window's points about the origin, so
	// a fill at x 100 is culled: by the first camera, among its drawn ones,
	// and by the second, which draws no other and does not clear.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	seen := canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 4, Height: 4}}
	away := canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: 100, Width: 4, Height: 4}, Order: 1}
	awayAbove := canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: 100, Width: 4, Height: 4}, Layer: 5}
	h.Record(func(list canvas.CommandList) {
		list.SetCamera(canvas.Camera{Clear: true, Layers: canvas.MakeLayer(0)})
		list.SetCamera(canvas.Camera{Layers: canvas.MakeLayer(5), Order: 1})
		list.DrawShape(seen)
		list.DrawShape(away)
		list.DrawShape(awayAbove)
	})

	h.Step()

	cameras := h.Frames()[0].Cameras
	if len(cameras) != 2 {
		t.Fatalf("the frame inspected %d cameras, want both, the second though it drew nothing", len(cameras))
	}
	entry := func(shape canvas.Shape, indexInCommandList int) canvas.InspectedEntry {
		return canvas.InspectedEntry{Kind: canvas.EntryKindShape, Shape: shape, Layer: shape.Layer, Order: shape.Order, IndexInCommandList: indexInCommandList}
	}
	drawing := cameras[0]
	if len(drawing.Batches) != 1 || !reflect.DeepEqual(drawing.Batches[0].Entries, []canvas.InspectedEntry{entry(seen, 0)}) {
		t.Errorf("the first camera drew %+v, want one batch of the fill it sees", drawing.Batches)
	}
	if want := []canvas.InspectedEntry{entry(away, 1)}; !reflect.DeepEqual(drawing.Culled, want) {
		t.Errorf("the first camera culled %+v, want the fill of its layer away from its view\n%+v", drawing.Culled, want)
	}
	culling := cameras[1]
	if culling.Window != window || culling.Camera.Order != 1 || len(culling.Batches) != 0 {
		t.Errorf("the second camera was inspected as %+v, want the camera of order 1 into the window, drawing no batch", culling)
	}
	if want := []canvas.InspectedEntry{entry(awayAbove, 2)}; !reflect.DeepEqual(culling.Culled, want) {
		t.Errorf("the second camera culled %+v, want the one fill of its layer\n%+v", culling.Culled, want)
	}
}

func TestAnInspectedFrameListsTheFirstUsesMadeAndReportsLoggedForItAndNoneOfTheFrameBefore(t *testing.T) {
	// The first frame packs the red image, which the red image of another
	// pivot shares, loads the tree's mesh file, lays out the label and makes
	// its two glyphs, and reports the missing mesh file; the second, drawing
	// the same, makes and reports nothing.
	const treePath = "meshes/tree.mesh"
	const missingPath = "meshes/missing.mesh"
	files := makeAtlasFiles(t)
	square := makeSquareVertices(10)
	files[treePath] = &fstest.MapFile{Data: encodeMeshFile(t, treePath, canvas.Triangles[canvas.Vertex]{Vertices: square, Indices: squareIndices, Topology: meshfile.TopologyTriangleList, Bounds: meshfile.CalculateBounds(square, canvas.Vertex.Position3)})}
	red := canvas.ImageFromFile(redPath)
	pivotedRed := canvas.ImageFromFile(redPath, canvas.ImageOptions{Pivot: canvas.PivotTopLeft})
	tree := canvas.MeshFileFromFile(treePath)
	record := func(list canvas.CommandList) {
		list.DrawSprite(canvas.Sprite{Image: red})
		list.DrawSprite(canvas.Sprite{Image: pivotedRed})
		list.DrawText(canvas.Text{String: "ab", Order: 1})
		list.DrawMesh(canvas.Mesh{File: tree, Order: 2})
		list.DrawMesh(canvas.Mesh{File: canvas.MeshFileFromFile(missingPath), Order: 3})
	}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMeshFileNotLoaded)
	h.Spawn(nil, nil, &fileMounter{files: files})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(record)
	h.Step()
	h.Record(record)
	h.Step()

	first := h.Frames()[0]
	second := h.Frames()[1]
	if want := []canvas.InspectedImage{{Image: red}}; !reflect.DeepEqual(first.PackedImages, want) {
		t.Errorf("the first frame packed %+v, want the red image's one frame, the pivoted one sharing it", first.PackedImages)
	}
	if want := []canvas.MeshFile{tree}; !reflect.DeepEqual(first.LoadedMeshFiles, want) {
		t.Errorf("the first frame loaded mesh files %+v, want the tree's alone, the missing one reported", first.LoadedMeshFiles)
	}
	if len(first.MadeLayouts) != 1 || first.MadeLayouts[0].String != "ab" {
		t.Errorf("the first frame made layouts of %+v, want the label's", first.MadeLayouts)
	}
	if len(first.MadeGlyphs) != 1 || first.MadeGlyphs[0].Text.String != "ab" || first.MadeGlyphs[0].Count != 2 {
		t.Errorf("the first frame made glyphs %+v, want the label's two", first.MadeGlyphs)
	}
	var notLoaded canvas.Report
	if len(first.Reports) != 1 || !errors.As(first.Reports[0], &notLoaded) || notLoaded.Kind != canvas.ReportKindMeshFileNotLoaded || !strings.Contains(notLoaded.Error(), missingPath) {
		t.Errorf("the first frame reported %v, want the missing mesh file's report alone", first.Reports)
	}
	if len(second.PackedImages)+len(second.LoadedMeshFiles)+len(second.MadeLayouts)+len(second.MadeGlyphs)+len(second.Reports) > 0 {
		t.Errorf("the second frame, drawing what the first did, made %+v, %+v, %+v and %+v and reported %v, want nothing", second.PackedImages, second.LoadedMeshFiles, second.MadeLayouts, second.MadeGlyphs, second.Reports)
	}
}

func TestAnInspectedFrameHoldsHowLongItsSortingAndDrawingTookByTheClock(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.LengthenNext(canvas.SortStopwatchName, 3*time.Millisecond)
	h.LengthenNext(canvas.DrawStopwatchName, 5*time.Millisecond)
	h.Record(func(list canvas.CommandList) {
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{Width: 4, Height: 4}})
	})

	h.Step()

	if frame := h.Frames()[0]; frame.SortTime != 3*time.Millisecond || frame.DrawTime != 5*time.Millisecond {
		t.Errorf("the frame was sorted in %v and drawn in %v, want 3ms and 5ms, as the clock measured them", frame.SortTime, frame.DrawTime)
	}
}

func TestAnInspectionsStringWritesTheFramesFirstUsesReportsAndTimesThenEachCameraItsBatchesAndCulls(t *testing.T) {
	const missingPath = "meshes/missing.mesh"
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMeshFileNotLoaded)
	h.Spawn(nil, nil, &fileMounter{files: makeAtlasFiles(t)})
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.LengthenNext(canvas.SortStopwatchName, 3*time.Millisecond)
	h.LengthenNext(canvas.DrawStopwatchName, 5*time.Millisecond)
	red := canvas.ImageFromFile(redPath)
	h.Record(func(list canvas.CommandList) {
		list.DrawSprite(canvas.Sprite{Image: red, Transform: m.Translation2x3(-8, 0)})
		list.DrawSprite(canvas.Sprite{Image: red, Transform: m.Translation2x3(8, 0)})
		list.DrawText(canvas.Text{String: "ab", Order: 1})
		list.DrawShape(canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: 100, Width: 4, Height: 4}, Order: 2})
		list.DrawMesh(canvas.Mesh{File: canvas.MeshFileFromFile(missingPath), Order: 3})
	})

	h.Step()

	frame := h.Frames()[0]
	text := frame.String()
	// Each line written is one of these, in this order; the report's line
	// is matched by its start, its error's wording canvas's own.
	want := []string{
		fmt.Sprintf("frame %d", frame.Beat.Tick),
		"  · red.png packed at its first draw",
		`  · "ab" laid out`,
		`  · 2 glyphs of "ab" made`,
		`  ! canvas: loading mesh file "meshes/missing.mesh"`,
		"  sorted in 3ms, drawn in 5ms",
		"",
		fmt.Sprintf("  camera 1, order 0 → window %v 64×48 px", window),
		"    viewport (0,0) 64×48 px · 1.00 px/unit · sees (-32,-24)–(32,24)",
		"    clears its viewport to (0, 0, 0, 1)",
		"    layer 0",
		"      batch 1: canvas default · canvas sprites sRGB · linear · no clip · 2 instances: red.png ×2",
		`      batch 2: canvas text · canvas glyphs · linear · no clip · 2 instances: "ab"`,
		"    3 entries drawn in 2 draws, 1 culled",
		"    culled: fill",
		"",
	}
	lines := strings.Split(text, "\n")
	if len(lines) != len(want) {
		t.Fatalf("String wrote %d lines, want %d:\n%s", len(lines), len(want), text)
	}
	for index, line := range lines {
		if !strings.HasPrefix(line, want[index]) || !strings.HasPrefix(want[index], "  !") && line != want[index] {
			t.Errorf("String's line %d is %q, want %q, in\n%s", index+1, line, want[index], text)
		}
	}
}
