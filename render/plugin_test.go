package render_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestAPluginOnFrameEventGetsOneEachBeatCarryingGfxsReadyAndTimingSignals(t *testing.T) {
	h := rendertest.NewHarness(t)
	watcher := &frameWatcher{}
	h.Spawn(nil, watcher)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.LengthenNextAcquire(3 * time.Millisecond)
	h.LengthenNextAcquire(5 * time.Millisecond)

	for range 3 {
		h.Step()
	}

	frameEvents, drawEvents := watcher.events()
	if len(frameEvents) != 3 || len(drawEvents) != 3 {
		t.Fatalf("3 beats published %d FrameEvents and %d gfx.DrawEvents, want 3 of each", len(frameEvents), len(drawEvents))
	}
	var waits, latencies []time.Duration
	for index, drawEvent := range drawEvents {
		want := render.FrameEvent{Beat: drawEvent.Beat, Ready: drawEvent.Ready, AcquireWait: drawEvent.AcquireWait, GPULatency: drawEvent.GPULatency}
		if frameEvents[index] != want {
			t.Errorf("beat %d's FrameEvent is %+v, want gfx's %+v", index+1, frameEvents[index], want)
		}
		waits = append(waits, frameEvents[index].AcquireWait)
		latencies = append(latencies, frameEvents[index].GPULatency)
	}
	if want := []time.Duration{0, 3 * time.Millisecond, 5 * time.Millisecond}; !slices.Equal(waits, want) {
		t.Errorf("the FrameEvents' AcquireWaits are %v, want %v", waits, want)
	}
	// Each beat submits its frame, which the harness's GPU finishes when the
	// next beat begins: beat 2 sees beat 1's finished, and beat 3 carries it.
	if want := []time.Duration{0, 0, rendertest.FrameInterval}; !slices.Equal(latencies, want) {
		t.Errorf("the FrameEvents' GPULatencies are %v, want %v", latencies, want)
	}
}

func TestTasksScheduledAfterReadyAndJoinedFinishBeforeGfxsFlush(t *testing.T) {
	h := rendertest.NewHarness(t)
	watcher := &frameWatcher{}
	h.Spawn(nil, watcher)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range 3 {
		h.Step()
	}

	encoderErrors := watcher.encoderAnswers()
	if len(encoderErrors) != 3 {
		t.Fatalf("%d recording tasks ran in 3 beats, want 3", len(encoderErrors))
	}
	for index, err := range encoderErrors {
		if err != nil {
			t.Errorf("beat %d's recording task took its encoder with %v, want it before the flush", index+1, err)
		}
	}
}

func TestZeroConfigCountsMeanTenFramesAndOneMebibyte(t *testing.T) {
	for _, test := range []struct {
		name           string
		config         *render.Config
		wantIdleFrames int
		wantChunkSize  uint64
	}{
		{name: "no config", config: nil, wantIdleFrames: 10, wantChunkSize: 1 << 20},
		{name: "a zero config", config: &render.Config{}, wantIdleFrames: 10, wantChunkSize: 1 << 20},
		{name: "counts set", config: &render.Config{TemporaryIdleFrames: 3, TemporaryChunkSize: 4096}, wantIdleFrames: 3, wantChunkSize: 4096},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			watcher := &frameWatcher{}
			h.Spawn(test.config, watcher)
			if watcher.temporaryIdleFrames != test.wantIdleFrames || watcher.temporaryChunkSize != test.wantChunkSize {
				t.Errorf("render keeps %d idle frames and %d-byte chunks, want %d and %d", watcher.temporaryIdleFrames, watcher.temporaryChunkSize, test.wantIdleFrames, test.wantChunkSize)
			}
		})
	}
}
