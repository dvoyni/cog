package rendertest_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

func TestRenderFreesIdleTemporariesAfterTheFramesPresentBeforeTheOutOfItsHandler(t *testing.T) {
	const beats = 20
	h := rendertest.NewHarness(t)
	// releasedCounts is how many temporaries render had released when each
	// beat's job after render's handler ran.
	var releasedCounts []int
	h.Spawn(&render.Config{TemporaryIdleFrames: 1}, &temporaryPlugin{onRenderEnd: func() {
		releasedCount, _ := countReleasedTemporaries(h.Calls())
		releasedCounts = append(releasedCounts, releasedCount)
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for range beats {
		h.Step()
	}

	// Each frame's end frees the frame before's temporaries.
	var want []int
	for beat := range beats {
		want = append(want, beat*temporaryCount)
	}
	if !slices.Equal(releasedCounts, want) {
		t.Errorf("by the out of render's handler, render had released %v temporaries, beat by beat, want %v", releasedCounts, want)
	}
	if _, releasedAfterPresent := countReleasedTemporaries(h.Calls()); !releasedAfterPresent {
		t.Errorf("render released a temporary after the next frame's acquire, want each after its frame's present")
	}
}

// countReleasedTemporaries answers how many of render's textures calls
// release, and whether each release follows a present, with no acquire of a
// framebuffer between them.
func countReleasedTemporaries(calls []recording.Call) (int, bool) {
	releasedCount := 0
	releasedAfterPresent := true
	for _, call := range readTextureCalls(calls) {
		if call.kind == recording.CallKindReleaseTexture {
			releasedCount++
			releasedAfterPresent = releasedAfterPresent && call.betweenFrames
		}
	}
	return releasedCount, releasedAfterPresent
}
