package canvas_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestEachRecordingTaskRecordsIntoItsOwnLaneAndTheLanesMergeInRegistrationOrder(t *testing.T) {
	h := canvastest.NewHarness(t)
	reader := &drawStateReader{}
	h.Spawn(nil, nil, reader, &orderedRecorder{name: "first recorder", lateOrder: 10, earlyOrder: 11}, &orderedRecorder{name: "second recorder", lateOrder: 20, earlyOrder: 21})
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Order: 1})
		list.DrawShape(canvas.Shape{Order: 1, Layer: 1})
	})

	h.Step()

	got := reader.mergedShapes()
	// canvastest's lane comes first, its plugin listed right after canvas;
	// each recorder's late task was registered before its early one, which
	// runs first.
	want := []canvas.Shape{{Order: 1}, {Order: 1, Layer: 1}, {Order: 10}, {Order: 10, Layer: 1}, {Order: 11}, {Order: 11, Layer: 1}, {Order: 20}, {Order: 20, Layer: 1}, {Order: 21}, {Order: 21, Layer: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("the frame's shapes merged as %+v, want %+v", got, want)
	}
}

func TestAFramesShapesAreForgottenOnceItIsDrawn(t *testing.T) {
	h := canvastest.NewHarness(t)
	reader := &drawStateReader{}
	h.Spawn(nil, nil, reader)
	h.Record(func(list canvas.DrawList) { list.DrawShape(canvas.Shape{Order: 1}) })
	h.Step()

	h.Step()

	if shapes := reader.mergedShapes(); len(shapes) != 0 {
		t.Errorf("a frame recording nothing merged %+v, the frame before's shapes", shapes)
	}
}

func TestAnEntryRecordedOutsideTheFrameIsRefusedAndReportedOncePerTask(t *testing.T) {
	h := canvastest.NewHarness(t)
	reader := &drawStateReader{}
	h.Spawn(nil, nil, reader, &lateRecorder{})
	h.ExpectReport(canvas.ReportKindEntryOutsideFrame)

	h.Step()
	h.Step()

	if shapes := reader.mergedShapes(); len(shapes) != 0 {
		t.Errorf("the frame merged %+v, shapes recorded after its drawing began", shapes)
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
	if lists := findCanvasLists(frames[0]); len(lists) != 0 {
		t.Errorf("a frame with neither entries nor cameras began canvas's command lists %+v, want none", lists)
	}
	if lists := findCanvasLists(frames[1]); len(lists) != 1 {
		t.Errorf("a frame with an entry and no camera began %d command lists of canvas's, want the default camera's", len(lists))
	}
}

func TestTwoJobsOfOneTaskAndOtherTasksRecordTogetherIntoTheFrame(t *testing.T) {
	h := canvastest.NewHarness(t)
	reader := &drawStateReader{}
	h.Spawn(nil, nil, reader, &parallelRecorder{})

	for frame := range 4 {
		h.Step()

		if got, want := len(reader.mergedShapes()), 3*parallelShapeCount; got != want {
			t.Errorf("frame %d's three jobs of two tasks recording together merged %d shapes, want %d", frame+1, got, want)
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
