package canvas_test

import (
	"image"
	"image/color"
	"math"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

// newRectArt answers a picture of 16 texels square, transparent but for an
// opaque white rect.
func newRectArt(rect image.Rectangle) *image.NRGBA {
	art := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			art.SetNRGBA(x, y, opaqueWhite)
		}
	}
	return art
}

// findProfileCoverage answers how much of a fragment profile's band covers,
// distance from the edge, positive outside it, changing by pixel over a
// pixel, both in the profile's units: canvas_sdf_fragment's coverage.
func findProfileCoverage(profile canvas.Profile, distance, pixel float64) float64 {
	ramp := func(amount float64) float64 { return min(max(amount/pixel+0.5, 0), 1) }
	inside := ramp(distance + min(float64(profile.Inner), 1e9))
	outer, plateau := float64(profile.Outer), float64(profile.Plateau)
	if profile.Falloff <= 0 {
		return inside * ramp(outer-distance)
	}
	fall := min(max((distance-plateau)/max(outer-plateau, pixel), 0), 1)
	return inside * math.Pow(max(1-fall, 1e-30), float64(profile.Falloff))
}

// findProfileShown answers how a fragment profile's band covers coverage of
// shows over opaque black in a window whose format encodes nothing: its
// colour, straight, encoded as sRGB, times its alpha and coverage, or, its
// alpha zero, added, its colour times its coverage encoded.
func findProfileShown(profile canvas.Profile, coverage float64) color.NRGBA {
	premultiplied := profile.Color
	toByte := func(value float32, factor float64) uint8 { return uint8(math.Round(255 * float64(value) * factor)) }
	if premultiplied.A == 0 {
		red, green, blue, _ := premultiplied.MulS(float32(coverage)).Srgb()
		return color.NRGBA{R: toByte(red, 1), G: toByte(green, 1), B: toByte(blue, 1), A: 255}
	}
	alpha := premultiplied.A
	red, green, blue, _ := m.Color{R: premultiplied.R / alpha, G: premultiplied.G / alpha, B: premultiplied.B / alpha, A: 1}.Srgb()
	factor := float64(alpha) * coverage
	return color.NRGBA{R: toByte(red, factor), G: toByte(green, factor), B: toByte(blue, factor), A: 255}
}

// profileProbe is a point of the world at which a pixel test expects the
// band of profile, its distance found by distanceAt at a point of the world.
type profileProbe struct {
	name       string
	point      m.Vec2
	profile    canvas.Profile
	distanceAt func(point m.Vec2) float64
}

// expectProfiles fails t for each of probes whose pixel shows other than its
// profile's band, within 2 of each channel, at the distance its pixel's
// centre lies at, the band's pixel the distance's change to the next pixel
// across and down.
func (p windowPicture) expectProfiles(t *testing.T, probes []profileProbe) {
	t.Helper()
	for _, probe := range probes {
		centre := p.findPixelCentre(probe.point)
		step := 1 / p.pixelsPerPoint
		distance := probe.distanceAt(centre)
		pixel := math.Abs(probe.distanceAt(centre.Add(m.Vec2{X: step}))-distance) + math.Abs(probe.distanceAt(centre.Add(m.Vec2{Y: step}))-distance)
		want := findProfileShown(probe.profile, findProfileCoverage(probe.profile, distance, max(pixel, 1e-6)))
		got := p.at(probe.point)
		if findMagnitude(int(got.R)-int(want.R)) > 2 || findMagnitude(int(got.G)-int(want.G)) > 2 || findMagnitude(int(got.B)-int(want.B)) > 2 {
			t.Errorf("%s, at %v, %.2f from the edge, is %v, want %v within 2", probe.name, probe.point, distance, got, want)
		}
	}
}

// findPixelCentre answers the centre, a point of the world, of the pixel
// holding point.
func (p windowPicture) findPixelCentre(point m.Vec2) m.Vec2 {
	pixel := p.pixel(point)
	return m.Vec2{X: (float32(pixel.X)+0.5)/p.pixelsPerPoint - pictureWidth/2, Y: (float32(pixel.Y)+0.5)/p.pixelsPerPoint - pictureHeight/2}
}

// findRoundedRectDistance answers point's signed distance from rect, its
// corners rounded by radius, negative inside: canvas_rounded_distance's.
func findRoundedRectDistance(point m.Vec2, rect m.Rect, radius float64) float64 {
	centre := rect.Center()
	cornerX := math.Abs(float64(point.X-centre.X)) - float64(rect.Width)/2 + radius
	cornerY := math.Abs(float64(point.Y-centre.Y)) - float64(rect.Height)/2 + radius
	return math.Hypot(max(cornerX, 0), max(cornerY, 0)) + min(max(cornerX, cornerY), 0) - radius
}

// findStoredFieldDistance answers the distance the field DistanceField bakes
// of rect, art of full alpha, with spread holds at texel, a point of the
// field in its texels, as a sampler filtering it linearly reads it: rect's
// own distance at the four texel centres around texel, each rounded to the
// byte of alpha the texel stores, blended by texel's place between them.
func findStoredFieldDistance(texel m.Vec2, rect image.Rectangle, spread int) float64 {
	findStoredDistance := func(x, y float64) float64 {
		return (0.5 - float64(findFieldAlpha(findRectDistance(x, y, rect), spread))/255) * 2 * float64(spread)
	}
	left := math.Floor(float64(texel.X) - 0.5)
	top := math.Floor(float64(texel.Y) - 0.5)
	across := float64(texel.X) - 0.5 - left
	down := float64(texel.Y) - 0.5 - top
	upper := (1-across)*findStoredDistance(left+0.5, top+0.5) + across*findStoredDistance(left+1.5, top+0.5)
	lower := (1-across)*findStoredDistance(left+0.5, top+1.5) + across*findStoredDistance(left+1.5, top+1.5)
	return (1-down)*upper + down*lower
}

// namedProfile is a profile a pixel test draws, named for its messages.
type namedProfile struct {
	name    string
	profile canvas.Profile
}

// sdfSpread is the spread the SDF tests bake their fields with, and
// sdfScale the world units a field's texel spans, its frame of 16 texels
// drawn 48 units wide.
const (
	sdfSpread = 4
	sdfScale  = 3
)

func TestEachProfileDrawsItsBandAroundASpritesDistanceField(t *testing.T) {
	// The art's field is drawn with each profile, three times its size
	// about its centre, read along its rectangle's middle row, a row of
	// texel centres, out of its right edge: deep inside, inside, on the
	// edge, through each band and past it. Its texels are filtered
	// linearly, so the field's distance between texel centres is its
	// straight edge's, as each texel stores it, rounded to a byte, its alpha
	// clamping it within the spread.
	files := fstest.MapFS{}
	addImageFile(t, files, fieldArtPath, newRectArt(fieldArt))
	field := canvas.ImageFromFile(fieldArtPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: sdfSpread}})
	profiles := []namedProfile{
		{"an outline", canvas.Outline(1.5, orange)},
		{"a halo", canvas.Halo(1, 3, orange)},
		{"a glow", canvas.Glow(3, orange)},
		{"a drop shadow", canvas.DropShadow(m.Vec2{X: 2 * sdfScale}, 1, orange)},
	}
	for index := range profiles {
		profiles[index].profile.Spread = sdfSpread
	}
	centreX := func(index int) float32 { return -120 + 80*float32(index) }

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.CommandList) {
		for index, profile := range profiles {
			sprite := canvas.Sprite{Image: field, Material: canvas.SDFMaterial, Transform: m.Translation2x3(centreX(index), 0), Size: m.Vec2{X: 16 * sdfScale}}
			list.DrawSpriteWith(sprite, profile.profile)
		}
	}, &fileMounter{files: files})

	var probes []profileProbe
	for index, profile := range profiles {
		// The frame's top left, and the art's right edge on the middle row.
		frameOrigin := m.Vec2{X: centreX(index) - 8*sdfScale, Y: -8 * sdfScale}
		edge := m.Vec2{X: frameOrigin.X + float32(fieldArt.Max.X)*sdfScale, Y: frameOrigin.Y + 7.5*sdfScale}
		offset := profile.profile.Offset
		distanceAt := func(point m.Vec2) float64 {
			return findStoredFieldDistance(point.Sub(offset).Sub(frameOrigin).DivS(sdfScale), fieldArt, sdfSpread)
		}
		for _, distance := range []float32{-3, -1.4, 0.1, 0.6, 1.1, 1.6, 2.1, 2.6, 3.3, 3.6} {
			probes = append(probes, profileProbe{name: profile.name, point: edge.Add(m.Vec2{X: distance * sdfScale}), profile: profile.profile, distanceAt: distanceAt})
		}
	}
	picture.expectProfiles(t, probes)
}

func TestEachProfileDrawsItsBandAroundARoundedFillItsQuadGrownToHoldIt(t *testing.T) {
	// A fill 40 units square, its corners rounded by 10, is drawn with each
	// profile, read out of its right side and along its top-right corner's
	// diagonal, its distance its rounded rectangle's, past the fill's own
	// quad, which its band grows.
	profiles := []namedProfile{
		{"an outline", canvas.Outline(4, orange)},
		{"a halo", canvas.Halo(3, 9, orange)},
		{"a glow", canvas.Glow(9, orange)},
		{"a drop shadow", canvas.DropShadow(m.Vec2{X: 6}, 3, orange)},
	}
	const radius = 10
	findRect := func(index int) m.Rect { return m.Rect{X: -140 + 80*float32(index), Y: -20, Width: 40, Height: 40} }

	picture := captureDrawing(t, func(list canvas.CommandList) {
		for index, profile := range profiles {
			list.DrawShapeWith(canvas.Shape{Kind: canvas.Fill, Rect: findRect(index), Radius: radius, Tint: m.White, Material: canvas.SDFMaterial}, profile.profile)
		}
	})

	var probes []profileProbe
	for index, profile := range profiles {
		rect := findRect(index)
		offset := profile.profile.Offset
		distanceAt := func(point m.Vec2) float64 { return findRoundedRectDistance(point.Sub(offset), rect, radius) }
		side := m.Vec2{X: rect.X + rect.Width, Y: rect.Y + rect.Height/2}
		for _, distance := range []float32{-6.5, -1.5, 0.5, 1.5, 2.5, 3.5, 4.5, 5.5, 7.5, 9.5, 11.5} {
			probes = append(probes, profileProbe{name: profile.name + " by its side", point: side.Add(m.Vec2{X: distance}), profile: profile.profile, distanceAt: distanceAt})
		}
		// The corner's curve's centre, then points out along its diagonal.
		cornerCentre := m.Vec2{X: rect.X + rect.Width - radius, Y: rect.Y + radius}
		for _, along := range []float32{5.5, 7.5, 9.5, 11.5, 13.5} {
			probes = append(probes, profileProbe{name: profile.name + " by its corner", point: cornerCentre.Add(m.Vec2{X: along, Y: -along}), profile: profile.profile, distanceAt: distanceAt})
		}
	}
	picture.expectProfiles(t, probes)
}

func TestADropShadowFallsTheSameWayInTheWorldUnderARotatedOrMirroredTransform(t *testing.T) {
	// A field of a centred square, and a fill of the same square, 24 units
	// across, each look the same under a quarter turn and a mirroring,
	// each drawn with a shadow offset 6 right and 9 down in the world. Its
	// shadow lies right and below it whatever the transform, so it shows
	// past the square's bottom-right corner and not past its bottom-left.
	const squareArtPath = "squareart.png"
	files := fstest.MapFS{}
	addImageFile(t, files, squareArtPath, newRectArt(image.Rect(4, 4, 12, 12)))
	field := canvas.ImageFromFile(squareArtPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: sdfSpread}})
	shadow := canvas.DropShadow(m.Vec2{X: 6, Y: 9}, 1, orange)
	shadow.Spread = sdfSpread
	transforms := []struct {
		name  string
		angle float32
		scale m.Vec2
	}{
		{"unturned", 0, m.Vec2{X: 1, Y: 1}},
		{"turned a quarter", math.Pi / 2, m.Vec2{X: 1, Y: 1}},
		{"mirrored", 0, m.Vec2{X: -1, Y: 1}},
	}
	findCentre := func(index int, row float32) m.Vec2 { return m.Vec2{X: -100 + 100*float32(index), Y: row} }
	const spriteRow, fillRow = -60, 60

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.CommandList) {
		for index, transform := range transforms {
			sprite := canvas.Sprite{Image: field, Material: canvas.SDFMaterial, Transform: m.TRS2x3(findCentre(index, spriteRow), transform.angle, transform.scale), Size: m.Vec2{X: 16 * sdfScale}}
			list.DrawSpriteWith(sprite, shadow)
			fill := canvas.Shape{Kind: canvas.Fill, Rect: m.Rect{X: -12, Y: -12, Width: 24, Height: 24}, Transform: m.TRS2x3(findCentre(index, fillRow), transform.angle, transform.scale), Tint: m.White, Material: canvas.SDFMaterial}
			list.DrawShapeWith(fill, shadow)
		}
	}, &fileMounter{files: files})

	for index, transform := range transforms {
		for _, row := range []struct {
			name string
			y    float32
		}{{"sprite", spriteRow}, {"fill", fillRow}} {
			centre := findCentre(index, row.y)
			picture.expect(t, []probe{
				{transform.name + " " + row.name + "'s shadow past its bottom-right corner", centre.Add(m.Vec2{X: 15, Y: 15}), orangeShown},
				{transform.name + " " + row.name + "'s shadow past its bottom side", centre.Add(m.Vec2{Y: 15}), orangeShown},
				{transform.name + " " + row.name + " past its bottom-left corner", centre.Add(m.Vec2{X: -15, Y: 15}), opaqueBlack},
				{transform.name + " " + row.name + " past its top-right corner", centre.Add(m.Vec2{X: 15, Y: -15}), opaqueBlack},
			})
		}
	}
}

func TestAShapesBandKeepsItDrawnWhileItLiesOutsideTheViewByLessThanItsReach(t *testing.T) {
	// Two fills lie 5 units past the right of the view, 32 units from the
	// centre: the one glowing 8 units around is drawn, and the one of the
	// default material culled.
	recorder := &materialRecorder{record: func(list canvas.CommandList, _ canvas.Assets, _ []canvas.Material, _ int) {
		list.DrawShapeWith(canvas.Shape{Rect: m.Rect{X: 37, Width: 4, Height: 4}, Tint: m.White, Material: canvas.SDFMaterial}, canvas.Glow(8, orange))
		list.DrawShape(canvas.Shape{Rect: m.Rect{X: 37, Width: 4, Height: 4}, Order: 1})
	}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	batches := h.Frames()[0].Cameras[0].Batches
	if len(batches) != 1 || batches[0].Material != canvas.SDFMaterial {
		t.Errorf("the fills past the view drew as batches %+v, want one, of SDFMaterial", batches)
	}
}

func TestReleasingSDFMaterialOrSettingItsParametersIsRefusedReportedOnceAndItStillDraws(t *testing.T) {
	// The first frame releases SDFMaterial and sets a uniform on it, and
	// both frames draw a fill of it.
	recorder := &materialRecorder{record: func(list canvas.CommandList, assets canvas.Assets, _ []canvas.Material, frame int) {
		if frame == 0 {
			assets.ReleaseMaterial(canvas.SDFMaterial)
			assets.SetMaterialUniform[float32](canvas.SDFMaterial, "anything", 1)
		}
		list.DrawShapeWith(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}, Tint: m.White, Material: canvas.SDFMaterial}, canvas.Outline(1, orange))
	}}
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMaterialNotHeld)
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	for index, frame := range h.Frames() {
		if materials := findBatchMaterials(frame.Cameras[0]); len(materials) != 1 || materials[0] != canvas.SDFMaterial {
			t.Errorf("frame %d drew batches of materials %v, want one of SDFMaterial", index, materials)
		}
	}
	if count := countReports(h, canvas.ReportKindMaterialNotHeld); count != 1 {
		t.Errorf("releasing SDFMaterial and setting its uniform reported %d refusals, want one, for the material", count)
	}
}
