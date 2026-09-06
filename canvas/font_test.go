package canvas

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/m"
)

// TestTheDefaultFontIsMountedWithItsLicence asserts the embedded font reaches
// storage through the same built-in mount as the shaders, so no caller has to
// vendor or mount anything to draw text.
func TestTheDefaultFontIsMountedWithItsLicence(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(*OpQueue) {})
	for _, path := range []string{DefaultFontPath, defaultFontLicensePath} {
		want, err := fs.ReadFile(builtinFS, path)
		if err != nil {
			t.Fatalf("read embedded file %q: %v", path, err)
		}
		got, _ := k.ExecuteCommand[readFileProbeCmd](readFileProbeRequest{Name: path})
		if !bytes.Equal(got.Data, want) {
			t.Fatalf("mounted file %q differs from the embedded source", path)
		}
	}
}

// TestNoFontPathDrawsWithTheDefaultFont is the ticket's headline: Text with an
// empty path must render, and render exactly what naming the default renders.
func TestNoFontPathDrawsWithTheDefaultFont(t *testing.T) {
	draw := TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1}}

	implicit, _, implicitBackend := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(write *OpQueue) {
		write.Text(0, "", "Ag", draw)
	})
	runFrame(implicit)

	explicit, _, explicitBackend := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(write *OpQueue) {
		write.Text(0, DefaultFontPath, "Ag", draw)
	})
	runFrame(explicit)

	if implicitBackend.draws == 0 {
		t.Fatalf("draws = 0, want the empty font path to render through the embedded default")
	}
	if implicitBackend.draws != explicitBackend.draws {
		t.Fatalf("draws = %d with no font path, %d naming the default; want the same frame",
			implicitBackend.draws, explicitBackend.draws)
	}
	if len(implicitBackend.updates) != 2 {
		t.Fatalf("glyph atlas updates = %d, want one rasterized glyph each for 'A' and 'g'",
			len(implicitBackend.updates))
	}
	if len(implicitBackend.updates) != len(explicitBackend.updates) {
		t.Fatalf("atlas updates = %d with no font path, %d naming the default; want the same glyphs",
			len(implicitBackend.updates), len(explicitBackend.updates))
	}
	if !bytes.Equal(implicitBackend.updates[0].pixels, explicitBackend.updates[0].pixels) {
		t.Fatalf("the glyph rasterized from the empty path differs from the one named explicitly")
	}
}

// TestNoFontPathMeasuresWithTheDefaultFont covers the measurement half of the
// same rule: ui sizes labels through Lookup, so a zero-value Font must measure.
func TestNoFontPathMeasuresWithTheDefaultFont(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, DefaultConfig(), func(*OpQueue) {})
	probeLookup(k, func(lookup LookupAccess) {
		implicit := lookup.MeasureTextSize("", 16, "Ag")
		explicit := lookup.MeasureTextSize(DefaultFontPath, 16, "Ag")
		if implicit.X <= 0 || implicit.Y <= 0 {
			t.Errorf("measured %+v with no font path, want a real size", implicit)
		}
		if implicit != explicit {
			t.Errorf("measured %+v with no font path, %+v naming the default; want the same size",
				implicit, explicit)
		}
		if metrics := lookup.FontMetrics("", 16); metrics.LineHeight <= 0 {
			t.Errorf("metrics = %+v with no font path, want the default font's metrics", metrics)
		}
	})
}

// TestAMissingFontIsNotSubstituted is the other half of the rule. An empty path
// asks for the default; a named path that cannot be loaded must keep failing,
// so a missing asset stays visible instead of rendering in another typeface.
func TestAMissingFontIsNotSubstituted(t *testing.T) {
	k, errs := testKernelCapturing(t, fstest.MapFS{}, DefaultConfig(), func(write *OpQueue) {
		write.Text(0, "fonts/absent.ttf", "Ag", TextDraw{Position: m.Vec2{Y: 40}, Size: 16})
	})
	runFrame(k)

	probeLookup(k, func(lookup LookupAccess) {
		if got := lookup.MeasureTextSize("fonts/absent.ttf", 16, "Ag"); got != (m.Vec2{}) {
			t.Errorf("measured %+v for a missing font, want zero rather than the default's size", got)
		}
	})
	if len(*errs) == 0 {
		t.Fatalf("a missing font reported no error; want it to keep failing loudly")
	}
}
