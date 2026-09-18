package internal

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
	"golang.org/x/image/font/gofont/goregular"
)

// TestTheDefaultFontIsMountedWithItsLicence asserts the embedded font reaches
// storage through the same built-in mount as the shaders, so no caller has to
// vendor or mount anything to draw text.
func TestTheDefaultFontIsMountedWithItsLicence(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, canvas.Config{}, func(*canvas.OpQueue) {})
	for _, path := range []string{canvas.DefaultFontPath, defaultFontLicensePath} {
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
	draw := canvas.TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1}}

	implicit, _, implicitBackend := testKernel(t, fstest.MapFS{}, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, "", "Ag", draw)
	})
	runFrame(implicit)

	explicit, _, explicitBackend := testKernel(t, fstest.MapFS{}, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, canvas.DefaultFontPath, "Ag", draw)
	})
	runFrame(explicit)

	if implicitBackend.draws == 0 {
		t.Fatalf("draws = 0, want the empty font path to render through the embedded default")
	}
	if implicitBackend.draws != explicitBackend.draws {
		t.Fatalf("draws = %d with no font path, %d naming the default; want the same frame",
			implicitBackend.draws, explicitBackend.draws)
	}
	// Three uploads, not two: every frame reserves the white texel before any
	// layer's ops, so it is the first upload even in a frame that only draws
	// text. Then one rasterized glyph each for 'A' and 'g'.
	if len(implicitBackend.updates) != 3 {
		t.Fatalf("atlas updates = %d, want the white texel plus one rasterized glyph each for 'A' and 'g'",
			len(implicitBackend.updates))
	}
	if len(implicitBackend.updates) != len(explicitBackend.updates) {
		t.Fatalf("atlas updates = %d with no font path, %d naming the default; want the same glyphs",
			len(implicitBackend.updates), len(explicitBackend.updates))
	}
	if !bytes.Equal(implicitBackend.updates[1].pixels, explicitBackend.updates[1].pixels) {
		t.Fatalf("the glyph rasterized from the empty path differs from the one named explicitly")
	}
}

// TestNoFontPathMeasuresWithTheDefaultFont covers the measurement half of the
// same rule: ui sizes labels through Lookup, so a zero-value Font must measure.
func TestNoFontPathMeasuresWithTheDefaultFont(t *testing.T) {
	k, _, _ := testKernel(t, fstest.MapFS{}, canvas.Config{}, func(*canvas.OpQueue) {})
	probeLookup(k, func(lookup canvas.LookupAccess) {
		implicit := lookup.MeasureTextSize("", 16, "Ag")
		explicit := lookup.MeasureTextSize(canvas.DefaultFontPath, 16, "Ag")
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

// Unloading is immediate now, and it lives on the facade that holds the device
// rather than on the one ui builds. There is no queue in front of it: the font is
// dropped at the call, and the next draw re-reads the file.
func TestUnloadingAFontDropsItAtTheCallAndRereadsOnTheNextDraw(t *testing.T) {
	const path = "fonts/text.ttf"
	filesystem := &testFS{FS: fstest.MapFS{path: &fstest.MapFile{Data: goregular.TTF}}}
	draw := canvas.TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1}}
	k, _, _ := testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, path, "Ag", draw)
	})
	runFrame(k)
	runFrame(k)
	if filesystem.opens != 1 {
		t.Fatalf("font opens across two frames = %d, want the one parse", filesystem.opens)
	}

	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadFont(path) })
	runFrame(k)
	if filesystem.opens != 2 {
		t.Fatalf("font opens after the unload = %d, want the file re-read", filesystem.opens)
	}
}

// TestAMissingFontIsNotSubstituted is the other half of the rule. An empty path
// asks for the default; a named path that cannot be loaded must keep failing,
// so a missing asset stays visible instead of rendering in another typeface.
func TestAMissingFontIsNotSubstituted(t *testing.T) {
	k, errs, _ := testKernelCapturing(t, fstest.MapFS{}, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, "fonts/absent.ttf", "Ag", canvas.TextDraw{Position: m.Vec2{Y: 40}, Size: 16})
	})
	runFrame(k)

	probeLookup(k, func(lookup canvas.LookupAccess) {
		if got := lookup.MeasureTextSize("fonts/absent.ttf", 16, "Ag"); got != (m.Vec2{}) {
			t.Errorf("measured %+v for a missing font, want zero rather than the default's size", got)
		}
	})
	if len(*errs) == 0 {
		t.Fatalf("a missing font reported no error; want it to keep failing loudly")
	}
}
