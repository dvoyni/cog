package canvastest

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// Harness runs canvas in a real kernel on render's fake GPU, for canvas's
// tests and those of the plugins that record into it. It is rendertest's
// harness, whose calls it keeps, with canvas spawned after render, a
// recording task of its own after canvas, and canvas's reports expected by
// kind.
//
// It is strict, as rendertest's is: the test fails on every error the
// kernel logged, unless it is a canvas.Report of a kind the test named with
// ExpectReport, a render.Report of a kind named with the embedded harness's
// ExpectReport, or an error a match given ExpectError answers true of.
type Harness struct {
	*rendertest.Harness
	t testing.TB
	// mutex guards records, the bodies Record queued for the next frame,
	// which its recording task takes.
	mutex   sync.Mutex
	records []func(list canvas.DrawList)
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
	h := &Harness{Harness: rendertest.NewHarness(t), t: t}
	h.Harness.ExpectError(h.matchReport)
	return h
}

// Spawn spawns rendertest's plugins with renderConfig, canvas with
// canvasConfig, the harness's own plugin, and plugins, in that order, so
// the test's plugins are listed after canvas, and their draw lists merge
// after the harness's. It fails the test when the spawn fails.
func (h *Harness) Spawn(renderConfig *render.Config, canvasConfig *canvas.Config, plugins ...kernel.Plugin) {
	h.t.Helper()
	h.Harness.Spawn(renderConfig, append([]kernel.Plugin{configuredPlugin{Plugin: canvas.NewPlugin(), config: canvasConfig}, recordPlugin{harness: h}}, plugins...)...)
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
