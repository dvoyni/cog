package canvas_test

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

// exactFieldConfig is canvas's config whose default font, JetBrains Mono,
// makes its glyphs' fields of 100 texels to the em reaching 17 across their
// edges: the full block's edges, at -10, 610, -300 and 1020 of its 1000
// units to the em, then lie on whole texels, so each texel centre lies a
// whole and a half texels from an edge, which a spread of 8.5 holds exactly
// in a byte, and linear filtering between them answers the true distance.
var exactFieldConfig = &canvas.Config{DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{FieldEm: 100, FieldRange: 17}}}

// fullBlock is the full block, which JetBrains Mono draws over its whole
// line: 0.62 em wide about its advance's middle, and its ascent and descent
// tall, so a run of it alone, pivoted at its centre, fills a rectangle
// 0.31 em either side of its origin and 0.66 em above and below it.
const fullBlock = "█"

// blockHalfSize answers half the rectangle a run of fullBlock alone at size
// fills.
func blockHalfSize(size float32) m.Vec2 {
	return m.Vec2{X: 0.31 * size, Y: 0.66 * size}
}

// blockProbes answers probes across the middle of each side of the
// rectangle a run of fullBlock alone at size fills, centred at centre and
// turned by angle, fill showing over each, at distances from the side in
// world units across a few pixels on either side, its distance the
// rectangle's in the world.
func blockProbes(name string, centre m.Vec2, angle, size float32, fill canvas.Profile, distances []float32) []profileProbe {
	halfSize := blockHalfSize(size)
	rect := m.Rect{X: -halfSize.X, Y: -halfSize.Y, Width: 2 * halfSize.X, Height: 2 * halfSize.Y}
	turn := m.TRS2x3(centre, angle, m.Vec2{X: 1, Y: 1})
	unturn := m.TRS2x3(m.Vec2{}, -angle, m.Vec2{X: 1, Y: 1})
	distanceAt := func(point m.Vec2) float64 {
		return findRoundedRectDistance(unturn.TransformPoint(point.Sub(centre)), rect, 0)
	}
	sides := []struct {
		name   string
		middle m.Vec2
		out    m.Vec2
	}{
		{"left", m.Vec2{X: -halfSize.X}, m.Vec2{X: -1}},
		{"right", m.Vec2{X: halfSize.X}, m.Vec2{X: 1}},
		{"top", m.Vec2{Y: -halfSize.Y}, m.Vec2{Y: -1}},
		{"bottom", m.Vec2{Y: halfSize.Y}, m.Vec2{Y: 1}},
	}
	var probes []profileProbe
	for _, side := range sides {
		for _, distance := range distances {
			point := turn.TransformPoint(side.middle.Add(side.out.MulS(distance)))
			probes = append(probes, profileProbe{name: name + " by its " + side.name + " side", point: point, profile: fill, distanceAt: distanceAt})
		}
	}
	return probes
}

// whiteFill is a profile drawing what a run's white fill shows: solid inside
// the edge, anti-aliased across it.
var whiteFill = canvas.Outline(0, m.White)

func TestALabelDrawsItsGlyphsAtTwoSizesAndTurned(t *testing.T) {
	// A full block is drawn at 100 units, at 60, and at 60 turned a
	// quarter, its fill read across the middle of each side, within a few
	// pixels of it, where the field holds the distance exactly. At 60, of
	// exactFieldConfig's 100 field texels an em, a pixel spans 1.67 texels
	// at scale 1, under text_minified_texels, so at any scale from 1 the
	// fill is read at one point a pixel, never as small text.
	labels := []struct {
		name   string
		centre m.Vec2
		angle  float32
		size   float32
	}{
		{"a block at 100", m.Vec2{X: -90}, 0, 100},
		{"a block at 60", m.Vec2{X: 10, Y: -60}, 0, 60},
		{"a block at 60 turned a quarter", m.Vec2{X: 90, Y: 50}, math.Pi / 2, 60},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), exactFieldConfig, func(list canvas.DrawList) {
		for _, label := range labels {
			list.DrawText(canvas.Text{String: fullBlock, Size: label.size, Transform: m.TRS2x3(label.centre, label.angle, m.Vec2{X: 1, Y: 1})})
		}
	})

	var probes []profileProbe
	for _, label := range labels {
		probes = append(probes, blockProbes(label.name, label.centre, label.angle, label.size, whiteFill, []float32{-3, -1.5, -0.7, -0.2, 0.3, 0.8, 1.5, 3})...)
	}
	picture.expectProfiles(t, probes)
}

func TestAnOutlinedLabelShowsItsBandOutsideItsFillFadedByItsTintsAlpha(t *testing.T) {
	// Two full blocks at 100 units, a field texel a unit, are outlined 4
	// texels wide in orange: past each one's edge the band shows,
	// anti-aliased at its outer edge, the translucent one's at half its
	// colour, as its tint's alpha fades it, and inside the opaque one the
	// white fill covers it.
	const size = 100
	outline := canvas.Outline(4, orange)
	faded := outline
	faded.Color = outline.Color.MulS(0.5)
	opaqueCentre := m.Vec2{X: -60}
	translucentCentre := m.Vec2{X: 60}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), exactFieldConfig, func(list canvas.DrawList) {
		list.DrawTextWith(canvas.Text{String: fullBlock, Size: size, Transform: m.Translation2x3(opaqueCentre.X, opaqueCentre.Y)}, outline)
		list.DrawTextWith(canvas.Text{String: fullBlock, Size: size, Tint: m.Color{R: 1, G: 1, B: 1, A: 0.5}, Transform: m.Translation2x3(translucentCentre.X, translucentCentre.Y)}, outline)
	})

	bandDistances := []float32{1.5, 2.5, 3.3, 3.8, 4.3, 5.5, 7}
	picture.expectProfiles(t, blockProbes("the opaque block's band", opaqueCentre, 0, size, outline, bandDistances))
	picture.expectProfiles(t, blockProbes("the translucent block's band", translucentCentre, 0, size, faded, bandDistances))
	picture.expectProfiles(t, blockProbes("the opaque block's fill", opaqueCentre, 0, size, whiteFill, []float32{-6, -3, -1.5}))
	// Inside the translucent block, its half-white fill lies over the band,
	// which shows through the half the fill leaves.
	translucentFill := m.Color{R: 0.5, G: 0.5, B: 0.5, A: 0.5}
	underFill := canvas.Profile{Inner: math.MaxFloat32, Color: translucentFill.Add(faded.Color.MulS(0.5))}
	picture.expectProfiles(t, blockProbes("the translucent block's fill over its band", translucentCentre, 0, size, underFill, []float32{-6, -3, -1.5}))
}

func TestALabelsDropShadowFallsByItsOffsetWithinItsGlyphsQuads(t *testing.T) {
	// A full block at 100 units, a field texel a unit, casts a shadow 3
	// units right: past its right side the shadow shows, out to its offset
	// edge, and past its left side nothing does.
	const size = 100
	shadow := canvas.DropShadow(m.Vec2{X: 3}, 1, orange)

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), exactFieldConfig, func(list canvas.DrawList) {
		list.DrawTextWith(canvas.Text{String: fullBlock, Size: size}, shadow)
	})

	halfSize := blockHalfSize(size)
	rect := m.Rect{X: -halfSize.X, Y: -halfSize.Y, Width: 2 * halfSize.X, Height: 2 * halfSize.Y}
	distanceAt := func(point m.Vec2) float64 { return findRoundedRectDistance(point.Sub(shadow.Offset), rect, 0) }
	var probes []profileProbe
	for _, distance := range []float32{1.5, 2.2, 2.8, 3.3, 4.5, 6} {
		probes = append(probes, profileProbe{name: "the shadow past the right side", point: m.Vec2{X: halfSize.X + distance}, profile: shadow, distanceAt: distanceAt})
	}
	for _, distance := range []float32{1.5, 3, 6} {
		probes = append(probes, profileProbe{name: "the shadow past the left side", point: m.Vec2{X: -halfSize.X - distance}, profile: shadow, distanceAt: distanceAt})
	}
	picture.expectProfiles(t, probes)
}

func TestEveryLabelOfAFrameIsOneBatchOfTextMaterialOverTheGlyphAtlas(t *testing.T) {
	// Three labels, of different sizes, tints, transforms and profiles, one
	// with none, recorded out of their order, draw their glyphs as one
	// batch, in order: an instance for each glyph but the space, over the
	// glyph atlas.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	h.Record(func(list canvas.DrawList) {
		list.DrawTextWith(canvas.Text{String: "e f", Size: 12, Transform: m.TRS2x3(m.Vec2{X: 60}, 1, m.Vec2{X: 1, Y: 1}), Order: 2}, canvas.Glow(4, orange))
		list.DrawText(canvas.Text{String: "ab", Transform: m.Translation2x3(-60, 0)})
		list.DrawTextWith(canvas.Text{String: "cd", Size: 24, Tint: orange, Order: 1}, canvas.Outline(2, m.Black))
	})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 1 {
		t.Fatalf("the labels drew %d batches, want one", len(batches))
	}
	batch := batches[0]
	if batch.Material != canvas.TextMaterial || batch.InstanceCount != 6 || len(batch.Entries) != 3 {
		t.Errorf("the labels drew a batch of material %v, %d instances and %d entries, want TextMaterial's, 6, one a glyph, and 3", batch.Material, batch.InstanceCount, len(batch.Entries))
	}
	for index, entry := range batch.Entries {
		if entry.Kind != canvas.EntryKindText || entry.Order != int32(index) {
			t.Errorf("entry %d is of kind %v and order %d, want a run of text of order %d", index, entry.Kind, entry.Order, index)
		}
	}
	if label := findTextureLabels(frame.Render)[batch.Texture]; label != glyphAtlasLabel {
		t.Errorf("the labels drew from texture %q, want the glyph atlas", label)
	}
}

func TestARunIsCulledByItsGlyphsBounds(t *testing.T) {
	// The view spans 32 units either side of the origin and 24 above and
	// below it. A run placed by its top left 8 units left of the view
	// reaches into it, and is drawn; one 60 units further left is culled.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawText(canvas.Text{String: "ab", Pivot: canvas.PivotTopLeft, Transform: m.Translation2x3(-40, -10)})
		list.DrawText(canvas.Text{String: "cd", Pivot: canvas.PivotTopLeft, Transform: m.Translation2x3(-100, -10)})
	})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 1 || batches[0].InstanceCount != 2 || batches[0].Entries[0].Text.String != "ab" {
		t.Errorf("the runs drew batches %+v, want one, of the run reaching into the view", batches)
	}
}

func TestARunOfSpacesAloneDrawsNothing(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawText(canvas.Text{String: "   "})
	})

	h.Step()

	if batches := h.Frames()[0].Cameras[0].Batches; len(batches) != 0 {
		t.Errorf("a run of spaces drew batches %+v, want none", batches)
	}
}

func TestAGlyphPackedOntoAnOldPageAsTheGlyphAtlasGrowsStillDraws(t *testing.T) {
	// Pages of 84 texels square hold one full block at 48 units to the em,
	// its field a cell 48 texels wide and 82 tall, with room beside it on
	// its shelf for the full stop but not for the upper half block, which
	// grows the atlas onto a second page: the full stop, packed after it
	// onto the first page, is written where the growth copies that page
	// from, so it is not lost when the copy lands.
	config := &canvas.Config{AtlasPageSize: 84}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), config, func(list canvas.DrawList) {
		list.DrawText(canvas.Text{String: "█▀.", Size: 48})
	})

	// The run is 3 advances of 28.8 units wide, centred, its baseline 17.28
	// below its centre.
	picture.expect(t, []probe{
		{"the full block's middle", m.Vec2{X: -28.8}, opaqueWhite},
		{"the upper half block's upper middle", m.Vec2{Y: -16}, opaqueWhite},
	})
	if lit := picture.countLit(m.Vec2{X: 28.8, Y: 0}, m.Vec2{X: 28.8, Y: 17}); lit == 0 {
		t.Error("the full stop drew no pixel above the baseline, want it drawn")
	}
}

func TestAGlyphNoPageHoldsDrawsNothingReportedOnceAndTheAtlasDoesNotGrow(t *testing.T) {
	// A full block of a font of 100 texels to the em is a cell 152 texels
	// tall, taller than a page of 120 texels, which holds the small letter.
	config := &canvas.Config{AtlasPageSize: 120, DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{FieldEm: 100}}}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindAtlasFull)
	h.Spawn(nil, config)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(func(list canvas.DrawList) {
		list.DrawText(canvas.Text{String: fullBlock + "a"})
	})

	h.Step()

	frame := h.Frames()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 1 || batches[0].InstanceCount != 1 {
		t.Errorf("the run drew batches %+v, want one of the small letter alone", batches)
	}
	if count := countReports(h, canvas.ReportKindAtlasFull); count != 1 {
		t.Errorf("the glyph no page holds was reported %d times, want once", count)
	}
	// A growth makes the atlas a texture of its own.
	if glyphAtlasCount := len(findLoadedTextures(frame.Render, glyphAtlasLabel)); glyphAtlasCount != 1 {
		t.Errorf("the frame made %d glyph atlas textures, want one, never grown", glyphAtlasCount)
	}
}

// smallTextSize is the em, in world units, small-text tests draw at: 11 to
// 22 framebuffer pixels at 1 to 2 pixels a point, each more than twice
// smaller than the default field's 48 texels, so small text, and an advance
// of 0.6 em a fraction of a pixel past a whole one.
const smallTextSize = 11

// findColumnInk answers the ink, white over black, of each pixel column of
// picture from the one holding from to the one holding to, both included,
// each the sum of its pixels' red channels, a fully covered pixel 255, over
// the rows holding top to bottom, both included.
func (p windowPicture) findColumnInk(from, to m.Vec2, top, bottom float32) []int {
	start := p.pixel(m.Vec2{X: from.X, Y: top})
	end := p.pixel(m.Vec2{X: to.X, Y: bottom})
	var inks []int
	for x := start.X; x <= end.X; x++ {
		ink := 0
		for y := start.Y; y <= end.Y; y++ {
			ink += int(p.picture.NRGBAAt(x, y).R)
		}
		inks = append(inks, ink)
	}
	return inks
}

func TestAThinStrokeOfSmallTextDrawsItsWholeInkAtAnyPhaseOfThePixelGrid(t *testing.T) {
	// The light horizontal box line is a bar 0.1 em thick, about a pixel at
	// a small size. Drawn on whole pixels, and turned a hair, so it lands
	// unsnapped a fraction of a pixel lower each time, each column across
	// its middle holds a bar's thickness of ink, never losing it as a
	// single sample a pixel did.
	phases := []float32{0, 0.13, 0.37, 0.61, 0.89}
	findCentre := func(index int) m.Vec2 { return m.Vec2{X: -90 + 45*float32(index), Y: phases[index]} }

	picture := captureDrawing(t, func(list canvas.DrawList) {
		for index := range phases {
			angle := float32(0)
			if index > 0 {
				angle = 1e-4
			}
			list.DrawText(canvas.Text{String: "───", Size: smallTextSize, Transform: m.TRS2x3(findCentre(index), angle, m.Vec2{X: 1, Y: 1})})
		}
	})

	thickness := 0.1 * smallTextSize * picture.pixelsPerPoint
	for index := range phases {
		centre := findCentre(index)
		// The bar lies 0.37 em above the baseline, 0.01 em above the box's
		// middle. The columns read lie within the middle glyph's 6.6 units
		// at any scale from 1, its pen moved by half a pixel at most, short
		// of its neighbours' bars, which overlap its own where their pens
		// round towards it.
		inks := picture.findColumnInk(centre.Add(m.Vec2{X: -1.5}), centre.Add(m.Vec2{X: 1.5}), centre.Y-4, centre.Y+4)
		for column, ink := range inks {
			if got := float32(ink) / 255; got < thickness-0.25 || got > thickness+0.25 {
				t.Errorf("the bar at phase %v draws %.2f pixels of ink in column %d of its middle, want its thickness, %.2f, within 0.25", phases[index], got, column, thickness)
			}
		}
	}
}

func TestSmallTextsGlyphsLandOnWholePixelsSoEachDrawsAlike(t *testing.T) {
	// Six light vertical box lines, a stem 0.1 em wide in the middle of
	// each advance of 0.6 em, a fraction of a pixel past a whole one: each
	// pen lands on a whole pixel, so each stem draws the same columns of
	// ink, a whole number of pixels from the last.
	const stemCount = 6
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawText(canvas.Text{String: strings.Repeat("│", stemCount), Size: smallTextSize})
	})

	halfWidth := float32(0.3 * smallTextSize * stemCount)
	inks := picture.findColumnInk(m.Vec2{X: -halfWidth - 2}, m.Vec2{X: halfWidth + 2}, -0.5*smallTextSize, 0.5*smallTextSize)
	// Each stem is a run of columns holding ink, between columns holding
	// none.
	var stems [][]int
	for column, ink := range inks {
		switch {
		case ink == 0:
		case column > 0 && inks[column-1] != 0:
			stems[len(stems)-1] = append(stems[len(stems)-1], ink)
		default:
			stems = append(stems, []int{ink})
		}
	}
	if len(stems) != stemCount {
		t.Fatalf("the run drew %d stems, %v, want %d", len(stems), stems, stemCount)
	}
	for index, stem := range stems[1:] {
		if !slices.EqualFunc(stem, stems[0], func(a, b int) bool { return a-b <= 3 && b-a <= 3 }) {
			t.Errorf("stem %d draws columns of ink %v, want the first stem's, %v", index+1, stem, stems[0])
		}
	}
}

func TestSmallTextsGlyphsOfDifferentWidthsCentredOnOnePenStayCentredAlike(t *testing.T) {
	// The light vertical box line, the middle dot, the bullet and the double
	// vertical box line, each centred in its advance but of a different
	// width, so their fields begin a different number of texels from the
	// pen, are drawn one above another from one pen, at several fractions
	// of a pixel: their pens land on the same whole pixel, so they stay
	// centred on one column line, where placing each glyph's field on whole
	// pixels instead would part them.
	glyphs := []string{"│", "·", "•", "║"}
	shifts := []float32{0, 0.21, 0.43, 0.67, 0.89}
	findLeft := func(index int) float32 { return -120 + 50*float32(index) + shifts[index] }
	findTop := func(row int) float32 { return -100 + 40*float32(row) }

	picture := captureDrawing(t, func(list canvas.DrawList) {
		for index := range shifts {
			for row, glyph := range glyphs {
				list.DrawText(canvas.Text{String: glyph, Size: smallTextSize, Pivot: canvas.PivotTopLeft, Transform: m.Translation2x3(findLeft(index), findTop(row))})
			}
		}
	})

	advance := float32(0.6 * smallTextSize)
	for index, shift := range shifts {
		centres := make([]float32, len(glyphs))
		for row := range glyphs {
			left := findLeft(index)
			inks := picture.findColumnInk(m.Vec2{X: left - 2}, m.Vec2{X: left + advance + 2}, findTop(row), findTop(row)+1.32*smallTextSize)
			total := 0
			weighted := 0
			for column, ink := range inks {
				total += ink
				weighted += column * ink
			}
			if total == 0 {
				t.Fatalf("%s at shift %v drew no ink", glyphs[row], shift)
			}
			centres[row] = float32(weighted) / float32(total)
		}
		for row, centre := range centres[1:] {
			if difference := centre - centres[0]; difference < -0.15 || difference > 0.15 {
				t.Errorf("at shift %v %s is centred at column %.2f and %s at %.2f, want them within 0.15 pixels", shift, glyphs[row+1], centre, glyphs[0], centres[0])
			}
		}
	}
}

func TestATwelvePixelLabelIsCapturedForTheOwnerToJudge(t *testing.T) {
	// A pangram and the digits 12 framebuffer pixels to the em, in the
	// default font at its default field, white on black and black on white,
	// are written to the test's artifact directory, kept by go test
	// -artifacts, for eyes to judge whether small text on distance fields
	// reads well. A first capture of nothing, in a subtest whose kernel
	// quits before the second's starts, finds the window's pixels to a
	// point.
	var pixelsPerPoint float32
	t.Run("pixels per point", func(t *testing.T) {
		pixelsPerPoint = captureDrawing(t, func(canvas.DrawList) {}).pixelsPerPoint
	})
	size := 12 / pixelsPerPoint
	picture := captureDrawing(t, func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Rect: m.Rect{X: -160, Width: 320, Height: 120}, Tint: m.White})
		for index, tint := range []m.Color{m.White, m.Black} {
			list.DrawText(canvas.Text{
				String:    "The quick brown fox jumps over the lazy dog.\n0123456789 Sphinx of black quartz, judge my vow.",
				Size:      size,
				Tint:      tint,
				Transform: m.Translation2x3(-150, -60+120*float32(index)),
				Pivot:     canvas.Pivot{Fraction: m.Vec2{X: -0.5}},
				Order:     1,
			})
		}
	})

	// The first line's lower-case letters lie between its baseline, 0.36
	// em above the box's middle, and its x-height above that.
	firstLine := -60 - 0.6*size
	if lit := picture.countLit(m.Vec2{X: -150, Y: firstLine}, m.Vec2{X: 150, Y: firstLine}); lit == 0 {
		t.Error("the white label drew no pixel along its first line, want its glyphs")
	}
	path := filepath.Join(t.ArtifactDir(), "label12.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, picture.picture); err != nil {
		t.Fatal(err)
	}
	t.Logf("the 12-pixel labels are at %s", path)
}
