package rendertest_test

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// benchmarkSwitchPeriod is how many draws the benchmarks' frames draw with
// one material before switching to the other.
const benchmarkSwitchPeriod = 16

// benchmarkFrames steps h gateWarmFrameCount frames, and then a frame for
// each of b's iterations, reporting the time of each frame's drawCount
// draws.
func benchmarkFrames(b *testing.B, h *rendertest.Harness, drawCount int) {
	b.ReportAllocs()
	for range gateWarmFrameCount {
		h.Step()
	}
	for b.Loop() {
		h.Step()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*drawCount), "ns/draw")
}

// BenchmarkDraws benchmarks a frame of an indexed mesh drawn with a uniform
// block draw parameter, the material switched every benchmarkSwitchPeriod
// draws, on render and drawn by hand on gfx, on gfx's recording adapter
// recording nothing, reporting the time per draw.
func BenchmarkDraws(b *testing.B) {
	for _, drawCount := range []int{256, 4096} {
		b.Run(fmt.Sprintf("render/draws=%d", drawCount), func(b *testing.B) {
			h := rendertest.NewHarness(b)
			h.RecordNothing()
			spawnGateHarness(b, h, gateScenario{shape: gateShapeMesh, drawCount: drawCount, listCount: 1, passCount: 1, switchPeriod: benchmarkSwitchPeriod, drawsOnly: true})
			benchmarkFrames(b, h, drawCount)
		})
		b.Run(fmt.Sprintf("gfx/draws=%d", drawCount), func(b *testing.B) {
			h := rendertest.NewHarness(b)
			h.RecordNothing()
			h.Spawn(nil, &handDrawnPlugin{files: makeGateFiles(b), drawCount: drawCount})
			h.OpenWindow(platform.Size{Width: 64, Height: 48})
			benchmarkFrames(b, h, drawCount)
		})
	}
}
