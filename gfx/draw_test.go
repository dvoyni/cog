package gfx_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

func TestDrawPublishesDrawEventAfterTheAcquire(t *testing.T) {
	h := newHarness(t)
	var mutex sync.Mutex
	var framebuffers []gfx.TextureView
	var window platform.Window
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		mutex.Lock()
		defer mutex.Unlock()
		framebuffers = append(framebuffers, device.Framebuffer(event.Frame, window))
	}
	h.mustSpawn(nil)
	window = h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(16 * time.Millisecond)

	drawn := h.drawEvents()
	if len(drawn) != 1 {
		t.Fatalf("%d DrawEvents, want 1", len(drawn))
	}
	event := drawn[0]
	if event.Tick != 1 || event.TimeFromStart != 16*time.Millisecond {
		t.Errorf("DrawEvent beat = %+v, want tick 1 at 16ms", event.Beat)
	}
	if event.Frame == (gfx.Frame{}) || event.Ready == (kernel.JobHandle{}) {
		t.Errorf("DrawEvent Frame %v, Ready %v, want neither zero", event.Frame, event.Ready)
	}
	calls := frameCalls(h.instance.Recording().Calls)
	if got, want := kinds(calls), []recording.CallKind{recording.CallKindAcquireFramebuffer, recording.CallKindPresent}; !slices.Equal(got, want) {
		t.Fatalf("frame calls = %v once the beat finished, want %v", got, want)
	}
	acquired := calls[0].Arguments.(recording.AcquireFramebufferArguments)
	if acquired.Window != window || acquired.View == (gfx.TextureView{}) {
		t.Errorf("acquired %+v, want a view of %v", acquired, window)
	}
	if !slices.Equal(framebuffers, []gfx.TextureView{acquired.View}) {
		t.Errorf("Framebuffer answered %v after Ready, want [%v]", framebuffers, acquired.View)
	}
	if calls[1].Arguments != window || calls[1].Error != nil {
		t.Errorf("presented %v, answering %v, want %v", calls[1].Arguments, calls[1].Error, window)
	}
}

func TestTheBeatLastsUntilThePresent(t *testing.T) {
	h := newHarness(t)
	h.pinTasks = true
	release := make(chan struct{})
	h.holdFrame = func(gfx.DrawEvent) { <-release }
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.clock.Jump(time.Millisecond)
	h.waitForCalls(recording.CallKindAcquireFramebuffer, 1)

	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := h.clock.Wait(ctx)

	if err == nil {
		t.Error("the beat settled with its present not run, want it in flight until the present")
	}
	h.settle()
}

func TestEachAcquireFollowsThePreviousPresent(t *testing.T) {
	h := newHarness(t)
	h.render.MaxInFlight = 3
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	for count := 1; count <= 3; count++ {
		h.clock.Jump(time.Millisecond)
		h.waitForDraws(count)
	}
	h.settle()

	calls := frameCalls(h.instance.Recording().Calls)
	want := []recording.CallKind{
		recording.CallKindAcquireFramebuffer, recording.CallKindPresent,
		recording.CallKindAcquireFramebuffer, recording.CallKindPresent,
		recording.CallKindAcquireFramebuffer, recording.CallKindPresent,
	}
	if got := kinds(calls); !slices.Equal(got, want) {
		t.Fatalf("frame calls = %v, want %v", got, want)
	}
	for _, call := range calls {
		if call.Error != nil {
			t.Errorf("%v answered %v", call.Kind, call.Error)
		}
	}
}

func TestAPresentedFrameAnswersTheZeroViewAndItsSlotIsReused(t *testing.T) {
	h := newHarness(t)
	var mutex sync.Mutex
	var window platform.Window
	var first gfx.Frame
	var current, old gfx.TextureView
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		mutex.Lock()
		defer mutex.Unlock()
		if event.Tick == 1 {
			first = event.Frame
			return
		}
		current = device.Framebuffer(event.Frame, window)
		old = device.Framebuffer(first, window)
	}
	h.mustSpawn(nil)
	window = h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)

	h.advance(time.Millisecond)
	h.advance(time.Millisecond)

	drawn := h.drawEvents()
	if len(drawn) != 2 || drawn[0].Frame == drawn[1].Frame {
		t.Fatalf("frames %+v, want two of different generations", drawn)
	}
	if current == (gfx.TextureView{}) || old != (gfx.TextureView{}) {
		t.Errorf("Framebuffer answered %v for the frame drawn and %v for the frame presented, want a view and the zero view", current, old)
	}
}

func TestAWindowWithNoFramebufferAnswersTheZeroView(t *testing.T) {
	h := newHarness(t)
	var mutex sync.Mutex
	var outdated, drawable, unopened gfx.TextureView
	var first, second platform.Window
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		mutex.Lock()
		defer mutex.Unlock()
		outdated = device.Framebuffer(event.Frame, first)
		drawable = device.Framebuffer(event.Frame, second)
		unopened = device.Framebuffer(event.Frame, platform.Window{})
	}
	h.instance.AnswerNext(recording.CallKindAcquireFramebuffer, recording.Answer{Status: uint8(gfx.AcquireStatusOutdated)})
	h.mustSpawn(nil)
	first = h.openWindow()
	second = h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 2)

	h.advance(time.Millisecond)

	if outdated != (gfx.TextureView{}) || drawable == (gfx.TextureView{}) || unopened != (gfx.TextureView{}) {
		t.Errorf("Framebuffer answered %v outdated, %v drawable, %v unopened, want only the drawable window's view", outdated, drawable, unopened)
	}
	var presented []any
	for _, call := range frameCalls(h.instance.Recording().Calls) {
		if call.Kind == recording.CallKindPresent {
			presented = append(presented, call.Arguments)
		}
	}
	if !slices.Equal(presented, []any{second}) {
		t.Errorf("presented %v, want only %v", presented, second)
	}
}

func TestDrawEventIsPublishedWithNoWindow(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.advance(time.Millisecond)

	if drawn := h.drawEvents(); len(drawn) != 1 {
		t.Fatalf("%d DrawEvents with no window, want 1", len(drawn))
	}
	if calls := frameCalls(h.instance.Recording().Calls); len(calls) != 0 {
		t.Errorf("frame calls = %v with no window, want none", kinds(calls))
	}
}

func TestAClosedWindowsSurfaceIsReleasedAfterTheLastPresent(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.holdFrame = func(gfx.DrawEvent) { <-release }
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.clock.Jump(time.Millisecond)
	h.waitForCalls(recording.CallKindAcquireFramebuffer, 1)

	h.closeWindow(window)
	close(release)
	h.settle()
	h.waitForCalls(recording.CallKindReleaseSurface, 1)

	calls := frameCalls(h.instance.Recording().Calls)
	want := []recording.CallKind{recording.CallKindAcquireFramebuffer, recording.CallKindPresent, recording.CallKindReleaseSurface}
	if got := kinds(calls); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for _, call := range calls {
		if call.Error != nil {
			t.Errorf("%v answered %v", call.Kind, call.Error)
		}
	}
}

func TestAWindowClosedDuringAFrameIsNotAcquiredByTheNext(t *testing.T) {
	h := newHarness(t)
	h.render.MaxInFlight = 2
	release := make(chan struct{})
	h.holdFrame = func(event gfx.DrawEvent) {
		if event.Tick == 1 {
			<-release
		}
	}
	h.mustSpawn(nil)
	window := h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	h.clock.Jump(time.Millisecond)
	h.waitForCalls(recording.CallKindAcquireFramebuffer, 1)

	h.closeWindow(window)
	h.clock.Jump(time.Millisecond)
	h.waitForDraws(2)
	close(release)
	h.settle()
	h.waitForCalls(recording.CallKindReleaseSurface, 1)

	calls := frameCalls(h.instance.Recording().Calls)
	want := []recording.CallKind{recording.CallKindAcquireFramebuffer, recording.CallKindPresent, recording.CallKindReleaseSurface}
	if got := kinds(calls); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: the second frame acquires nothing of the window closing", got, want)
	}
	for _, call := range calls {
		if call.Error != nil {
			t.Errorf("%v answered %v", call.Kind, call.Error)
		}
	}
}

// frameCalls answers the acquires, presents and surface releases among calls,
// in order.
func frameCalls(calls []recording.Call) []recording.Call {
	var answer []recording.Call
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindAcquireFramebuffer, recording.CallKindPresent, recording.CallKindReleaseSurface:
			answer = append(answer, call)
		}
	}
	return answer
}
