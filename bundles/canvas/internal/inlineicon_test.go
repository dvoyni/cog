package internal

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas"
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
	k, _, backend := testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, "", "${icon.png}", canvas.TextDraw{
			Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink,
		})
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 || len(instances[0])/testInstanceSize != 1 {
		t.Fatalf("instance buffers = %d, want the one icon", len(instances))
	}
	want := m.Color{R: 1, G: 1, B: 1, A: ink.A}
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
	k, _, backend := testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, "", "ab${icon.png}", canvas.TextDraw{
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
		if got := instanceTint(glyphs, i); got != ink {
			t.Fatalf("glyph %d tint = %+v, want the run's whole colour %+v", i, got, ink)
		}
	}
	want := m.Color{R: 1, G: 1, B: 1, A: ink.A}
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
	k, _, backend := testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Text(0, "", "${icon.png}", canvas.TextDraw{
			Position: m.Vec2{X: 10, Y: 40}, Size: 16, Color: ink,
		})
		write.SetLayerMaterial(0, canvas.HaloMaterialSet(canvas.DefaultHaloProfile()))
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
	want := m.Color{R: 1, G: 1, B: 1, A: ink.A}
	if got := instanceTint(instances[0], 0); got != want {
		t.Fatalf("haloed icon tint = %+v, want %+v - a neutral band at the run's presence", got, want)
	}
}
