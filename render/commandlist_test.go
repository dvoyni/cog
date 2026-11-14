package render_test

import (
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

// stepAndReadFrame steps h once and answers the frame the beat drew, read back.
func stepAndReadFrame(t *testing.T, h *rendertest.Harness) render.Inspection {
	t.Helper()
	h.Step()
	frames := h.Frames()
	if len(frames) == 0 {
		t.Fatal("the harness read back no frame")
	}
	return frames[len(frames)-1]
}

// makeReportKinds answers the kind of each report h's kernel logged so far.
func makeReportKinds(h *rendertest.Harness) []render.ReportKind {
	var kinds []render.ReportKind
	for _, report := range h.Reports() {
		kinds = append(kinds, report.Kind)
	}
	return kinds
}

// recordPass begins a pass of descriptor in list and ends it.
func recordPass(list *render.CommandList, descriptor render.PassDescriptor) {
	pass := list.BeginPass(descriptor)
	pass.End()
}

func TestTheReaderShowsListsFilledConcurrentlyByOrderAndPassesWithTheirLabelsTargetsLoadsAndStores(t *testing.T) {
	h := rendertest.NewHarness(t)
	var first, second platform.Window
	red := render.Color{R: 1, A: 1}
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		late := frame.BeginCommandList("late", 5)
		recordPass(&late, render.PassDescriptor{Label: "overlay", ColorTargets: []render.ColorTarget{{Window: first}}})
		late.End()
	}, func(frame render.Frame) {
		early := frame.BeginCommandList("early", -3)
		recordPass(&early, render.PassDescriptor{Label: "scene", ColorTargets: []render.ColorTarget{
			{Window: first, Clear: true, ClearColor: red},
			{Window: second, Discard: true},
		}})
		recordPass(&early, render.PassDescriptor{Label: "interface", ColorTargets: []render.ColorTarget{{Window: second}}})
		early.End()
	}}})
	first = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	second = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	got := stepAndReadFrame(t, h)

	want := render.Inspection{CommandLists: []render.InspectedCommandList{
		{Passes: []render.InspectedPass{
			{Label: "scene", ColorTargets: []render.ColorTarget{
				{Window: first, Clear: true, ClearColor: red},
				{Window: second, Discard: true},
			}},
			{Label: "interface", ColorTargets: []render.ColorTarget{{Window: second}}},
		}},
		{Passes: []render.InspectedPass{
			{Label: "overlay", ColorTargets: []render.ColorTarget{{Window: first}}},
		}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want %+v", got, want)
	}
}

func TestListsAtATakenOrderOrRendersOwnAreRefusedAndReportedOnce(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindOrderTaken)
	h.ExpectReport(render.ReportKindOrderReserved)
	var window platform.Window
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		target := []render.ColorTarget{{Window: window}}
		for _, list := range []struct {
			label string
			order render.Order
		}{{"first", 1}, {"second", 1}, {"own", math.MinInt32}} {
			commandList := frame.BeginCommandList(list.label, list.order)
			recordPass(&commandList, render.PassDescriptor{Label: list.label, ColorTargets: target})
			commandList.End()
		}
	}}})
	window = h.OpenWindow(platform.Size{Width: 64, Height: 48})

	got := stepAndReadFrame(t, h)

	if kinds, want := makeReportKinds(h), []render.ReportKind{render.ReportKindOrderTaken, render.ReportKindOrderReserved}; !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v", kinds, want)
	}
	want := render.Inspection{CommandLists: []render.InspectedCommandList{
		{Passes: []render.InspectedPass{{Label: "first", ColorTargets: []render.ColorTarget{{Window: window}}}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want only the first list's pass, %+v", got, want)
	}
}

func TestAListBegunWhileNoFrameIsRecordingRecordsIntoNothingReportedOnce(t *testing.T) {
	for _, test := range []struct {
		name        string
		beatsBefore int
	}{
		{name: "before the first frame", beatsBefore: 0},
		{name: "between frames", beatsBefore: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			h.ExpectReport(render.ReportKindListOutsideFrame)
			watcher := &recorder{onWindowOpened: func(frame render.Frame) {
				list := frame.BeginCommandList("outside", 1)
				list.End()
			}}
			h.Spawn(nil, watcher)
			for range test.beatsBefore {
				h.Step()
			}
			framesBefore := len(h.Frames())

			h.OpenWindow(platform.Size{Width: 64, Height: 48})
			select {
			case <-watcher.windowRecorded:
			case <-time.After(5 * time.Second):
				t.Fatal("the task on the window's opening did not run")
			}

			if kinds, want := makeReportKinds(h), []render.ReportKind{render.ReportKindListOutsideFrame}; !slices.Equal(kinds, want) {
				t.Errorf("render reported kinds %v, want %v", kinds, want)
			}
			if frames := h.Frames(); len(frames) != framesBefore {
				t.Errorf("the list submitted %+v to gfx, want nothing", frames[framesBefore:])
			}
		})
	}
}

func TestAPassIntoAWindowWithNoFramebufferIsSkippedSilently(t *testing.T) {
	h := rendertest.NewHarness(t)
	var shown, empty platform.Window
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		list := frame.BeginCommandList("scene", 1)
		recordPass(&list, render.PassDescriptor{Label: "empty", ColorTargets: []render.ColorTarget{{Window: empty}}})
		recordPass(&list, render.PassDescriptor{Label: "both", ColorTargets: []render.ColorTarget{{Window: shown}, {Window: empty}}})
		recordPass(&list, render.PassDescriptor{Label: "shown", ColorTargets: []render.ColorTarget{{Window: shown}}})
		list.End()
	}}})
	shown = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	empty = h.OpenWindow(platform.Size{})

	got := stepAndReadFrame(t, h)

	want := render.Inspection{CommandLists: []render.InspectedCommandList{
		{Passes: []render.InspectedPass{{Label: "shown", ColorTargets: []render.ColorTarget{{Window: shown}}}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want only the pass into the window shown, %+v", got, want)
	}
}

func TestAPassWhoseTargetsAreNoneTooManyNamelessOrDisagreeIsRefusedReportedOnceAndRecordsNothing(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.ExpectReport(render.ReportKindPassTargetsRefused)
	var large, small platform.Window
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		list := frame.BeginCommandList("scene", 1)
		recordPass(&list, render.PassDescriptor{Label: "none"})
		recordPass(&list, render.PassDescriptor{Label: "too many", ColorTargets: slices.Repeat([]render.ColorTarget{{Window: large}}, 9)})
		recordPass(&list, render.PassDescriptor{Label: "nameless", ColorTargets: []render.ColorTarget{{Window: large}, {}}})
		recordPass(&list, render.PassDescriptor{Label: "disagreeing", ColorTargets: []render.ColorTarget{{Window: large}, {Window: small}}})
		recordPass(&list, render.PassDescriptor{Label: "kept", ColorTargets: []render.ColorTarget{{Window: small}}})
		list.End()
	}}})
	large = h.OpenWindow(platform.Size{Width: 64, Height: 48})
	small = h.OpenWindow(platform.Size{Width: 32, Height: 32})

	got := stepAndReadFrame(t, h)

	if kinds, want := makeReportKinds(h), slices.Repeat([]render.ReportKind{render.ReportKindPassTargetsRefused}, 4); !slices.Equal(kinds, want) {
		t.Errorf("render reported kinds %v, want %v", kinds, want)
	}
	want := render.Inspection{CommandLists: []render.InspectedCommandList{
		{Passes: []render.InspectedPass{{Label: "kept", ColorTargets: []render.ColorTarget{{Window: small}}}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the frame reads back as %+v, want only the pass kept, %+v", got, want)
	}
}
