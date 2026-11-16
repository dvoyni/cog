package rendertest

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/gogpu"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/headless"
	"github.com/dvoyni/cog/plugins/render"
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
	// tickMainThread is the supervisor's TickMainThread, made a func once,
	// so a tick allocates nothing.
	tickMainThread func()
	// advanceRequests is what Step sends on for advanceClock to advance the
	// clock by FrameInterval, and checkAdvanced takes what advanceClock
	// sends once it has, so a Step allocates nothing.
	advanceRequests chan struct{}
	checkAdvanced   func() bool
	// recordingDisabled is set when gfx's recording adapter keeps no call,
	// so Step reads no frames back.
	recordingDisabled bool
	// declarations is the catalog of the binding declarations answering the
	// reflection of the shaders compiled on instance.
	declarations *declarationCatalog
	// timestampsEnabled is set by EnableTimestamps, for Spawn to have gfx
	// require them.
	timestampsEnabled bool
	// softwareAdapter is set by UseSoftwareAdapter, for Spawn to list
	// gogpu's plugin on wgpu's software adapter in place of the recording
	// instance and the headless windows.
	softwareAdapter bool
	// mainThread is RunMain's, which UseSoftwareAdapter takes for the
	// harness to run the kernel's main thread on the process's; nil without
	// RunMain, or on the recording adapter, where the test's goroutine runs
	// it.
	mainThread chan func()
	// expectedReports are the kinds ExpectReport named; only the test's
	// goroutine reads and writes them.
	expectedReports []render.ReportKind
	// expectedErrors are the errors whose logging, wrapped or not, fails no
	// test, for rendertest's own tests; only the test's goroutine reads and
	// writes them.
	expectedErrors []error
	// opened is the window OpenWindow's open job opened, and surfaceMade the
	// job making its surface, each set by the jobs OpenWindow waits for.
	opened      platform.Window
	surfaceMade kernel.JobHandle
	// submittedIndex is the newest submission the GPU was given, and
	// finishedIndex the newest it has finished: what was submitted when the
	// last Step began.
	submittedIndex atomic.Uint64
	finishedIndex  atomic.Uint64
	// frameNotes are what each Step saw of the frames it drew, by the index
	// of each frame recorded; only the test's goroutine reads and writes
	// them.
	frameNotes []frameNote
	// mutex guards logged, the errors logged and not yet checked; reports,
	// every render.Report logged; and beat, the beat of the last FrameEvent
	// published.
	mutex   sync.Mutex
	logged  []error
	reports []render.Report
	beat    metronome.Beat
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
	h.declarations = newDeclarationCatalog(h)
	h.supervisor = kernel.NewSupervisor(logHandler{harness: h})
	h.tickMainThread = h.supervisor.TickMainThread
	h.advanceRequests = make(chan struct{})
	advances := make(chan struct{}, 1)
	h.checkAdvanced = makeReceiveCheck(advances)
	go h.advanceClock(advances)
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
	gfxConfig := &gfx.Config{ForceFallbackAdapter: h.softwareAdapter}
	if h.timestampsEnabled {
		gfxConfig.RequiredFeatures = gfx.FeatureFlagTimestampQuery
	}
	configs[gfx.PluginName] = gfxConfig
	renderStack := []kernel.Plugin{platform.NewPlugin(), metronome.NewPlugin(), gfx.NewPlugin()}
	if h.softwareAdapter {
		if h.mainThread == nil {
			// gogpu's HostLoop runs every call on the thread that started
			// it: gfx's Start, in Spawn, and the pinned jobs, which the
			// harness ticks, each on this goroutine without RunMain.
			runtime.LockOSThread()
		}
		renderStack = append(renderStack, gogpu.NewPlugin())
	}
	renderStack = append(renderStack, render.NewPlugin(), harnessPlugin{harness: h})
	var id kernel.KernelID
	var err error
	h.runOnMainThread(func() {
		id, err = h.supervisor.Spawn(configs, workerCount, append(renderStack, plugins...)...)
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
}

// Step draws one beat: it has the GPU finish the work submitted so far,
// advances the clock by FrameInterval, and returns once the beat has
// finished, its frame presented and every job of its publication done. It
// notes the beat and the reports of the frame it drew, for Frames, and then
// checks the errors logged.
func (h *Harness) Step() {
	h.t.Helper()
	h.finishedIndex.Store(h.submittedIndex.Load())
	h.mutex.Lock()
	firstReport := len(h.reports)
	h.mutex.Unlock()
	h.advanceRequests <- struct{}{}
	h.tickUntil("the beat", h.checkAdvanced)
	h.mutex.Lock()
	note := frameNote{beat: h.beat, reports: slices.Clone(h.reports[firstReport:])}
	h.mutex.Unlock()
	if !h.recordingDisabled {
		for range len(h.instance.Recording().Frames()) - len(h.frameNotes) {
			h.frameNotes = append(h.frameNotes, note)
		}
	}
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

// EnableTimestamps gives the fake GPU timestamp queries, which no real
// backend has until the wgpu fork exposes them, so a test turns render's
// pass timings on: gfx requires FeatureFlagTimestampQuery, and gfx's
// recording adapter writes each pass's timestamps a nanosecond apart for
// each command the pass records, its beginning among them. It is called
// before Spawn, and fails the test after, or on the software adapter.
func (h *Harness) EnableTimestamps() {
	h.t.Helper()
	switch {
	case h.kernel != nil:
		h.t.Fatal("rendertest: enabling timestamps after Spawn")
	case h.softwareAdapter:
		h.t.Fatal("rendertest: enabling the fake GPU's timestamps on the software adapter")
	}
	h.instance.EnableTimestamps()
	h.timestampsEnabled = true
}

// UseSoftwareAdapter has Spawn run render on wgpu's software adapter, real
// rendering on the CPU, in place of gfx's recording adapter, for pixel
// tests: it lists gogpu's plugin after gfx, with gfx's
// ForceFallbackAdapter, and binds neither the recording instance nor the
// headless windows, which gogpu's instance and window system replace. A
// test draws into textures and reads them back through render's readback;
// nothing is recorded, so Frames answers no frame.
//
// The harness still wraps gfx's instance: a shader compiled answers what
// the adapter reflects of it, and a binding declaration that differs from
// that fails the test, naming the shader and the first binding that
// differs, so a stale declaration is caught. A shader compiled with no
// declaration fails the test as on the recording adapter.
//
// gogpu's HostLoop runs every call on one thread, the kernel's main thread.
// When the test binary's TestMain calls RunMain, that is the process's main
// thread, which runs the harness's Spawn, each tick and its quit. Without
// it, it is the test's goroutine, which Spawn locks to its OS thread until
// the harness quits, which serves on Windows and Linux.
//
// It is called before Spawn, and fails the test after, or with timestamps
// enabled. It skips the test under -short; on macOS without RunMain, whose
// HostLoop must run on the process's main thread; and on Linux with no
// display.
func (h *Harness) UseSoftwareAdapter() {
	h.t.Helper()
	switch {
	case testing.Short():
		h.t.Skip("rendertest: a test on the software adapter is skipped under -short")
	case runtime.GOOS == "darwin" && mainThreadWork == nil:
		h.t.Skip("rendertest: gogpu's HostLoop must run on the process's main thread on macOS: call rendertest.RunMain from TestMain")
	case runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "":
		h.t.Skip("rendertest: no display for gogpu")
	case h.kernel != nil:
		h.t.Fatal("rendertest: choosing the software adapter after Spawn")
	case h.timestampsEnabled:
		h.t.Fatal("rendertest: choosing the software adapter with the fake GPU's timestamps enabled")
	}
	h.softwareAdapter = true
	h.mainThread = mainThreadWork
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
// recording adapter in render's terms, as render.Inspection says, with the
// beat of the Step that drew it and the reports logged during that Step.
// Its command lists are every encoder gfx submitted, by order, those of
// plugins drawing on gfx alone included. What render made and wrote between
// two frames goes into the earlier, as recording.Recording.FrameCalls groups
// calls. On the software adapter nothing is recorded, so it answers no
// frame: a pixel test reads its texels back through render's readback.
func (h *Harness) Frames() []render.Inspection {
	recorded := h.instance.Recording()
	reader := newFrameReader(recorded.Calls, h.declarations.findAnsweredBindings())
	frameCalls := recorded.FrameCalls()
	var inspections []render.Inspection
	for index, frame := range recorded.Frames() {
		inspection := reader.makeInspection(frame, frameCalls[index])
		if index < len(h.frameNotes) {
			inspection.Beat = h.frameNotes[index].beat
			inspection.Reports = h.frameNotes[index].reports
		}
		inspections = append(inspections, inspection)
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

// noteBeat keeps beat as the beat of the last FrameEvent published.
func (h *Harness) noteBeat(beat metronome.Beat) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.beat = beat
}

// checkLogged fails the test on each error logged since the last check, but a
// report of an expected kind, and an expected error.
func (h *Harness) checkLogged() {
	h.t.Helper()
	h.mutex.Lock()
	logged := h.logged
	h.logged = nil
	h.mutex.Unlock()
	for _, err := range logged {
		var report render.Report
		switch {
		case slices.ContainsFunc(h.expectedErrors, func(expected error) bool { return errors.Is(err, expected) }):
		case !errors.As(err, &report):
			h.t.Errorf("the kernel logged %v", err)
		case !slices.Contains(h.expectedReports, report.Kind):
			h.t.Errorf("render reported %v, of kind %d, which the test did not expect", err, report.Kind)
		}
	}
}

// quit quits the supervisor and ticks the main thread, for the pinned jobs
// still to run, until every kernel is destroyed, and then unlocks the
// goroutine from the OS thread Spawn locked it to.
func (h *Harness) quit() {
	h.t.Helper()
	h.supervisor.Quit()
	h.tickUntil("the supervisor's quit", makeReceiveCheck(h.supervisor.Done()))
	close(h.advanceRequests)
	if h.softwareAdapter && h.mainThread == nil {
		runtime.UnlockOSThread()
	}
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
		h.runOnMainThread(h.tickMainThread)
		if done() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%s did not finish in %v", what, timeout)
		}
		runtime.Gosched()
	}
}

// advanceClock advances the clock by FrameInterval for each request Step
// sends, sending on advances once Advance returns, until the harness quits.
func (h *Harness) advanceClock(advances chan<- struct{}) {
	for range h.advanceRequests {
		h.clock.Advance(FrameInterval)
		advances <- struct{}{}
	}
}

// runOnMainThread runs run on the kernel's main thread and waits for it: on
// the process's main thread, through RunMain, when the harness took it, a
// panic of run panicking again on the caller's goroutine, and on the
// caller's otherwise.
func (h *Harness) runOnMainThread(run func()) {
	if h.mainThread == nil {
		run()
		return
	}
	var recovered any
	finished := make(chan struct{})
	h.mainThread <- func() {
		defer close(finished)
		defer func() { recovered = recover() }()
		run()
	}
	<-finished
	if recovered != nil {
		panic(recovered)
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

// frameNote is what a Step saw of a frame it drew: the beat of its
// FrameEvent, and the reports logged while it drew.
type frameNote struct {
	beat    metronome.Beat
	reports []render.Report
}
