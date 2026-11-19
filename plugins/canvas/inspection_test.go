package canvas_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestAnInspectedFrameAnswersItsCamerasBatchesInstanceCountsAndEntriesWithTheirSortKeysAndDrawListsPlaces(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &orderedRecorder{name: "recorder", lateOrder: 10, earlyOrder: 11})
	window := h.OpenWindow(platform.Size{Width: 64, Height: 48})
	first := canvas.Shape{Kind: canvas.Fill, Layer: 2, Order: -3}
	second := canvas.Shape{Kind: canvas.Fill, Layer: 1, Order: 4}
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(first)
		list.DrawShape(second)
	})

	h.Step()

	frame := h.Frames()[0]
	// canvastest's list comes first; the recorder's late task, registered
	// before its early one, runs after it. The entries are drawn in sort
	// order, each keeping its list's place and its own.
	entry := func(shape canvas.Shape, drawListIndex, indexInDrawList int) canvas.InspectedEntry {
		return canvas.InspectedEntry{Kind: canvas.EntryKindShape, Shape: shape, Layer: shape.Layer, Order: shape.Order, DrawListIndex: drawListIndex, IndexInDrawList: indexInDrawList}
	}
	want := []canvas.InspectedCamera{{
		Camera:   canvas.Camera{Clear: true, ClearColor: m.Color{A: 1}},
		Window:   window,
		Viewport: m.Recti{Width: 64, Height: 48},
		Batches: []canvas.InspectedBatch{{
			InstanceCount: 6,
			Entries: []canvas.InspectedEntry{
				entry(canvas.Shape{Order: 10}, 1, 0),
				entry(canvas.Shape{Order: 11}, 2, 0),
				entry(second, 0, 1),
				entry(canvas.Shape{Order: 10, Layer: 1}, 1, 1),
				entry(canvas.Shape{Order: 11, Layer: 1}, 2, 1),
				entry(first, 0, 0),
			},
		}},
	}}
	if !reflect.DeepEqual(frame.Cameras, want) {
		t.Errorf("the frame inspected was seen through\n%+v\nwant the default camera's one batch of six entries in sort order\n%+v", frame.Cameras, want)
	}
	lists := findCanvasLists(frame.Render)
	if frame.Render.Beat != frame.Beat || len(lists) != 1 || len(lists[0].Passes) != 1 || len(lists[0].Passes[0].Draws) != 1 || lists[0].Passes[0].Draws[0].InstanceCount != 6 {
		t.Errorf("render's inspection beside it, of beat %d, holds canvas's lists %+v, want render's of the same beat, %d, with one draw of 6 instances", frame.Render.Beat.Tick, lists, frame.Beat.Tick)
	}
}

func TestAnInspectedBatchAnswersTheClipItsEntriesShare(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	clip := m.Rect{X: -4, Y: -4, Width: 8, Height: 8}
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 10, Height: 10}, Order: 1})
		list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 10, Height: 10}, Order: 2, Clip: clip})
	})

	h.Step()

	var clips []m.Rect
	for _, batch := range h.Frames()[0].Cameras[0].Batches {
		clips = append(clips, batch.Clip)
	}
	if want := []m.Rect{{}, clip}; !slices.Equal(clips, want) {
		t.Errorf("an unclipped fill then a clipped one drew batches clipped to %v, want a batch each, %v", clips, want)
	}
}

func TestEachFrameCanvastestStepsIsInspectedBesideRendersInspectionOfIt(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Record(func(list canvas.DrawList) { list.DrawShape(canvas.Shape{}) })
		h.Step()
	}

	frames := h.Frames()
	recorded := h.Harness.Frames()
	if len(frames) != 3 || len(recorded) != 3 {
		t.Fatalf("3 steps answered %d frames inspected and %d recorded, want 3 of each", len(frames), len(recorded))
	}
	for index, frame := range frames {
		if frame.Beat != recorded[index].Beat || frame.Render.Beat != frame.Beat || len(findDrawnShapes(frame)) != 1 {
			t.Errorf("step %d's frame is of beat %d, render's beside it of beat %d, drawing %d shapes, want the beat recorded, %d, for both, drawing the one", index+1, frame.Beat.Tick, frame.Render.Beat.Tick, len(findDrawnShapes(frame)), recorded[index].Beat.Tick)
		}
	}
}

func TestOneInspectionIsDeliveredAtATimeAndARequestWhileOneIsPendingJoinsIt(t *testing.T) {
	h := rendertest.NewHarness(t)
	inspections := make(chan canvas.Inspection, 8)
	var beats []metronome.Beat
	h.Spawn(nil, canvas.NewPlugin(), &inspectionTool{
		duringFrame: func(frameIndex int, beat metronome.Beat, assets canvas.Assets, frame render.Frame) {
			beats = append(beats, beat)
			switch frameIndex {
			case 0:
				assets.RequestInspection()
				assets.RequestInspection()
			case 1, 4:
				assets.RequestInspection()
			}
		},
		onInspected: func(event canvas.InspectedEvent, assets canvas.Assets) {
			inspection, err := assets.ReadInspection(event)
			if err != nil {
				t.Errorf("reading the inspection of beat %d: %v", event.Beat.Tick, err)
			}
			inspections <- inspection
		},
	})

	for range 7 {
		h.Step()
	}

	for _, frameIndex := range []int{1, 5} {
		got := receiveInspection(t, inspections)
		if got.Beat != beats[frameIndex] || got.Render.Beat != got.Beat {
			t.Errorf("an inspection of beat %d, render's beside it of beat %d, was delivered, want one of beat %d, frame %d's, for both", got.Beat.Tick, got.Render.Beat.Tick, beats[frameIndex].Tick, frameIndex)
		}
	}
	select {
	case got := <-inspections:
		t.Errorf("an inspection of beat %d was delivered, want two alone: of the frame after the first requests, which the request during it joined, and of the frame after the last", got.Beat.Tick)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestARequestRenderJoinsToAFrameCanvasHasDrawnIsAnsweredByALaterFrame(t *testing.T) {
	h := rendertest.NewHarness(t)
	inspections := make(chan canvas.Inspection, 8)
	var beats []metronome.Beat
	h.Spawn(nil, canvas.NewPlugin(), &inspectionTool{
		// Another tool asks render alone for its inspection of frame 1.
		duringFrame: func(frameIndex int, beat metronome.Beat, assets canvas.Assets, frame render.Frame) {
			beats = append(beats, beat)
			if frameIndex == 0 {
				frame.RequestInspection()
			}
		},
		// After canvas's drawing of frame 1, render joins canvas's request
		// to its inspection of that frame, still recording, which canvas did
		// not write down.
		afterDrawing: func(frameIndex int, assets canvas.Assets) {
			if frameIndex == 1 {
				assets.RequestInspection()
			}
		},
		onInspected: func(event canvas.InspectedEvent, assets canvas.Assets) {
			inspection, _ := assets.ReadInspection(event)
			inspections <- inspection
		},
	})

	for range 6 {
		h.Step()
	}

	got := receiveInspection(t, inspections)
	if got.Beat.Tick <= beats[1].Tick || got.Render.Beat != got.Beat {
		t.Errorf("an inspection of beat %d, render's beside it of beat %d, was delivered, want one of a frame after beat %d, the frame drawn before the request, for both", got.Beat.Tick, got.Render.Beat.Tick, beats[1].Tick)
	}
	select {
	case got := <-inspections:
		t.Errorf("an inspection of beat %d was delivered, want one alone", got.Beat.Tick)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTimedFramesInspectedWhileLaterFramesAreWrittenDownAreEachDeliveredBesideRendersOwn(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	inspections := make(chan canvas.Inspection, 64)
	h.Spawn(nil, canvas.NewPlugin(), &orderedRecorder{name: "recorder", lateOrder: 1, earlyOrder: 2}, &inspectionTool{
		duringFrame: func(frameIndex int, beat metronome.Beat, assets canvas.Assets, frame render.Frame) {
			if frameIndex == 0 {
				frame.SetPassTimings(true)
				assets.RequestInspection()
			}
		},
		// A timed frame's inspection is delivered once its pass timings are
		// read back, a frame or more later, while canvas writes down the
		// frames of the request asked for here.
		onInspected: func(event canvas.InspectedEvent, assets canvas.Assets) {
			inspection, err := assets.ReadInspection(event)
			if err != nil {
				t.Errorf("reading the inspection of beat %d: %v", event.Beat.Tick, err)
			}
			inspections <- inspection
			assets.RequestInspection()
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 30 {
		h.Step()
	}

	if len(inspections) < 3 {
		t.Fatalf("30 timed frames delivered %d inspections, each asking for the next, want several", len(inspections))
	}
	for range len(inspections) {
		got := <-inspections
		if got.Render.Beat != got.Beat || len(findDrawnShapes(got)) != 4 || len(got.Render.PassTimings) == 0 {
			t.Errorf("an inspection of beat %d drew %d shapes, render's beside it of beat %d timed %d passes, want render's of the same beat, timed, beside the frame's 4 shapes", got.Beat.Tick, len(findDrawnShapes(got)), got.Render.Beat.Tick, len(got.Render.PassTimings))
		}
	}
}

func TestAnInspectionIsReadOnlyWhileItsEventsJobsRun(t *testing.T) {
	h := rendertest.NewHarness(t)
	inspections := make(chan canvas.Inspection, 4)
	var delivered canvas.InspectedEvent
	var readErr error
	h.Spawn(nil, canvas.NewPlugin(), &inspectionTool{
		duringFrame: func(frameIndex int, beat metronome.Beat, assets canvas.Assets, frame render.Frame) {
			switch frameIndex {
			case 0:
				assets.RequestInspection()
			case 4:
				_, readErr = assets.ReadInspection(delivered)
			}
		},
		onInspected: func(event canvas.InspectedEvent, assets canvas.Assets) {
			delivered = event
			inspection, _ := assets.ReadInspection(event)
			inspections <- inspection
		},
	})

	for range 5 {
		h.Step()
	}

	receiveInspection(t, inspections)
	if readErr == nil {
		t.Error("reading an inspection after its event's jobs ran answered no error")
	}
}

// receiveInspection answers the next inspection inspections delivers,
// failing the test when none comes within a second.
func receiveInspection(t *testing.T, inspections <-chan canvas.Inspection) canvas.Inspection {
	t.Helper()
	select {
	case inspection := <-inspections:
		return inspection
	case <-time.After(time.Second):
		t.Fatal("no inspection was delivered")
		return canvas.Inspection{}
	}
}
