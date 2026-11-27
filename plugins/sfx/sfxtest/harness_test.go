package sfxtest_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// failureRecorder is the test a harness runs under, keeping each failure it
// is told of instead of failing, so a test can assert that the harness
// failed it.
type failureRecorder struct {
	testing.TB
	failures []string
}

func (r *failureRecorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// playMissingClip records a play of a path that is not there, and steps
// twice, so sfx logs the clip's failure and ends its voice.
func playMissingClip(h *sfxtest.Harness) {
	h.Record(func(queue sfx.Queue) { queue.Play("missing.ogg", 0, sfx.VoiceOptions{}) })
	h.Step()
	h.Step()
}

// An error the test did not expect through ExpectError fails it, at the Step
// that logged it, and Reports answers it all the same.
func TestAnErrorTheTestDidNotExpectFailsIt(t *testing.T) {
	recorder := &failureRecorder{TB: t}
	h := sfxtest.NewHarness(recorder)
	h.Spawn(nil)

	playMissingClip(h)

	if len(recorder.failures) != 1 {
		t.Fatalf("an unexpected error failed the test %d times, want once: %q", len(recorder.failures), recorder.failures)
	}
	if reports := h.Reports(); len(reports) != 1 || !isUnreadable(reports[0]) {
		t.Fatalf("the reports are %v, want the clip's ErrClipUnreadable", reports)
	}
}

// An error a match given ExpectError answers true of fails nothing, and is
// still among the reports.
func TestAnExpectedErrorFailsNothing(t *testing.T) {
	recorder := &failureRecorder{TB: t}
	h := sfxtest.NewHarness(recorder)
	h.Spawn(nil)
	h.ExpectError(isUnreadable)

	playMissingClip(h)

	if len(recorder.failures) != 0 {
		t.Fatalf("an expected error failed the test: %q", recorder.failures)
	}
	if reports := h.Reports(); len(reports) != 1 {
		t.Fatalf("the reports are %v, want the clip's failure", reports)
	}
}

// lifecycleStub is the lifecycle of a plugin of the test's that does
// nothing but its Init, for it to embed.
type lifecycleStub struct{}

func (lifecycleStub) Dependencies() []kernel.PluginName { return nil }
func (lifecycleStub) Start(*kernel.Kernel) error        { return nil }
func (lifecycleStub) Stop(*kernel.Kernel) error         { return nil }
func (lifecycleStub) Destroy() error                    { return nil }

// configRecorder is a plugin of the test's that keeps the config its Init
// is given.
type configRecorder struct {
	lifecycleStub
	config any
}

func (*configRecorder) Name() kernel.PluginName { return "configrecorder" }
func (r *configRecorder) Init(_ *kernel.Configurator, config any) error {
	r.config = config
	return nil
}

// Configure puts a config in the kernel's config under the name it is
// given, so a plugin among the test's is handed it at Init.
func TestConfigureHandsAPluginOfTheTestsItsConfig(t *testing.T) {
	recorder := &configRecorder{}
	config := &struct{ Retries int }{Retries: 3}
	h := sfxtest.NewHarness(t)
	h.Configure(recorder.Name(), config)
	h.Spawn(nil, recorder)

	if recorder.config != config {
		t.Fatalf("the plugin's Init was given %v, want the config Configure was given", recorder.config)
	}
}

// warningLogger is a plugin of the test's that logs one warning in Init.
type warningLogger struct {
	lifecycleStub
}

func (warningLogger) Name() kernel.PluginName { return "warninglogger" }
func (warningLogger) Init(configurator *kernel.Configurator, _ any) error {
	configurator.Logger().LogWarn("no sound card", "tries", 3)
	return nil
}

// Warnings answers each warning the kernel logged, its message and its args,
// and a warning fails no test.
func TestWarningsAnswersEachWarningLoggedAndFailsNothing(t *testing.T) {
	recorder := &failureRecorder{TB: t}
	h := sfxtest.NewHarness(recorder)
	h.Spawn(nil, warningLogger{})
	h.Step()

	if len(recorder.failures) != 0 {
		t.Fatalf("a warning failed the test: %q", recorder.failures)
	}
	warnings := h.Warnings()
	if len(warnings) != 1 || warnings[0].Message != "no sound card" || !slices.Equal(warnings[0].Args, []any{"tries", 3}) {
		t.Fatalf("the warnings are %v, want the one the plugin logged", warnings)
	}
}

// isUnreadable answers whether err is a clip's ErrClipUnreadable.
func isUnreadable(err error) bool {
	_, unreadable := errors.AsType[sfx.ErrClipUnreadable](err)
	return unreadable
}
