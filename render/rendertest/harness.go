package rendertest

import (
	"errors"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/platform/headless"
	"github.com/dvoyni/cog/render"
)

// FrameInterval is the clock time Step advances by, to one beat of the render
// track: what each beat's Delta stands for.
const FrameInterval = time.Second / 60

// workerCount is how many workers the harness's kernel runs.
const workerCount = 2

// timeout is how long the harness waits for the kernel before failing the
// test.
const timeout = 5 * time.Second

// Harness runs render in a real kernel on a fake GPU, for render's tests and
// those of the plugins that draw with it. Spawn spawns, through a
// kernel.Supervisor, platform on headless windows, metronome with a render
// track on a metronome.ManualClock, gfx on its recording adapter, render, and
// the test's own plugins after render; Step draws one beat. The fake GPU is a
// frame behind: it finishes the work submitted in one beat when the next
// Step begins, so a frame's GPULatency reads FrameInterval and a readback
// lands a beat after its submit.
//
// It is strict: the test fails on every error the kernel logged, checked after
// each Step and when the test ends, unless it is a render.Report of a kind the
// test named with ExpectReport.
type Harness struct {
	t          testing.TB
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	clock      *metronome.ManualClock
	instance   *recording.Instance
	windows    *headless.Windows
	// expectedReports are the kinds ExpectReport named; only the test's
	// goroutine reads and writes them.
	expectedReports []render.ReportKind
	// opened is the window OpenWindow's open job opened, and surfaceMade the
	// job making its surface, each set by the jobs OpenWindow waits for.
	opened      platform.Window
	surfaceMade kernel.JobHandle
	// submittedIndex is the newest submission the GPU was given, and
	// finishedIndex the newest it has finished: what was submitted when the
	// last Step began.
	submittedIndex atomic.Uint64
	finishedIndex  atomic.Uint64
	// mutex guards logged, the errors logged and not yet checked, and
	// reports, every render.Report logged.
	mutex   sync.Mutex
	logged  []error
	reports []render.Report
}

// NewHarness answers a harness whose kernel is not yet spawned. When the test
// ends, it quits the supervisor, waits for every kernel to be destroyed, and
// checks the errors logged.
func NewHarness(t testing.TB) *Harness {
	t.Helper()
	h := &Harness{
		t:        t,
		clock:    metronome.NewManualClock(),
		instance: recording.NewInstance(),
		windows:  headless.NewWindows(),
	}
	h.supervisor = kernel.NewSupervisor(logHandler{harness: h})
	t.Cleanup(h.checkLogged)
	t.Cleanup(h.quit)
	return h
}

// Spawn spawns platform, metronome with a variable render track, gfx, render
// with config, the harness's own plugin, and plugins, in that order, so the
// test's plugins are listed after render. It fails the test when the spawn
// fails.
func (h *Harness) Spawn(config *render.Config, plugins ...kernel.Plugin) {
	h.t.Helper()
	configs := map[kernel.PluginName]any{
		render.PluginName: config,
		metronome.PluginName: &metronome.Config{
			Clock:  h.clock,
			Tracks: []metronome.Track{metronome.NewTrack[gfx.RenderEvent](metronome.Variable())},
		},
	}
	renderStack := []kernel.Plugin{platform.NewPlugin(), metronome.NewPlugin(), gfx.NewPlugin(), render.NewPlugin(), harnessPlugin{harness: h}}
	id, err := h.supervisor.Spawn(configs, workerCount, append(renderStack, plugins...)...)
	if err != nil {
		h.t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
}

// Step draws one beat: it has the GPU finish the work submitted so far,
// advances the clock by FrameInterval, and returns once the beat has
// finished, its frame presented and every job of its publication done. It
// then checks the errors logged.
func (h *Harness) Step() {
	h.t.Helper()
	h.finishedIndex.Store(h.submittedIndex.Load())
	advanced := make(chan struct{})
	go func() {
		defer close(advanced)
		h.clock.Advance(FrameInterval)
	}()
	h.tickUntil("the beat", makeReceiveCheck(advanced))
	h.checkLogged()
}

// OpenWindow opens a headless window of size and pumps, and returns once gfx
// has made its surface, so the next Step acquires its framebuffer.
func (h *Harness) OpenWindow(size platform.Size) platform.Window {
	h.t.Helper()
	_, out := h.kernel.Publish(openWindowEvent{size: size})
	h.wait("the window's open", out)
	h.wait("the window's surface", h.surfaceMade)
	return h.opened
}

// LengthenNextAcquire makes the next acquire of a window's framebuffer take d
// longer by the clock, as a GPU behind would, so the frame after it carries d
// in its AcquireWait. Calls queue, one for each acquire to come.
func (h *Harness) LengthenNextAcquire(d time.Duration) {
	h.clock.LengthenNext(gfx.AcquireStopwatchName, d)
}

// ExpectReport names a kind of report the test expects: a render.Report of it
// that the kernel logs fails no test. A report of a kind no call named, and
// any other error, fails it.
func (h *Harness) ExpectReport(kind render.ReportKind) {
	h.expectedReports = append(h.expectedReports, kind)
}

// Frames answers each frame gfx has drawn so far, read back from its
// recording adapter in render's terms. Its command lists are every encoder
// gfx submitted, by order, those of plugins drawing on gfx alone included.
func (h *Harness) Frames() []render.Inspection {
	recorded := h.instance.Recording()
	viewTargets := makeViewTargets(recorded.Calls)
	var inspections []render.Inspection
	for _, frame := range recorded.Frames() {
		inspections = append(inspections, makeInspection(frame, viewTargets))
	}
	return inspections
}

// Reports answers every render.Report the kernel has logged so far, expected
// or not, in the order logged.
func (h *Harness) Reports() []render.Report {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.reports)
}

// checkLogged fails the test on each error logged since the last check, but a
// report of an expected kind.
func (h *Harness) checkLogged() {
	h.t.Helper()
	h.mutex.Lock()
	logged := h.logged
	h.logged = nil
	h.mutex.Unlock()
	for _, err := range logged {
		var report render.Report
		switch {
		case !errors.As(err, &report):
			h.t.Errorf("the kernel logged %v", err)
		case !slices.Contains(h.expectedReports, report.Kind):
			h.t.Errorf("render reported %v, of kind %d, which the test did not expect", err, report.Kind)
		}
	}
}

// quit quits the supervisor and ticks the main thread, for the pinned jobs
// still to run, until every kernel is destroyed.
func (h *Harness) quit() {
	h.t.Helper()
	h.supervisor.Quit()
	h.tickUntil("the supervisor's quit", makeReceiveCheck(h.supervisor.Done()))
}

// wait ticks the main thread until out, the job of what, has finished.
func (h *Harness) wait(what string, out kernel.JobHandle) {
	h.t.Helper()
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, out)
	h.tickUntil(what, makeReceiveCheck(signal.C()))
}

// tickUntil ticks the main thread, for the pinned jobs to run, until done
// answers true, and fails the test naming what when that takes longer than
// timeout.
func (h *Harness) tickUntil(what string, done func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		h.supervisor.TickMainThread()
		if done() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%s did not finish in %v", what, timeout)
		}
		runtime.Gosched()
	}
}

// makeReceiveCheck answers a check of whether a receive from channel would
// not block: it is closed, or a value was sent on it, which the check takes.
func makeReceiveCheck(channel <-chan struct{}) func() bool {
	return func() bool {
		select {
		case <-channel:
			return true
		default:
			return false
		}
	}
}
