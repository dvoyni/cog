package rendertest_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestMain(m *testing.M) { rendertest.RunMain(m) }

func TestTheKernelsMainThreadIsTheMainGoroutineOnTheSoftwareAdapterAndTheTestsOwnOnTheRecordingAdapter(t *testing.T) {
	for _, test := range []struct {
		name            string
		softwareAdapter bool
		want            bool
	}{
		{"software adapter", true, true},
		{"recording adapter", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := rendertest.NewHarness(t)
			if test.softwareAdapter {
				h.UseSoftwareAdapter()
			}
			onMainGoroutine := false
			h.RunOnMainThread(func() { onMainGoroutine = isMainGoroutine() })
			if onMainGoroutine != test.want {
				t.Errorf("the harness ran its main-thread work on the main goroutine: %v, want %v", onMainGoroutine, test.want)
			}
		})
	}
}

func TestAPanicOnTheProcesssMainThreadPanicsAgainOnTheTestsGoroutine(t *testing.T) {
	h := rendertest.NewHarness(t)
	h.UseSoftwareAdapter()
	defer func() {
		if recovered := recover(); recovered != "refused" {
			t.Errorf("the test's goroutine recovered %v, want the main thread's panic, refused", recovered)
		}
	}()
	h.RunOnMainThread(func() { panic("refused") })
}

// isMainGoroutine answers whether the calling goroutine is the main one,
// which runs TestMain and RunMain: goroutine 1.
func isMainGoroutine() bool {
	stack := make([]byte, 64)
	return strings.HasPrefix(string(stack[:runtime.Stack(stack, false)]), "goroutine 1 [")
}
