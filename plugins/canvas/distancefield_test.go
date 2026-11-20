package canvas_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
)

// findFieldAlpha answers the alpha of a texel of DistanceField{Spread:
// spread}'s output at distance from the art's edge, positive outside it:
// ½ at the edge, falling to 0 spread texels outside it and rising to 1
// spread texels inside it.
func findFieldAlpha(distance float64, spread int) int {
	return int(math.Round(255 * min(max(0.5-distance/float64(2*spread), 0), 1)))
}

// newCoveragePicture answers a picture of width by height texels, white, each
// texel's alpha the fraction of 16 by 16 samples within it that isInside
// answers true of, a point of the picture.
func newCoveragePicture(width, height int, isInside func(x, y float64) bool) *image.NRGBA {
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			covered := 0
			for sampleY := range 16 {
				for sampleX := range 16 {
					if isInside(float64(x)+(float64(sampleX)+0.5)/16, float64(y)+(float64(sampleY)+0.5)/16) {
						covered++
					}
				}
			}
			picture.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(math.Round(255 * float64(covered) / 256))})
		}
	}
	return picture
}

// expectField fails t for each texel of field within probed,
// DistanceField{Spread: spread}'s output, that is not white, or whose alpha
// is more than 2 from that of the true signed distance trueDistance answers
// at the texel's centre, positive outside the art. It fails t at once when
// field's bounds are not wantBounds.
func expectField(t *testing.T, field image.Image, wantBounds, probed image.Rectangle, spread int, trueDistance func(x, y float64) float64) {
	t.Helper()
	if field.Bounds() != wantBounds {
		t.Fatalf("the field's bounds are %v, want %v", field.Bounds(), wantBounds)
	}
	worst := 0
	for y := probed.Min.Y; y < probed.Max.Y; y++ {
		for x := probed.Min.X; x < probed.Max.X; x++ {
			shown := color.NRGBAModel.Convert(field.At(x, y)).(color.NRGBA)
			if shown.R != 255 || shown.G != 255 || shown.B != 255 {
				t.Fatalf("the field's texel %d, %d is %v, want white", x, y, shown)
			}
			distance := trueDistance(float64(x)+0.5, float64(y)+0.5)
			want := findFieldAlpha(distance, spread)
			if off := int(shown.A) - want; off > 2 || off < -2 {
				t.Errorf("the field's texel %d, %d has alpha %d, want %d, of the distance %.3f, within 2", x, y, shown.A, want, distance)
			}
			worst = max(worst, findMagnitude(int(shown.A)-want))
		}
	}
	t.Logf("the field's alpha is at most %d from the true distance's", worst)
}

// findMagnitude answers x's magnitude.
func findMagnitude(x int) int {
	return max(x, -x)
}

func TestADistanceFieldOfAHardEdgedRectangleMatchesItsTrueDistance(t *testing.T) {
	// An opaque rectangle of 10 by 6 texels, at 5, 4 in a transparent
	// picture of 20 by 14, is baked with a spread of 8: the field grows the
	// picture by 8 on each side, and its distance is the rectangle's, its
	// corners' round.
	picture := image.NewNRGBA(image.Rect(0, 0, 20, 14))
	art := image.Rect(5, 4, 15, 10)
	for y := art.Min.Y; y < art.Max.Y; y++ {
		for x := art.Min.X; x < art.Max.X; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{R: 30, G: 60, B: 90, A: 255})
		}
	}

	field := canvas.DistanceField{Spread: 8}.Convert(picture)

	expectField(t, field, image.Rect(-8, -8, 28, 22), image.Rect(-8, -8, 28, 22), 8, func(x, y float64) float64 {
		return findRectDistance(x, y, art)
	})
}

// findRectDistance answers the signed distance from x, y to rect's edge,
// positive outside it.
func findRectDistance(x, y float64, rect image.Rectangle) float64 {
	centreX := float64(rect.Min.X+rect.Max.X) / 2
	centreY := float64(rect.Min.Y+rect.Max.Y) / 2
	outX := math.Abs(x-centreX) - float64(rect.Dx())/2
	outY := math.Abs(y-centreY) - float64(rect.Dy())/2
	return math.Hypot(max(outX, 0), max(outY, 0)) + min(max(outX, outY), 0)
}

func TestADistanceFieldOfAnAntiAliasedDiscMatchesItsTrueDistance(t *testing.T) {
	// A white disc of radius 9.3 texels, off the texel grid, its edge's
	// coverage taken from 16 by 16 samples a texel, in a picture of 32
	// square, cut out of a larger one so its bounds begin at 3, 2: the
	// field grows those bounds by its spread, 6, and finds the disc's edge
	// to within a fraction of a texel from its coverage.
	const radius, centreX, centreY = 9.3, 18.2, 17.7
	whole := newCoveragePicture(40, 40, func(x, y float64) bool { return math.Hypot(x-centreX, y-centreY) < radius })
	picture := whole.SubImage(image.Rect(3, 2, 35, 34))

	field := canvas.DistanceField{Spread: 6}.Convert(picture)

	expectField(t, field, image.Rect(-3, -4, 41, 40), image.Rect(-3, -4, 41, 40), 6, func(x, y float64) float64 {
		return math.Hypot(x-centreX, y-centreY) - radius
	})
}

func TestADistanceFieldOfASlantedAntiAliasedEdgeMatchesItsTrueDistance(t *testing.T) {
	// A straight edge 35° off the vertical, anti-aliased, its coverage
	// taken from 16 by 16 samples a texel, crosses a picture of 40 square,
	// the art below its line; with a spread of 4, a 255th of the field's
	// alpha is 1/32 of a texel. Where the edge cuts a texel's corner, its
	// coverage is no linear measure of its centre's distance, so the field
	// places it along the coverage's gradient. Each texel whose nearest
	// edge is the slanted one, a spread clear of the picture's sides,
	// matches the line's distance.
	const spread = 4
	normalX, normalY := math.Sincos(35 * math.Pi / 180)
	lineDistance := func(x, y float64) float64 { return 20 - (normalX*x + normalY*y) }
	picture := newCoveragePicture(40, 40, func(x, y float64) bool { return lineDistance(x, y) < 0 })

	field := canvas.DistanceField{Spread: spread}.Convert(picture)

	expectField(t, field, image.Rect(-spread, -spread, 40+spread, 40+spread), image.Rect(2*spread, 2*spread, 40-2*spread, 40-2*spread), spread, lineDistance)
}

func TestADistanceFieldOfNoSpreadSpreadsEightTexels(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 4, 4))

	field := canvas.DistanceField{}.Convert(picture)

	if want := image.Rect(-8, -8, 12, 12); field.Bounds() != want {
		t.Errorf("the field of no spread has the bounds %v, want %v, grown by 8", field.Bounds(), want)
	}
}
