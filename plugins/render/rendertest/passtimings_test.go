package rendertest_test

import (
	"cmp"
	"errors"
	"math"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// passTimingsDelivery is one render.PassTimingsEvent and the timings read
// for it, copied.
type passTimingsDelivery struct {
	event   render.PassTimingsEvent
	timings []render.PassTiming
}

// timingRun is what a timing test's harness delivered: each event's
// timings, in the order delivered, and each inspection.
type timingRun struct {
	h           *rendertest.Harness
	inspections chan render.Inspection
	// delivered is sent to once the timings of each event are read.
	delivered chan struct{}
	// mutex guards deliveries.
	mutex      sync.Mutex
	deliveries []passTimingsDelivery
}

// spawnTimingHarness spawns h with config, the draw tests' files and a PNG
// with mipmaps beside them as mask.png, and opens a window. In each frame's
// recording it turns pass timings on in the first frame, runs duringFrame,
// unless nil, and records the timed lists recordTimedLists records. It
// keeps each pass timings event's timings, read while its jobs run, and
// each inspection delivered.
func spawnTimingHarness(t *testing.T, h *rendertest.Harness, config *render.Config, duringFrame func(frameIndex int, inventory render.Inventory, frame render.Frame)) *timingRun {
	t.Helper()
	run := &timingRun{h: h, inspections: make(chan render.Inspection, 4), delivered: make(chan struct{}, 64)}
	files := makeDrawFiles(t)
	mask, _ := makePNG(t, 6, 4, 255)
	putFile(t, files, "mask.png", mask)
	var window platform.Window
	var scene timedScene
	h.Spawn(config, &loaderPlugin{
		files: files,
		duringFrame: func(frameIndex int, inventory render.Inventory, frame render.Frame) {
			if frameIndex == 0 {
				scene = timedScene{drawScene: makeDrawScene(inventory), material: inventory.MakeMaterial(render.MaterialDescriptor{Label: "plain", Shader: inventory.AcquireShader("plain.wgsl")})}
				frame.SetPassTimings(true)
			}
			if duringFrame != nil {
				duringFrame(frameIndex, inventory, frame)
			}
			recordTimedLists(frame, window, scene)
		},
		onInspected: func(event render.InspectedEvent, frame render.Frame) {
			inspection, err := frame.ReadInspection(event)
			if err != nil {
				t.Errorf("reading the inspection of beat %d: %v", event.Beat.Tick, err)
			}
			run.inspections <- inspection
		},
		onPassTimings: func(event render.PassTimingsEvent, frame render.Frame) {
			timings, err := frame.ReadPassTimings(event)
			if err != nil {
				t.Errorf("reading the pass timings of beat %d: %v", event.Beat.Tick, err)
			}
			run.mutex.Lock()
			run.deliveries = append(run.deliveries, passTimingsDelivery{event: event, timings: slices.Clone(timings)})
			run.mutex.Unlock()
			run.delivered <- struct{}{}
		},
	})
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	return run
}

// readDeliveries answers each delivery kept.
func (r *timingRun) readDeliveries() []passTimingsDelivery {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.deliveries)
}

// waitForDeliveries waits until count events' timings are read: render
// publishes them from the job that found them mapped, a publication of its
// own that a Step does not wait for.
func (r *timingRun) waitForDeliveries(count int) {
	for range count {
		r.h.WaitForReceive("the PassTimingsEvent", r.delivered)
	}
}

// timedScene is what the timing tests draw: the draw tests' scene, and a
// material of plain.wgsl.
type timedScene struct {
	drawScene
	material render.Material
}

// recordTimedLists records, at window, the lists the timing tests time: at
// order 20, a list "late" with a pass "late pass"; and before it, at order
// 5, a list "early" with a compute pass "early compute" and a pass "early
// pass", which draws scene's quad.
func recordTimedLists(frame render.Frame, window platform.Window, scene timedScene) {
	late := frame.BeginCommandList("late", 20)
	latePass := late.BeginPass(render.PassDescriptor{Label: "late pass", ColorTargets: []render.ColorTarget{{Window: window}}})
	latePass.End()
	late.End()
	early := frame.BeginCommandList("early", 5)
	computePass := early.BeginComputePass("early compute")
	computePass.End()
	pass := early.BeginPass(render.PassDescriptor{Label: "early pass", ColorTargets: []render.ColorTarget{{Window: window, Clear: true}}})
	pass.SetMaterial(scene.material)
	pass.Draw(scene.indexedMesh, render.DrawArgs{})
	pass.End()
	early.End()
}

// readTimedPasses answers the passes of frame that wrote timestamps, in the
// order gfx submitted them, which is the order of their lists, each timed
// as gfx's recording adapter times it: a nanosecond for each command it
// recorded, its beginning among them; and its order, read from its list's
// label by timedListOrders.
func readTimedPasses(frame recording.Frame) []render.PassTiming {
	var timings []render.PassTiming
	for _, buffer := range readSubmittedBuffers(frame) {
		for _, command := range buffer.Commands {
			var label string
			var timestampWrites gfx.PassTimestampWrites
			switch arguments := command.Arguments.(type) {
			case gfx.RenderPassDescriptor:
				label = arguments.Label
				timestampWrites = arguments.TimestampWrites
			case gfx.ComputePassDescriptor:
				label = arguments.Label
				timestampWrites = arguments.TimestampWrites
			}
			if timestampWrites.QuerySet != (gfx.QuerySet{}) {
				timings = append(timings, render.PassTiming{Order: timedListOrders[buffer.Label], Label: label, Duration: time.Duration(1 + len(command.Commands))})
			}
		}
	}
	return timings
}

// timedListOrders are the orders of the lists the timing tests time, by
// their labels, render's own mip list's among them.
var timedListOrders = map[string]render.Order{"render: mips": math.MinInt32, "early": 5, "late": 20}

func TestEachPassOfATimedFrameRendersOwnAmongThemIsTimedByItsListsOrder(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	run := spawnTimingHarness(t, h, nil, func(frameIndex int, inventory render.Inventory, frame render.Frame) {
		if frameIndex == 1 {
			inventory.AcquireTexture("mask.png", render.TextureOptions{Mipmaps: true})
		}
	})

	h.Step()
	h.Step()
	h.Step()
	run.waitForDeliveries(1)

	frames := h.Frames()
	recorded := h.RecordedFrames()
	delivery := run.readDeliveries()[0]
	if got, want := delivery.event.Beat, frames[1].Beat; got != want {
		t.Errorf("the first timings are of beat %d, want %d, the first frame after timings were turned on", got.Tick, want.Tick)
	}
	want := readTimedPasses(recorded[1])
	if !slices.ContainsFunc(want, func(timing render.PassTiming) bool { return timing.Order == math.MinInt32 }) {
		t.Fatalf("the timed frame generated no mips: %+v", want)
	}
	if !reflect.DeepEqual(delivery.timings, want) {
		t.Errorf("the timings are\n%+v\nwant\n%+v", delivery.timings, want)
	}
	var usersLabels []string
	for _, timing := range delivery.timings {
		if timing.Order != math.MinInt32 {
			usersLabels = append(usersLabels, timing.Label)
		}
	}
	if want := []string{"early compute", "early pass", "late pass"}; !slices.Equal(usersLabels, want) {
		t.Errorf("the users' passes timed are %v, want %v", usersLabels, want)
	}
	if untimed := readTimedPasses(recorded[0]); len(untimed) != 0 {
		t.Errorf("the frame turning timings on timed %+v, want none", untimed)
	}
}

func TestTurningPassTimingsOffEndsTheirEventsAndFreesTheirQuerySetsOnceIdle(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	run := spawnTimingHarness(t, h, &render.Config{TemporaryIdleFrames: 2}, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		if frameIndex == 3 {
			frame.SetPassTimings(false)
		}
	})

	for range 9 {
		h.Step()
	}
	run.waitForDeliveries(3)

	frames := h.Frames()
	var beats []metronome.Beat
	for _, delivery := range run.readDeliveries() {
		beats = append(beats, delivery.event.Beat)
	}
	if want := []metronome.Beat{frames[1].Beat, frames[2].Beat, frames[3].Beat}; !slices.Equal(beats, want) {
		t.Errorf("timings were delivered for beats %v, want those of the frames timed, %v", beats, want)
	}
	for frameIndex, frame := range h.RecordedFrames()[4:] {
		if timed := readTimedPasses(frame); len(timed) != 0 {
			t.Errorf("frame %d, after timings were turned off, timed %+v", frameIndex+4, timed)
		}
	}
	created, released := findQuerySets(h.Calls())
	slices.SortFunc(released, func(a, b gfx.QuerySet) int { return cmp.Compare(a.Index(), b.Index()) })
	if len(created) == 0 || !slices.Equal(released, created) {
		t.Errorf("render made the query sets %v and released %v, want every one released", created, released)
	}
}

func TestPassTimingsTurnedOnWhereTheyAreUnavailableAreReportedOnceAndFramesDrawAsBefore(t *testing.T) {
	for _, test := range []struct {
		name  string
		ready func(h *rendertest.Harness)
	}{
		{"a backend without timestamp queries", func(*rendertest.Harness) {}},
		{"a device requested without timestamp queries", func(h *rendertest.Harness) { h.EnableBackendTimestamps() }},
		{"a backend without query sets", func(h *rendertest.Harness) {
			h.EnableTimestamps()
			h.AnswerNext(recording.CallKindCreateQuerySet, recording.Answer{Error: gfx.ErrQuerySetsUnavailable})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			test.ready(h)
			h.ExpectReport(render.ReportKindPassTimingsUnavailable)
			run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
				if frameIndex%2 == 0 {
					frame.SetPassTimings(true)
				}
			})

			for range 5 {
				h.Step()
			}

			if count := countReports(h, render.ReportKindPassTimingsUnavailable); count != 1 {
				t.Errorf("pass timings were reported unavailable %d times, want once", count)
			}
			for frameIndex, frame := range h.RecordedFrames() {
				if timed := readTimedPasses(frame); len(timed) != 0 {
					t.Errorf("frame %d timed %+v", frameIndex, timed)
				}
			}
			for frameIndex, frame := range h.Frames() {
				if passCount := len(frame.CommandLists[1].Passes) + len(frame.CommandLists[2].Passes); passCount != 2 {
					t.Errorf("frame %d drew %d passes, want 2", frameIndex, passCount)
				}
			}
			if deliveries := run.readDeliveries(); len(deliveries) != 0 {
				t.Errorf("timings were delivered: %+v", deliveries)
			}
		})
	}
}

func TestAnInspectionOfATimedFrameWaitsForItsPassTimingsAndHoldsThemAndOneOfAFrameNotTimedDoesNot(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		switch frameIndex {
		case 0, 3:
			frame.RequestInspection()
		case 1:
			frame.SetPassTimings(false)
		}
	})

	h.Step()
	h.Step()
	if len(run.inspections) != 0 {
		t.Error("the inspection was delivered before its frame's pass timings")
	}
	h.Step()
	run.waitForDeliveries(1)
	inspection := receiveInspection(t, run.inspections)

	if want := run.readDeliveries()[0]; inspection.Beat != want.event.Beat || !reflect.DeepEqual(inspection.PassTimings, want.timings) {
		t.Errorf("the inspection of beat %d holds the pass timings\n%+v\nwant those of beat %d\n%+v", inspection.Beat.Tick, inspection.PassTimings, want.event.Beat.Tick, want.timings)
	}
	if len(inspection.PassTimings) == 0 {
		t.Error("the inspection holds no pass timings")
	}
	compareInspections(t, eraseRendersOwn(inspection), h.Frames()[1])

	h.Step()
	h.Step()
	if untimed := receiveInspection(t, run.inspections); untimed.Beat != h.Frames()[4].Beat || untimed.PassTimings != nil {
		t.Errorf("the inspection of beat %d, not timed, holds the timings %+v, want that of beat %d delivered without them", untimed.Beat.Tick, untimed.PassTimings, h.Frames()[4].Beat.Tick)
	}
}

func TestARequestWhileAnInspectionAwaitsItsPassTimingsInspectsTheNextFrame(t *testing.T) {
	// Frame 1's inspection waits for its pass timings through frame 2,
	// whose request, of a frame after the one it inspects, is held back by
	// none: frame 3, the next to begin, is inspected too, each delivered
	// once its timings are read back.
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		if frameIndex == 0 || frameIndex == 2 {
			frame.RequestInspection()
		}
	})

	for range 7 {
		h.Step()
	}

	frames := h.Frames()
	for _, frameIndex := range []int{1, 3} {
		if got := receiveInspection(t, run.inspections); got.Beat != frames[frameIndex].Beat {
			t.Errorf("an inspection of beat %d was delivered, want one of beat %d, frame %d's", got.Beat.Tick, frames[frameIndex].Beat.Tick, frameIndex)
		}
	}
}

func TestATimedFramesPassesAfterItsMostAreReportedAndUntimed(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	h.RecordNothing()
	h.ExpectReport(render.ReportKindPassUntimed)
	const maxTimedPassCount = 2048
	run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		list := frame.BeginCommandList("many", drawOrder)
		for range maxTimedPassCount {
			pass := list.BeginComputePass("many")
			pass.End()
		}
		list.End()
	})

	h.Step()
	h.Step()
	h.Step()
	run.waitForDeliveries(1)

	if timings := run.readDeliveries()[0].timings; len(timings) != maxTimedPassCount {
		t.Errorf("%d passes were timed, want %d", len(timings), maxTimedPassCount)
	}
	if count := countReports(h, render.ReportKindPassUntimed); count != 2 {
		t.Errorf("untimed passes were reported %d times, want once for each of the 2 frames timed", count)
	}
}

func TestAPassOfATimedFrameAllocatesNothingOnceWarm(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	h.RecordNothing()
	const passCount = 100
	const warmFrameCount = 5
	const frameCount = 8
	var allocations [frameCount]float64
	run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		list := frame.BeginCommandList("passes", drawOrder)
		timePass := func() {
			pass := list.BeginComputePass("timed")
			pass.End()
		}
		if frameIndex < warmFrameCount {
			for range passCount + 1 {
				timePass()
			}
		} else {
			allocations[frameIndex] = testing.AllocsPerRun(passCount, timePass)
		}
		list.End()
	})

	for range frameCount {
		h.Step()
	}
	run.waitForDeliveries(frameCount - 2)

	for frameIndex, count := range allocations[warmFrameCount:] {
		if count != 0 {
			t.Errorf("frame %d, warm, allocated %v times a timed pass, want none", warmFrameCount+frameIndex, count)
		}
	}
}

func TestTheQuerySetsOfFramesWhoseTimingsWereDeliveredAreReused(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	const frameCount = 10
	run := spawnTimingHarness(t, h, nil, nil)

	for range frameCount {
		h.Step()
	}
	run.waitForDeliveries(frameCount - 2)

	calls := h.Calls()
	if created, _ := findQuerySets(calls); len(created) > 3 {
		t.Errorf("render made %d query sets for %d frames timed, want those of the frames in flight reused", len(created), frameCount-1)
	}
	if created := findStagingBuffers(calls); len(created) > 3 {
		t.Errorf("render made %d staging buffers for %d frames timed, want those of the frames in flight reused", len(created), frameCount-1)
	}
}

func TestPassTimingsAreReadOnlyWhileTheirEventsJobsRun(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.EnableTimestamps()
	var lateRead error
	var run *timingRun
	run = spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
		if frameIndex == 4 {
			_, lateRead = frame.ReadPassTimings(run.readDeliveries()[0].event)
		}
	})

	h.Step()
	h.Step()
	h.Step()
	run.waitForDeliveries(1)
	h.Step()
	h.Step()

	if lateRead == nil {
		t.Error("ReadPassTimings after the event's jobs answered no error")
	}
}

func TestPassTimingsWhoseMapFailsAreNotDeliveredAndTheirInspectionComesWithoutThem(t *testing.T) {
	errMapRefused := errors.New("the map is refused")
	for _, test := range []struct {
		name string
		// failed is the call whose answer fails the map: its start, or the
		// status gfx polls.
		failed recording.CallKind
	}{
		{name: "when it starts", failed: recording.CallKindMapBuffer},
		{name: "when it resolves", failed: recording.CallKindMapStatus},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			h.EnableTimestamps()
			h.ExpectLoggedError(errMapRefused)
			run := spawnTimingHarness(t, h, nil, func(frameIndex int, _ render.Inventory, frame render.Frame) {
				if frameIndex == 0 {
					frame.RequestInspection()
				}
			})
			h.AnswerNext(test.failed, recording.Answer{Error: errMapRefused})

			for range 4 {
				h.Step()
			}
			run.waitForDeliveries(1)
			inspection := receiveInspection(t, run.inspections)

			frames := h.Frames()
			if inspection.Beat != frames[1].Beat || inspection.PassTimings != nil {
				t.Errorf("the inspection of beat %d holds the timings %+v, want that of beat %d without them", inspection.Beat.Tick, inspection.PassTimings, frames[1].Beat.Tick)
			}
			if beat := run.readDeliveries()[0].event.Beat; beat != frames[2].Beat {
				t.Errorf("the first timings delivered are of beat %d, want %d, the frame after the one whose map failed", beat.Tick, frames[2].Beat.Tick)
			}
		})
	}
}

// findQuerySets answers the query sets calls created and released, in
// the order of each.
func findQuerySets(calls []recording.Call) (created, released []gfx.QuerySet) {
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindCreateQuerySet:
			if call.Error == nil {
				created = append(created, call.Arguments.(recording.CreateQuerySetArguments).QuerySet)
			}
		case recording.CallKindReleaseQuerySet:
			released = append(released, call.Arguments.(gfx.QuerySet))
		}
	}
	return created, released
}
