package canvastest

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// Harness runs canvas in a real kernel on render's fake GPU, for canvas's
// tests and those of the plugins that record into it. It is rendertest's
// harness, whose calls it keeps, with canvas spawned after render, a
// recording task of its own after canvas, each frame inspected in canvas's
// terms, and canvas's reports expected by kind.
//
// It is strict, as rendertest's is: the test fails on every error the
// kernel logged, unless it is a canvas.Report of a kind the test named with
// ExpectReport, a render.Report of a kind named with the embedded harness's
// ExpectReport, or an error a match given ExpectError answers true of.
type Harness struct {
	*rendertest.Harness
	t testing.TB
	// kernel is the harness's kernel, which its plugin hands it at Start.
	kernel *kernel.Kernel
	// inspectingNothing is set by RecordNothing, when Step asks for no
	// inspection.
	inspectingNothing bool
	// mutex guards records, the bodies Record queued for the next frame,
	// which its recording task takes.
	mutex   sync.Mutex
	records []func(list canvas.DrawList)
	// deliveredInspections carries each inspection canvas delivers to Step,
	// which keeps it in inspections, for Frames; only the test's goroutine
	// reads and writes inspections.
	deliveredInspections chan canvas.Inspection
	inspections          []canvas.Inspection
	// expectedReports are the kinds ExpectReport named, and reports every
	// canvas.Report the harness checked; only the test's goroutine, which
	// checks what was logged, reads and writes them.
	expectedReports []canvas.ReportKind
	reports         []canvas.Report
}

// NewHarness answers a harness whose kernel is not yet spawned, quit and
// checked when the test ends, as rendertest's is.
func NewHarness(t testing.TB) *Harness {
	t.Helper()
	h := &Harness{Harness: rendertest.NewHarness(t), t: t, deliveredInspections: make(chan canvas.Inspection, 1)}
	h.Harness.ExpectError(h.matchReport)
	return h
}

// Spawn spawns rendertest's plugins with renderConfig, canvas with
// canvasConfig, the harness's own plugin, and plugins, in that order, so
// the test's plugins are listed after canvas, and their draw lists merge
// after the harness's. It fails the test when the spawn fails.
func (h *Harness) Spawn(renderConfig *render.Config, canvasConfig *canvas.Config, plugins ...kernel.Plugin) {
	h.t.Helper()
	h.Harness.Spawn(renderConfig, append([]kernel.Plugin{configuredPlugin{Plugin: canvas.NewPlugin(), config: canvasConfig}, harnessPlugin{harness: h}}, plugins...)...)
}

// Step draws one beat, as the embedded harness's Step does, its frame
// inspected: it asks canvas for an inspection before the beat, and returns
// once canvas has delivered it, for Frames. After RecordNothing, it asks for
// none. A frame whose inspection waits for render's pass timings is
// delivered a beat later, so Step fails on it.
func (h *Harness) Step() {
	h.t.Helper()
	if h.inspectingNothing {
		h.Harness.Step()
		return
	}
	_, requestJob := h.kernel.Publish(inspectionRequestEvent{})
	signal := kernel.NewSignal()
	h.kernel.Notify(signal, requestJob)
	select {
	case <-signal.C():
	case <-time.After(rendertest.Timeout):
		h.t.Fatalf("canvastest: the request for an inspection did not finish in %v", rendertest.Timeout)
	}
	h.Harness.Step()
	select {
	case inspection := <-h.deliveredInspections:
		h.inspections = append(h.inspections, inspection)
	case <-time.After(rendertest.Timeout):
		h.t.Fatalf("canvastest: the inspection of the frame stepped was not delivered in %v", rendertest.Timeout)
	}
}

// Frames answers each frame Step drew so far, inspected in canvas's terms,
// with render's inspection of it beside it: its cameras, their batches and
// entries, and render's command lists. After RecordNothing, it answers no
// frame. The embedded harness's Frames answers what gfx's recording adapter
// recorded of the same frames.
func (h *Harness) Frames() []canvas.Inspection {
	return slices.Clone(h.inspections)
}

// RecordNothing puts gfx's recording adapter in its mode that records
// nothing, as the embedded harness's does, and has Step ask for no
// inspection, so a frame allocates only what canvas and render do for it.
// It is called before Spawn.
func (h *Harness) RecordNothing() {
	h.t.Helper()
	h.inspectingNothing = true
	h.Harness.RecordNothing()
}

// Record has the harness's recording task, hung off canvas.FrameEvent, call
// record with its draw list in the next frame, after every record queued
// before it, so a test records a frame without writing a plugin.
func (h *Harness) Record(record func(list canvas.DrawList)) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.records = append(h.records, record)
}

// ExpectReport names a kind of report the test expects: a canvas.Report of it
// that the kernel logs fails no test. A canvas report of a kind no call
// named fails it.
func (h *Harness) ExpectReport(kind canvas.ReportKind) {
	h.expectedReports = append(h.expectedReports, kind)
}

// Reports answers every canvas.Report the harness has checked so far,
// expected or not, in the order logged: each Step checks what was logged
// during it.
func (h *Harness) Reports() []canvas.Report {
	return slices.Clone(h.reports)
}

// takeRecords answers the records queued for this frame, and queues none
// for the next.
func (h *Harness) takeRecords() []func(list canvas.DrawList) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	records := h.records
	h.records = nil
	return records
}

// matchReport is the first match the embedded harness checks a logged error
// with: it keeps each canvas.Report, and answers true of one of a kind
// ExpectReport named.
func (h *Harness) matchReport(logged error) bool {
	report, isReport := errors.AsType[canvas.Report](logged)
	if !isReport {
		return false
	}
	h.reports = append(h.reports, report)
	return slices.Contains(h.expectedReports, report.Kind)
}
