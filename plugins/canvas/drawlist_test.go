package canvas_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestEntriesDrawByLayerThenOrderThenTheirDrawListsRegistrationOrderThenCallOrder(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &orderedRecorder{name: "first recorder", lateOrder: 5, earlyOrder: 5}, &orderedRecorder{name: "second recorder", lateOrder: 5, earlyOrder: 5})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Order: 9, Layer: -1})
		list.DrawShape(canvas.Shape{Order: 5, Layer: 1})
		list.DrawShape(canvas.Shape{Order: 5, Tint: m.Color{R: 1, A: 1}})
		list.DrawShape(canvas.Shape{Order: 5, Tint: m.Color{G: 1, A: 1}})
		list.DrawShape(canvas.Shape{Order: 4, Layer: 1})
	})

	h.Step()

	var got [][2]int
	for _, entry := range findDrawnEntries(h.Frames()[0]) {
		got = append(got, [2]int{entry.DrawListIndex, entry.IndexInDrawList})
	}
	// canvastest's list, 0, comes first, its plugin listed right after
	// canvas; each recorder's late task, 1 and 3, was registered before its
	// early one, 2 and 4, which runs first. Each recorder draws order 5 at
	// layers 0 and 1.
	want := [][2]int{
		{0, 0},
		{0, 2}, {0, 3}, {1, 0}, {2, 0}, {3, 0}, {4, 0},
		{0, 4}, {0, 1}, {1, 1}, {2, 1}, {3, 1}, {4, 1},
	}
	if !slices.Equal(got, want) {
		t.Errorf("the frame drew its entries from these draw lists' places, and their own, %v, want %v", got, want)
	}
}

func TestAFramesShapesAreForgottenOnceItIsDrawn(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) { list.DrawShape(canvas.Shape{Order: 1}) })
	h.Step()

	h.Step()

	if shapes := findDrawnShapes(h.Frames()[1]); len(shapes) != 0 {
		t.Errorf("a frame recording nothing drew %+v, the frame before's shapes", shapes)
	}
}

func TestAnEntryRecordedOutsideTheFrameIsRefusedAndReportedOncePerTask(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &lateRecorder{})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindEntryOutsideFrame)

	h.Step()
	h.Step()

	for index, frame := range h.Frames() {
		if shapes := findDrawnShapes(frame); len(shapes) != 0 {
			t.Errorf("frame %d drew %+v, shapes recorded after a frame's drawing began", index+1, shapes)
		}
	}
	var kinds []canvas.ReportKind
	for _, report := range h.Reports() {
		kinds = append(kinds, report.Kind)
	}
	if want := []canvas.ReportKind{canvas.ReportKindEntryOutsideFrame, canvas.ReportKindEntryOutsideFrame}; !slices.Equal(kinds, want) {
		t.Errorf("two tasks each recording twice outside two frames reported %v, want one report for each task", kinds)
	}
}

func TestAFrameWithNeitherEntriesNorCamerasBeginsNoCommandList(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	h.Record(func(list canvas.DrawList) { list.DrawShape(canvas.Shape{}) })

	h.Step()

	frames := h.Frames()
	if len(frames) != 2 {
		t.Fatalf("two beats drew %d frames, want 2", len(frames))
	}
	if lists := findCanvasLists(frames[0].Render); len(frames[0].Cameras) != 0 || len(lists) != 0 {
		t.Errorf("a frame with neither entries nor cameras was seen through cameras %+v and began canvas's command lists %+v, want none", frames[0].Cameras, lists)
	}
	if lists := findCanvasLists(frames[1].Render); len(lists) != 1 {
		t.Errorf("a frame with an entry and no camera began %d command lists of canvas's, want the default camera's", len(lists))
	}
}

func TestTwoJobsOfOneTaskAndOtherTasksRecordTogetherIntoTheFrame(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &parallelRecorder{})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for frame := range 4 {
		h.Step()

		if got, want := len(findDrawnShapes(h.Frames()[frame])), 3*parallelShapeCount; got != want {
			t.Errorf("frame %d's three jobs of two tasks recording together drew %d shapes, want %d", frame+1, got, want)
		}
	}
}

func TestFrameEventCarriesRendersBeatAndTimingSignals(t *testing.T) {
	h := canvastest.NewHarness(t)
	watcher := &frameWatcher{}
	h.Spawn(nil, nil, watcher)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.LengthenNextAcquire(3 * time.Millisecond)

	for range 3 {
		// A fill has canvas begin a command list, so each frame submits work
		// the GPU's latency is measured on.
		h.Record(func(list canvas.DrawList) { list.DrawShape(canvas.Shape{}) })
		h.Step()
	}

	canvasEvents, renderEvents := watcher.events()
	if len(canvasEvents) != 3 || len(renderEvents) != 3 {
		t.Fatalf("3 beats published %d canvas.FrameEvents and %d render.FrameEvents, want 3 of each", len(canvasEvents), len(renderEvents))
	}
	for index, renderEvent := range renderEvents {
		got := canvasEvents[index]
		if got.Beat != renderEvent.Beat || got.AcquireWait != renderEvent.AcquireWait || got.GPULatency != renderEvent.GPULatency {
			t.Errorf("beat %d's canvas.FrameEvent is %+v, want render's beat and timing signals, %+v", index+1, got, renderEvent)
		}
	}
	if renderEvents[1].AcquireWait == 0 || renderEvents[2].GPULatency == 0 {
		t.Errorf("render's FrameEvents carry no AcquireWait or GPULatency to compare: %+v", renderEvents)
	}
}

// findDrawnEntries answers the entries frame drew, camera after camera, in
// the order each drew them.
func findDrawnEntries(frame canvas.Inspection) []canvas.InspectedEntry {
	var entries []canvas.InspectedEntry
	for _, camera := range frame.Cameras {
		for _, batch := range camera.Batches {
			entries = append(entries, batch.Entries...)
		}
	}
	return entries
}

// findDrawnShapes answers the shapes frame drew, camera after camera, in the
// order each drew them.
func findDrawnShapes(frame canvas.Inspection) []canvas.Shape {
	var shapes []canvas.Shape
	for _, entry := range findDrawnEntries(frame) {
		shapes = append(shapes, entry.Shape)
	}
	return shapes
}

// findCanvasLists answers the command lists of frame that canvas began.
func findCanvasLists(frame render.Inspection) []render.InspectedCommandList {
	var lists []render.InspectedCommandList
	for _, list := range frame.CommandLists {
		if list.Label == "canvas" {
			lists = append(lists, list)
		}
	}
	return lists
}
