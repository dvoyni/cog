package internal

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// wgpu is app's MainLoop: each gogpu update hands the Loop app attached the real
// time of the frames drawn since the last one, after flushing the frame's input,
// so every tick the Loop publishes sees that input. The window itself is not
// needed to show it: onUpdate runs against a bare plugin and a real engine.

// recordingLoop is an app.Loop that records what the MainLoop hands it, and what
// the input seam held at that moment.
type recordingLoop struct {
	harness *mainLoopHarness
	frames  []float64
	// inputAtFrame is how many input changes had been applied when each frame
	// was handed over.
	inputAtFrame []int
}

func (l *recordingLoop) Init(kernel.Executioner) error { return nil }
func (l *recordingLoop) Render(kernel.Executioner)     {}
func (l *recordingLoop) Quit(kernel.Executioner)       {}

func (l *recordingLoop) WindowSize(kernel.Executioner, float32, float32) {}

func (l *recordingLoop) Frame(_ kernel.Executioner, dt float64) {
	l.frames = append(l.frames, dt)
	l.inputAtFrame = append(l.inputAtFrame, len(l.harness.appliedChanges()))
}

// mainLoopHarness answers input.ApplyCmd, the seam a frame's input flush reaches.
type mainLoopHarness struct {
	mu      sync.Mutex
	applied []input.Change
}

func (*mainLoopHarness) Name() kernel.PluginName           { return "wgpumainlooptest" }
func (*mainLoopHarness) Dependencies() []kernel.PluginName { return nil }

func (h *mainLoopHarness) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[input.ApplyCmd](func() (kernel.Lock, kernel.Execute[input.ApplyRequest, input.ApplyResponse]) {
		return nil, func(_ kernel.Kernel, request input.ApplyRequest) (input.ApplyResponse, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.applied = append(h.applied, request.Changes...)
			return input.ApplyResponse{}, nil
		}
	})
	return nil
}

func (h *mainLoopHarness) appliedChanges() []input.Change {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]input.Change(nil), h.applied...)
}

func TestMainLoop_UpdateFlushesInputThenHandsTheLoopTheFrameTime(t *testing.T) {
	harness := &mainLoopHarness{}
	ctx, cancel := context.WithCancel(context.Background())
	engine := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(harness)
	stopped := make(chan struct{})
	go func() { defer close(stopped); engine.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("engine did not shut down")
		}
	})
	<-engine.Ready()
	k := engine.Executioner()

	p := &plugin{}
	loop := &recordingLoop{harness: harness}
	app.MainLoop(mainLoop{p}).Attach(loop)

	// Two frames drawn at 10ms apiece since the last update, and a key press
	// waiting to be flushed.
	p.frameDtBits.Store(math.Float64bits(0.010))
	p.frameSeq.Add(2)
	p.pending = []input.Change{input.KeyChange(input.KeyA, 0, true)}
	p.onUpdate(k, 0)
	// No frame drawn since: the update hands over no time.
	p.onUpdate(k, 0)

	if len(loop.frames) != 2 || !almostEqual(loop.frames[0], 0.020) || loop.frames[1] != 0 {
		t.Errorf("the Loop was handed %v, want [0.02 0]", loop.frames)
	}
	if len(loop.inputAtFrame) == 0 || loop.inputAtFrame[0] != 1 {
		t.Errorf("input applied when the first frame was handed over = %v, want the key already flushed", loop.inputAtFrame)
	}
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
