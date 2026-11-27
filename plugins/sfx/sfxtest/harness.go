package sfxtest

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/sfx"
)

// StepInterval is the clock time Step advances by, one beat of the harness's
// fixed-step track: what each flush's delta is. A sixty-fourth of a second is
// an exact binary fraction, so a playhead a test adds up flush by flush lands
// where the arithmetic says.
const StepInterval = time.Second / 64

// Timeout is how long the harness waits for the kernel before failing the
// test.
const Timeout = 5 * time.Second

// workerCount is how many workers the harness's kernel runs.
const workerCount = 2

// Harness runs sfx in a real kernel over the headless backend, for sfx's
// tests and those of a game's sound. Spawn spawns, through a
// kernel.Supervisor, platform with a memory mount, metronome with a
// fixed-step track on a metronome.ManualClock, sfx, the harness's own
// plugin, which binds the headless backend or the one UseBackend gave, and
// the test's plugins after it; Step steps one beat.
//
// It is strict: the test fails on every error the kernel logged, checked
// after each Step and when the test ends, unless a match given ExpectError
// answers true of it.
type Harness struct {
	t          testing.TB
	supervisor *kernel.Supervisor
	kernel     *kernel.Kernel
	clock      *metronome.ManualClock
	// backend is what UseBackend gave, which the harness's plugin binds in
	// place of a headless backend; nil without UseBackend.
	backend sfx.BackendSocket
	// errorMatches are the matches ExpectError was given; only the test's
	// goroutine reads and writes them.
	errorMatches []func(error) bool
	// memory is the memory mount Mount puts clip bytes in, bound at the root
	// of platform's FS.
	memory *filesystems.MemoryFS
	// reading is what Read hands the views to, set while it waits for its
	// job.
	reading func(voices sfx.LiveVoices, clips sfx.ClipTable)
	// mutex guards records, the bodies Record queued for the next step,
	// which the harness's recording task takes; endings, every
	// VoiceEndedEvent published; reports, every error logged; and
	// checkedCount, how many of reports the harness has checked.
	mutex        sync.Mutex
	records      []func(queue sfx.Queue)
	endings      []sfx.VoiceEndedEvent
	reports      []error
	checkedCount int
}

// NewHarness answers a harness whose kernel is not yet spawned. When the test
// ends, it quits the supervisor, waits for every kernel to be destroyed, and
// checks the errors logged.
func NewHarness(t testing.TB) *Harness {
	t.Helper()
	h := &Harness{
		t:      t,
		clock:  metronome.NewManualClock(),
		memory: filesystems.NewMemoryFS(),
	}
	h.supervisor = kernel.NewSupervisor(logHandler{harness: h})
	t.Cleanup(h.quit)
	return h
}

// UseBackend has the harness's plugin bind backend to sfx's BackendSocket in
// place of a headless backend: a fake one, say, that answers its prepares
// with done=false or reports a device that is not ready. It is called before
// Spawn, and fails the test after it.
func (h *Harness) UseBackend(backend sfx.BackendSocket) {
	h.t.Helper()
	if h.kernel != nil {
		h.t.Fatal("sfxtest: UseBackend was called after Spawn")
	}
	h.backend = backend
}

// Spawn spawns platform, metronome, sfx with config, the harness's own
// plugin and plugins, in that order, so the test's plugins are listed after
// sfx. It fails the test when the spawn fails.
func (h *Harness) Spawn(config *sfx.Config, plugins ...kernel.Plugin) {
	h.t.Helper()
	configs := map[kernel.PluginName]any{
		sfx.PluginName: config,
		metronome.PluginName: &metronome.Config{
			Clock:  h.clock,
			Tracks: []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(int(time.Second / StepInterval)))},
		},
	}
	stack := []kernel.Plugin{platform.NewPlugin(), metronome.NewPlugin(), sfx.NewPlugin(), harnessPlugin{harness: h}}
	id, err := h.supervisor.Spawn(configs, workerCount, append(stack, plugins...)...)
	if err != nil {
		h.t.Fatal(err)
	}
	h.kernel = h.supervisor.GetKernel(id)
}

// Mount puts data in the memory mount as the file path, which a play then
// names. It fails the test when path is no valid path.
func (h *Harness) Mount(path string, data []byte) {
	h.t.Helper()
	if err := h.memory.Put(path, data); err != nil {
		h.t.Fatal(err)
	}
}

// Record has the harness's recording task call record with sfx's queue in
// the next step, before its flush and after every record queued before it,
// so a test records without writing a plugin.
func (h *Harness) Record(record func(queue sfx.Queue)) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.records = append(h.records, record)
}

// Step steps one beat, StepInterval long: the harness's recording task runs
// the records queued, then sfx's flush applies them, and Step returns once
// the beat has finished, every ending of it published.
func (h *Harness) Step() {
	h.t.Helper()
	h.clock.Advance(StepInterval)
	h.checkLogged()
}

// Endings answers every VoiceEndedEvent published so far, in the order
// published.
func (h *Harness) Endings() []sfx.VoiceEndedEvent {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.endings)
}

// ExpectError names errors the test expects, by match: an error that the
// kernel logs and match answers true of fails no test, such as a clip's
// failure the test causes on purpose. Each call adds a match, an error any
// of them matches failing nothing.
func (h *Harness) ExpectError(match func(error) bool) {
	h.errorMatches = append(h.errorMatches, match)
}

// Reports answers every error the kernel has logged so far, expected or not,
// in the order logged.
func (h *Harness) Reports() []error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return slices.Clone(h.reports)
}

// Read calls read with sfx's views, in a job holding their read locks, and
// returns once it has, so a test reads what the last step's flush left.
func (h *Harness) Read(read func(voices sfx.LiveVoices, clips sfx.ClipTable)) {
	h.t.Helper()
	h.reading = read
	defer func() { h.reading = nil }()
	_, out := h.kernel.Publish(readEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, out)
	select {
	case <-signal.C():
	case <-time.After(Timeout):
		h.t.Fatalf("sfxtest: the read did not finish in %v", Timeout)
	}
}

// takeRecords answers the records queued for this step, and queues none for
// the next.
func (h *Harness) takeRecords() []func(queue sfx.Queue) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	records := h.records
	h.records = nil
	return records
}

// noteEnding keeps ending, for Endings.
func (h *Harness) noteEnding(ending sfx.VoiceEndedEvent) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.endings = append(h.endings, ending)
}

// checkLogged fails the test on each error logged since the last check, but
// one an ExpectError match answers true of.
func (h *Harness) checkLogged() {
	h.t.Helper()
	h.mutex.Lock()
	unchecked := h.reports[h.checkedCount:]
	h.checkedCount = len(h.reports)
	h.mutex.Unlock()
	for _, err := range unchecked {
		if !slices.ContainsFunc(h.errorMatches, func(match func(error) bool) bool { return match(err) }) {
			h.t.Errorf("the kernel logged %v, which the test did not expect", err)
		}
	}
}

// quit quits the supervisor, waits until every kernel is destroyed, and
// checks the errors logged.
func (h *Harness) quit() {
	h.t.Helper()
	h.supervisor.Quit()
	select {
	case <-h.supervisor.Done():
	case <-time.After(Timeout):
		h.t.Fatalf("sfxtest: the supervisor's quit did not finish in %v", Timeout)
	}
	h.checkLogged()
}
