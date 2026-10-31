package internal

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"golang.org/x/image/font/gofont/goregular"
)

// BenchmarkCanvasFlushText is the steady-state text frame: every face is baked,
// every glyph is rasterized, and the flush should resolve both by lookup alone.
// It is here because the font store became two caches and nothing else measures
// the path a frame takes through them.
func BenchmarkCanvasFlushText(b *testing.B) {
	const path = "fonts/text.ttf"
	filesystem := fstest.MapFS{path: &fstest.MapFile{Data: goregular.TTF}}
	k, _, backend := testKernel(b, filesystem, Config{}, func(write *OpQueue) {
		for i := range 40 {
			write.Text(Layer(i%4), path, "the quick brown fox", TextDraw{
				Position: m.Vec2{X: 4, Y: float32(i)}, Size: float32(8 + i%3),
				Color: m.Color{R: 1, G: 1, B: 1, A: 1},
			})
		}
	})
	runFrame(k)
	backend.capture = false
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		runFrame(k)
	}
}
