package internal

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
)

// instanceTint reads one batched instance's tint, the field every effect that
// colours a mark reads. Its offset is the instance record's, asserted against
// the record layout by the halo tests rather than restated here.
func instanceTint(buffer []byte, i int) m.Color {
	record := instanceAt(buffer, i)
	return m.Color{
		R: floatAt(record, 48), G: floatAt(record, 52),
		B: floatAt(record, 56), A: floatAt(record, 60),
	}
}

// TextDraw.Color carries two things at once, because m.Color does, and an inline
// icon obeys exactly one of them.
//
// Rgb says what colour the ink is. For a glyph that is the whole mark - the font
// atlas holds RGB=255 with coverage in alpha, so multiplying by tint is literally
// what colours the text - but an icon's texel carries its own artwork, and the
// same multiply is a modulation that destroys it. White stays the identity.
//
// Alpha says how present the run is, which is meaningful for anything drawn,
// artwork included. ui varies a label's colour per VisualState through the
// published InteractiveTextParams, so a disabled label has to take its coin down
// with it rather than leave it riding fully opaque over faded text.
func TestAnInlineIconTakesTheRunsAlphaAndNotItsColour(t *testing.T) {
	ink := m.Color{R: 0.2, G: 0.4, B: 0.8, A: 0.5}
	filesystem := fstest.MapFS{"icon.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	k, _, backend := testKernel(t, filesystem, Config{}, func(write *OpQueue) {
		write.Text(0, "", "${icon.png}", TextDraw{
			Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink,
		})
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 || len(instances[0])/testInstanceSize != 1 {
		t.Fatalf("instance buffers = %d, want the one icon", len(instances))
	}
	want := DefaultTint().Fade(ink.A)
	if got := instanceTint(instances[0], 0); got != want {
		t.Fatalf("inline icon tint = %+v, want %+v - the artwork untouched, at the run's presence", got, want)
	}
}

// The glyph half of the same rule, in one run so the two cannot drift apart
// unnoticed. A glyph takes the whole Color and an icon takes only its alpha:
// they are deliberately different rather than two call sites that happen to
// disagree, and a later change that tints an icon whole - the reading this
// ticket refused - fails here rather than passing quietly.
//
// The two land in separate instance buffers because the batch key includes the
// texture and glyphs come from the font atlas while icons come from the sprite
// atlas. They are told apart by how many records each holds, never by the tint
// under test.
func TestAGlyphAndAnInlineIconInOneRunTakeDifferentTints(t *testing.T) {
	ink := m.Color{R: 0.2, G: 0.4, B: 0.8, A: 0.5}
	filesystem := fstest.MapFS{"icon.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	k, _, backend := testKernel(t, filesystem, Config{}, func(write *OpQueue) {
		write.Text(0, "", "ab${icon.png}", TextDraw{
			Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink,
		})
	})
	runFrame(k)

	var glyphs, icons []byte
	for _, buffer := range spriteInstances(backend) {
		switch len(buffer) / testInstanceSize {
		case 2:
			glyphs = buffer
		case 1:
			icons = buffer
		}
	}
	if glyphs == nil || icons == nil {
		t.Fatalf("instance buffers = %d, want the two-glyph run and the one icon", len(spriteInstances(backend)))
	}
	for i := 0; i < 2; i++ {
		if got := instanceTint(glyphs, i); got != TintFor(ink) {
			t.Fatalf("glyph %d tint = %+v, want the tint of the run's whole colour %+v", i, got, TintFor(ink))
		}
	}
	want := DefaultTint().Fade(ink.A)
	if got := instanceTint(icons, 0); got != want {
		t.Fatalf("inline icon tint = %+v, want %+v", got, want)
	}
}

// The consequence beyond colour, which issue #192 surfaced and ruled out of
// scope: a halo takes its colour and its peak alpha from tint, so whatever an
// inline icon batches is what haloes it. Under this rule the band is neutral in
// colour and follows the run in presence - a fading label takes its icon's band
// down with it, and the band under artwork is white rather than the run's ink.
//
// That is the settled answer rather than a hole left open, so it is asserted
// here rather than described somewhere. A halo that sampled the artwork for a
// band colour would be a new mechanism, not a fix to this one.
func TestAnInlineIconHaloesWhiteAtTheRunsAlpha(t *testing.T) {
	ink := m.Color{R: 0.2, G: 0.4, B: 0.8, A: 0.5}
	filesystem := fstest.MapFS{"icon.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	k, _, backend := testKernel(t, filesystem, Config{}, func(write *OpQueue) {
		write.Text(0, "", "${icon.png}", TextDraw{
			Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink,
		})
		write.SetLayerMaterial(0, HaloMaterialSet(DefaultHaloProfile()))
	})
	runFrame(k)

	if len(backend.pipelines) == 0 {
		t.Fatal("nothing was drawn")
	}
	for i := range backend.pipelines {
		if !strings.Contains(backend.pipelineShader(i), "fn haloFalloff") {
			t.Fatalf("pipeline %d was not built from the halo material; the band under test is not a halo", i)
		}
	}
	instances := spriteInstances(backend)
	if len(instances) != 1 || len(instances[0])/testInstanceSize != 1 {
		t.Fatalf("instance buffers = %d, want the one haloed icon", len(instances))
	}
	want := DefaultTint().Fade(ink.A)
	if got := instanceTint(instances[0], 0); got != want {
		t.Fatalf("haloed icon tint = %+v, want %+v - a neutral band at the run's presence", got, want)
	}
}

// ${path|scale} grows the icon by scale on both axes about the middle of the
// cap height, so it stays centred on the letters it sits among.
func TestAScaledInlineIconGrowsAboutTheCapHeightsMiddle(t *testing.T) {
	filesystem := fstest.MapFS{"icon.png": &fstest.MapFile{Data: pngBytes(t, 8, 4)}}
	k, _, backend := testKernel(t, filesystem, Config{}, func(write *OpQueue) {
		write.Text(0, "", "${icon.png}", TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1}})
		write.Text(1, "", "${icon.png|1.5}", TextDraw{Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1}})
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 2 {
		t.Fatalf("instance buffers = %d, want one per layer", len(instances))
	}
	plain, scaled := instanceAt(instances[0], 0), instanceAt(instances[1], 0)
	pw, ph := floatAt(plain, 8), floatAt(plain, 12)
	sw, sh := floatAt(scaled, 8), floatAt(scaled, 12)
	if pw != 2*ph {
		t.Fatalf("plain icon %vx%v, want the source's 2:1 aspect", pw, ph)
	}
	if !nearly(sw, pw*1.5) || !nearly(sh, ph*1.5) {
		t.Fatalf("scaled icon %vx%v, want 1.5 x %vx%v", sw, sh, pw, ph)
	}
	if px, sx := floatAt(plain, 0), floatAt(scaled, 0); !nearly(px, sx) {
		t.Fatalf("scaled icon starts at x %v, want the pen's %v", sx, px)
	}
	if pc, sc := floatAt(plain, 4)+ph/2, floatAt(scaled, 4)+sh/2; !nearly(pc, sc) {
		t.Fatalf("scaled icon centred at y %v, want the plain icon's %v", sc, pc)
	}
}

func nearly(a, b float32) bool {
	d := a - b
	return d < 1e-3 && d > -1e-3
}

// Layout measures a scaled icon as wide as it draws, so a label sized from the
// measurement holds it and wrapping breaks where the drawn line would.
func TestAScaledInlineIconMeasuresAsWideAsItDraws(t *testing.T) {
	filesystem := fstest.MapFS{"icon.png": &fstest.MapFile{Data: pngBytes(t, 8, 4)}}
	k, _, _ := testKernel(t, filesystem, Config{}, func(*OpQueue) {})
	probeLookup(k, func(lookup LookupAccess) {
		plain := lookup.MeasureTextSize("", 16, "${icon.png}")
		scaled := lookup.MeasureTextSize("", 16, "${icon.png|1.5}")
		if plain.X <= 0 || !nearly(scaled.X, plain.X*1.5) {
			t.Errorf("scaled icon measures %v wide, want 1.5 x %v", scaled.X, plain.X)
		}
		if scaled.Y != plain.Y {
			t.Errorf("scaled icon line is %v tall, want the line height %v it may overlap", scaled.Y, plain.Y)
		}
	})
}
