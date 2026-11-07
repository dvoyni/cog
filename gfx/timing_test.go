package gfx_test

import (
	"math"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

// heldCompletionDevice is a recording device whose Poll answers no
// submission after completedIndex as complete.
type heldCompletionDevice struct {
	gfx.BackendDevice
	completedIndex *atomic.Uint64
}

func (d *heldCompletionDevice) Poll() uint64 {
	return min(d.BackendDevice.Poll(), d.completedIndex.Load())
}

// clearEachFrame records a clear of the buffer bufferOf answers into each
// frame drawn, so each frame submits, for the frames whose tick drawsTick
// answers true.
func clearEachFrame(h *harness, bufferOf func() gfx.Buffer, drawsTick func(tick uint64) bool) {
	h.readFrame = func(event gfx.DrawEvent, device gfx.Device) {
		if !drawsTick(event.Tick) {
			return
		}
		encoder, err := device.Encoder(event.Frame, 0)
		if err != nil {
			h.t.Errorf("Encoder answered %v", err)
			return
		}
		encoder.ClearBuffer(bufferOf(), 0, 4)
	}
}

func TestAcquireWaitIsHowLongThePreviousFramesAcquireBlockedByMetronomesClock(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.openWindow()
	h.waitForCalls(recording.CallKindCreateSurface, 1)
	for _, wait := range []time.Duration{3 * time.Millisecond, 5 * time.Millisecond, 7 * time.Millisecond} {
		h.clock.LengthenNext(gfx.AcquireStopwatchName, wait)
	}

	for range 3 {
		h.advance(16 * time.Millisecond)
	}

	var waits []time.Duration
	for _, event := range h.drawEvents() {
		waits = append(waits, event.AcquireWait)
	}
	if want := []time.Duration{0, 3 * time.Millisecond, 5 * time.Millisecond}; !slices.Equal(waits, want) {
		t.Errorf("DrawEvent AcquireWaits = %v, want %v", waits, want)
	}
}

func TestGPULatencyIsFromTheNewestFinishedFramesSubmitToThePollThatSawItComplete(t *testing.T) {
	h := newHarness(t)
	var completedIndex atomic.Uint64
	h.wrapDevice = func(device gfx.BackendDevice) gfx.BackendDevice {
		return &heldCompletionDevice{BackendDevice: device, completedIndex: &completedIndex}
	}
	var buffer gfx.Buffer
	clearEachFrame(h, func() gfx.Buffer { return buffer }, func(uint64) bool { return true })
	h.mustSpawn(nil)
	buffer = h.createBuffers(1)[0]

	// Each frame submits once, its submission index its tick; before each
	// beat, the submissions up to completed finish, and the clock advances
	// by interval to the beat.
	for _, step := range []struct {
		completed uint64
		interval  time.Duration
	}{
		{0, 16 * time.Millisecond}, // frame 1 submits at 16ms
		{1, 4 * time.Millisecond},  // frame 2 submits at 20ms, its poll sees frame 1 finished: 4ms
		{1, 6 * time.Millisecond},  // frame 3 submits at 26ms, nothing new finished
		{3, 3 * time.Millisecond},  // frame 4 submits at 29ms, frames 2 and 3 finished: 3ms, frame 3's
		{3, 16 * time.Millisecond}, // frame 5 shows it
	} {
		completedIndex.Store(step.completed)
		h.advance(step.interval)
	}

	var latencies []time.Duration
	for _, event := range h.drawEvents() {
		latencies = append(latencies, event.GPULatency)
	}
	if want := []time.Duration{0, 0, 4 * time.Millisecond, 4 * time.Millisecond, 3 * time.Millisecond}; !slices.Equal(latencies, want) {
		t.Errorf("DrawEvent GPULatencies = %v, want %v", latencies, want)
	}
}

func TestGPULatencyIsZeroWithTheRecordingAdapter(t *testing.T) {
	h := newHarness(t)
	var buffer gfx.Buffer
	clearEachFrame(h, func() gfx.Buffer { return buffer }, func(uint64) bool { return true })
	h.mustSpawn(nil)
	buffer = h.createBuffers(1)[0]

	for range 4 {
		h.advance(16 * time.Millisecond)
	}

	if submissions := len(h.instance.Recording().Submissions); submissions != 4 {
		t.Fatalf("%d submissions, want one a frame", submissions)
	}
	for _, event := range h.drawEvents() {
		if event.GPULatency != 0 {
			t.Errorf("DrawEvent at tick %d has GPULatency %v, want zero", event.Tick, event.GPULatency)
		}
	}
}

func TestAFrameSubmittedBeforeALossIsNotCompletedByTheRecoveredDevicesSubmissions(t *testing.T) {
	h := newHarness(t)
	var deviceCount atomic.Int32
	h.wrapDevice = func(device gfx.BackendDevice) gfx.BackendDevice {
		completedIndex := &atomic.Uint64{}
		if deviceCount.Add(1) > 1 {
			completedIndex.Store(math.MaxUint64)
		}
		return &heldCompletionDevice{BackendDevice: device, completedIndex: completedIndex}
	}
	var buffer gfx.Buffer
	clearEachFrame(h, func() gfx.Buffer { return buffer }, func(tick uint64) bool { return tick == 1 })
	h.mustSpawn(nil)
	buffer = h.createBuffers(1)[0]
	h.advance(16 * time.Millisecond)
	loseDevice(h)
	h.advance(16 * time.Millisecond)
	if recovered := h.recoveredEvents(); recovered != 1 {
		t.Fatalf("DeviceRecoveredEvent published %d times, want once", recovered)
	}

	h.advance(50 * time.Millisecond)
	h.readDevice(func(device gfx.Device) {
		encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
		if err != nil {
			t.Errorf("CreateCommandEncoder answered %v", err)
			return
		}
		encoder.ClearBuffer(buffer, 0, 4)
		commandBuffer, err := encoder.Finish()
		if err != nil {
			t.Errorf("Finish answered %v", err)
			return
		}
		if err := device.Submit(commandBuffer); err != nil {
			t.Errorf("Submit answered %v", err)
		}
	})
	h.advance(16 * time.Millisecond)

	drawn := h.drawEvents()
	if latency := drawn[len(drawn)-1].GPULatency; latency != 0 {
		t.Errorf("the DrawEvent after a standalone submit finished on the recovered device has GPULatency %v, want zero: the lost device's frame is not finished by it", latency)
	}
}
