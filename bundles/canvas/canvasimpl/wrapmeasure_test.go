package canvasimpl

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/extensions/storage"
	"golang.org/x/image/font/gofont/goregular"
)

// Layout measures text with the face at its logical size while drawing rasterizes
// at the device size, and hinted advances do not agree between the two. Wrapping
// must follow the layout metrics, or a line arranged to exactly fit its measured
// width drops its last word onto a line the element was never sized for.
func TestWrapMeasureFollowsLayoutMetrics(t *testing.T) {
	const path = "font.ttf"
	const size = 18
	filesystem := storage.NewFileSystem("test", fstest.MapFS{path: &fstest.MapFile{Data: goregular.TTF}})
	fonts := internal.NewFontStore()
	logical := fonts.Face(filesystem, path, size)
	raster := fonts.Face(filesystem, path, size*2)
	if logical == nil || raster == nil {
		t.Fatal("test font could not be loaded")
	}

	const text = "a line that exactly fills its measured width"
	lines := internal.ParseInlineText(text)
	arranged := internal.MeasureLine(logical, text)
	rasterized := func(segments []internal.InlineSegment) float32 {
		var width float32
		for _, segment := range segments {
			width += internal.MeasureLine(raster, segment.Text) * 0.5
		}
		return width
	}
	if rasterized(lines[0]) <= arranged {
		t.Skip("test text no longer exercises rasterization drift for this font")
	}
	if got := internal.WrapInlineText(lines, arranged, rasterized); len(got) != 2 {
		t.Fatalf("rasterized wrapping produced %d lines, want the 2 the drift causes", len(got))
	}

	wrap := (&plugin{}).wrapMeasure(nil, nil, filesystem, fonts, path, size, rasterized)
	got := internal.WrapInlineText(lines, arranged, wrap)
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].Icon || got[0][0].Text != text {
		t.Fatalf("layout-measured wrapping = %+v, want the one line %q", got, text)
	}
}
