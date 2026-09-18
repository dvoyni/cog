package internal

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"golang.org/x/image/font/gofont/goregular"
)

// The font store is two caches, and these are the two release rules that make it
// two. A font file is one asset and a face baked from it is another thing made of
// it: unloading the font frees every sized face it backs, and a framebuffer
// resize frees every face and keeps every parsed source. One cache keyed by path
// cannot say the second at all, and one cache keyed by path and size would
// re-parse the file once per size.

const testFontPath = "fonts/text.ttf"

// fontRig builds a harness that counts storage opens and atlas uploads, drawing
// text at every size given. The viewport is 100x100 logical over a 200x200
// framebuffer, so a logical size rasterizes at twice its value.
func fontRig(t *testing.T, sizes ...float32) (k kernel.Executioner, filesystem *testFS, backend *testBackend) {
	t.Helper()
	filesystem = &testFS{FS: fstest.MapFS{testFontPath: &fstest.MapFile{Data: goregular.TTF}}}
	k, _, backend = testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		for i, size := range sizes {
			write.Text(0, testFontPath, "Ag", canvas.TextDraw{
				Position: m.Vec2{X: 10, Y: 20 * float32(i+1)}, Size: size,
				Color: m.Color{R: 1, G: 1, B: 1, A: 1},
			})
		}
	})
	return k, filesystem, backend
}

// TestAFramebufferResizeFreesEveryFaceAndKeepsEverySource is the property that
// decides the shape. Every face is dropped so glyphs re-rasterize at the new
// device resolution, and nothing is re-read or re-parsed to do it.
func TestAFramebufferResizeFreesEveryFaceAndKeepsEverySource(t *testing.T) {
	k, filesystem, backend := fontRig(t, 16)

	runFrame(k)
	// The white texel is reserved before any layer's ops, then one rasterized
	// glyph each for 'A' and 'g'.
	if len(backend.updates) != 3 {
		t.Fatalf("uploads after the first frame = %d, want the white texel plus 'A' and 'g'", len(backend.updates))
	}
	runFrame(k)
	if len(backend.updates) != 3 {
		t.Fatalf("uploads after a second frame = %d, want the baked face and its glyphs reused", len(backend.updates))
	}
	if filesystem.opens != 1 {
		t.Fatalf("font opens = %d, want the one read the parse needed", filesystem.opens)
	}

	k.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 100, Height: 100, FramebufferWidth: 400, FramebufferHeight: 400,
	})
	runFrame(k)

	if len(backend.updates) != 5 {
		t.Fatalf("uploads after the resize = %d, want 'A' and 'g' rasterized again at the new scale", len(backend.updates))
	}
	// This is the half a single cache keyed by path cannot express: the faces are
	// gone and the parsed source is not, so nothing went back to storage.
	if filesystem.opens != 1 {
		t.Fatalf("font opens after the resize = %d, want the parsed source kept and the file never re-read", filesystem.opens)
	}
}

// TestUnloadingAFontFreesEveryFaceBakedFromIt is the other half: an unload names
// a path and the sizes are whatever was ever baked, so it is a predicate over
// entries rather than a set of keys. Both faces go, and the source goes with
// them.
func TestUnloadingAFontFreesEveryFaceBakedFromIt(t *testing.T) {
	k, filesystem, backend := fontRig(t, 16, 24)

	runFrame(k)
	// The white texel, then 'A' and 'g' at each of the two rasterization sizes.
	if len(backend.updates) != 5 {
		t.Fatalf("uploads after the first frame = %d, want the white texel plus two glyphs at each of two sizes", len(backend.updates))
	}
	runFrame(k)
	if len(backend.updates) != 5 || filesystem.opens != 1 {
		t.Fatalf("uploads = %d and opens = %d on a second frame, want both faces and the one parse reused",
			len(backend.updates), filesystem.opens)
	}

	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadFont(testFontPath) })
	runFrame(k)

	if len(backend.updates) != 9 {
		t.Fatalf("uploads after the unload = %d, want both sized faces re-baked and all four glyphs rasterized again",
			len(backend.updates))
	}
	if filesystem.opens != 2 {
		t.Fatalf("font opens after the unload = %d, want the source freed with its faces and the file re-read once",
			filesystem.opens)
	}
}

// TestAMissingFontIsOneReportPerEpisodeAcrossSizes pins where the report lives.
// Only the source tier names a file, so a font that is not there is the
// Library's one read failure however many sizes ask for it and whichever verb
// asks - and freeing it is what lets it speak again.
func TestAMissingFontIsOneReportPerEpisodeAcrossSizes(t *testing.T) {
	const absent = "fonts/absent.ttf"
	k, errs, _ := testKernelCapturing(t, fstest.MapFS{}, canvas.Config{}, func(write *canvas.OpQueue) {
		for _, size := range []float32{16, 24, 32} {
			write.Text(0, absent, "Ag", canvas.TextDraw{Size: size})
		}
	})
	runFrame(k)
	runFrame(k)
	probeLookup(k, func(lookup canvas.LookupAccess) {
		lookup.MeasureTextSize(absent, 16, "Ag")
		lookup.MeasureWrappedTextSize(absent, 40, "Ag", 100)
		lookup.FontMetrics(absent, 12)
	})
	if len(*errs) != 1 {
		t.Fatalf("reports for one missing font = %d (%v), want the one read failure the source tier is", len(*errs), *errs)
	}

	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadFont(absent) })
	runFrame(k)
	if len(*errs) != 2 {
		t.Fatalf("reports after the unload = %d, want the freed entry to have forgotten what it said", len(*errs))
	}
}
