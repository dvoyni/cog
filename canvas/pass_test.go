package canvas

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// A contiguous run of canvas layers is one GPU pass: each layer declares its
// own, and gfx's merge rule collapses them because every pass but the first
// preserves both attachments and every pass but the last keeps both.
func TestCanvasLayersCollapseToOneGpuPass(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.Clear(0, m.Color{A: 1})
		write.FillRect(0, m.Rect{Width: 10, Height: 10}, m.Color{R: 1, A: 1})
		write.FillRect(1, m.Rect{Width: 10, Height: 10}, m.Color{G: 1, A: 1})
		write.FillRect(2, m.Rect{Width: 10, Height: 10}, m.Color{B: 1, A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 1 {
		t.Fatalf("GPU passes = %d, want the three layers merged into 1", len(backend.passes))
	}
	pass := backend.passes[0]
	if !pass.Screen || !pass.DepthAuto {
		t.Errorf("pass = %+v, want the screen with automatic depth", pass)
	}
	// The clear belongs to the layer it was recorded at, which is the lowest
	// here, so the merged run opens with it.
	if pass.Load != gfx.LoadClear || pass.Clear != (m.Color{A: 1}) {
		t.Errorf("pass load = (%v, %+v), want LoadClear with the recorded colour", pass.Load, pass.Clear)
	}
	// Depth clears once at the bottom of the run and is thrown away at the top.
	if pass.DepthLoad != gfx.LoadClear || pass.DepthClear != 1 {
		t.Errorf("depth load = (%v, %v), want LoadClear at 1", pass.DepthLoad, pass.DepthClear)
	}
	if pass.DepthStore != gfx.StoreDiscard || pass.Store != gfx.StoreKeep {
		t.Errorf("stores = (colour %v, depth %v), want keep and discard", pass.Store, pass.DepthStore)
	}
}

// Clear is positioned: it stays on the layer it names even when that layer
// draws nothing, instead of migrating to whichever layer happens to draw.
func TestClearStaysOnItsLayerWhenThatLayerIsEmpty(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.Clear(-5, m.Color{R: 1, A: 1})
		write.FillRect(3, m.Rect{Width: 10, Height: 10}, m.Color{A: 1})
	})
	runFrame(k)

	if len(backend.passes) != 1 {
		t.Fatalf("GPU passes = %d, want the empty clearing pass merged with the drawing one", len(backend.passes))
	}
	if backend.passes[0].Load != gfx.LoadClear || backend.passes[0].Clear != (m.Color{R: 1, A: 1}) {
		t.Errorf("pass load = (%v, %+v), want the clear from the empty layer below", backend.passes[0].Load, backend.passes[0].Clear)
	}
}
