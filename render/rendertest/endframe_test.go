package rendertest_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
	"github.com/dvoyni/cog/render/rendertest"
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
	// renderTextures are the gfx textures render made, and lastFrameCallKind
	// the kind of the newest acquire or present before each call.
	renderTextures := map[gfx.Texture]bool{}
	var lastFrameCallKind recording.CallKind
	releasedCount := 0
	releasedAfterPresent := true
	for _, call := range calls {
		switch call.Kind {
		case recording.CallKindCreateTexture:
			arguments := call.Arguments.(recording.CreateTextureArguments)
			_, renderTextures[arguments.Texture] = handle.ParseLabel[render.Texture](arguments.Descriptor.Label)
		case recording.CallKindAcquireFramebuffer, recording.CallKindPresent:
			lastFrameCallKind = call.Kind
		case recording.CallKindReleaseTexture:
			if renderTextures[call.Arguments.(gfx.Texture)] {
				releasedCount++
				releasedAfterPresent = releasedAfterPresent && lastFrameCallKind == recording.CallKindPresent
			}
		}
	}
	return releasedCount, releasedAfterPresent
}
