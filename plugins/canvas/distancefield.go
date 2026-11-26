package canvas

import (
	"image"
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// defaultDistanceFieldSpread is the spread of a DistanceField whose Spread
// is zero or less, in texels.
const defaultDistanceFieldSpread = 8

// DistanceField is a Converter baking an image's alpha into a distance
// field, as an SDF material reads one: each texel's alpha holds its distance to
// the edge of the image's art, where its alpha crosses ½, its colour white,
// premultiplied, in its colour space's RGBA8 format, so the fields of one
// Spread share an atlas, and those of another draw from one of their own.
// The alpha is ½ on the edge, falling to 0 Spread texels outside it and
// rising to 1 Spread texels inside it. Its output is the image grown by
// Spread on each side, its bounds' Min Spread texels above and left of the
// image's, so it lines up with the image at any pivot and size, and its
// field reaches Spread texels beyond the art. An anti-aliased edge is found
// to within a fraction of a texel from its coverage.
type DistanceField struct {
	// Spread is how far the field reaches beyond the art and into it, in
	// the image's texels, zero or less meaning 8.
	Spread int
}

// Format answers space's RGBA8 format: RGBA8UnormSrgb for ColorSpaceSrgb
// and RGBA8Unorm for ColorSpaceLinear.
func (d DistanceField) Format(space ColorSpace) render.TextureFormat {
	return findRGBA8Format(space)
}

// Convert answers d's distance field of picture's alpha, white premultiplied
// in space's RGBA8 format, and its bounds.
func (d DistanceField) Convert(picture *image.NRGBA, space ColorSpace) (texels []byte, bounds image.Rectangle) {
	spread := d.Spread
	if spread <= 0 {
		spread = defaultDistanceFieldSpread
	}
	pictureBounds := picture.Bounds()
	bounds = pictureBounds.Inset(-spread)
	width := bounds.Dx()
	coverage := make([]float32, width*bounds.Dy())
	for y := pictureBounds.Min.Y; y < pictureBounds.Max.Y; y++ {
		for x := pictureBounds.Min.X; x < pictureBounds.Max.X; x++ {
			coverage[(y-bounds.Min.Y)*width+x-bounds.Min.X] = float32(picture.Pix[picture.PixOffset(x, y)+3]) / 255
		}
	}
	distances := measureEdgeDistances(coverage, width, bounds.Dy(), float32(spread))
	texels = make([]byte, 4*len(distances))
	for index, distance := range distances {
		alpha := uint8(255*m.Clamp01(0.5-distance/float32(2*spread)) + 0.5)
		// White premultiplied is the alpha itself in linear.
		white := alpha
		if space != ColorSpaceLinear {
			white = srgbWhiteOfAlpha[alpha]
		}
		texels[4*index] = white
		texels[4*index+1] = white
		texels[4*index+2] = white
		texels[4*index+3] = alpha
	}
	return texels, bounds
}

// srgbWhiteOfAlpha is white premultiplied by each alpha, encoded in sRGB.
var srgbWhiteOfAlpha = func() (whites [256]uint8) {
	for alpha := range whites {
		value := float32(alpha) / 255
		whites[alpha], _, _, _ = m.NewColorLinear(value, value, value, value).Srgb8()
	}
	return whites
}()

// measureEdgeDistances answers the signed distance, in texels, from the
// centre of each texel of a grid width by height, coverage holding each
// texel's fraction inside the art row by row, to the art's edge, where the
// coverage crosses ½: positive outside the art and negative inside it, its
// magnitude no more than distanceLimit, which a texel farther from the edge, or of
// a grid holding no edge, answers. It is canvas's one distance transform,
// after Gustavson and Strand's anti-aliased Euclidean distance transform: a
// texel wholly outside measures to the part of the edge the nearest texel
// covered at all holds, which that texel's coverage places across its
// coverage's gradient; a texel wholly inside does the same towards the
// texels not wholly covered; and a texel the edge crosses measures to it
// along its coverage's gradient.
func measureEdgeDistances(coverage []float32, width, height int, distanceLimit float32) []float32 {
	// The gradient of the coverage, by Sobel's kernel, points into the art.
	gradients := make([]m.Vec2, len(coverage))
	for index, covered := range coverage {
		if covered <= 0 || covered >= 1 {
			continue
		}
		x := index % width
		y := index / width
		coverageAt := func(offsetX, offsetY int) float32 {
			neighbourX := min(max(x+offsetX, 0), width-1)
			neighbourY := min(max(y+offsetY, 0), height-1)
			return coverage[neighbourY*width+neighbourX]
		}
		gradients[index] = m.Vec2{
			X: coverageAt(1, -1) + 2*coverageAt(1, 0) + coverageAt(1, 1) - coverageAt(-1, -1) - 2*coverageAt(-1, 0) - coverageAt(-1, 1),
			Y: coverageAt(-1, 1) + 2*coverageAt(0, 1) + coverageAt(1, 1) - coverageAt(-1, -1) - 2*coverageAt(0, -1) - coverageAt(1, -1),
		}
	}
	outsideDistances := measureOutsideDistances(coverage, gradients, width, height, false, distanceLimit)
	insideDistances := measureOutsideDistances(coverage, gradients, width, height, true, distanceLimit)
	distances := make([]float32, len(coverage))
	for index, covered := range coverage {
		switch {
		case covered <= 0:
			distances[index] = outsideDistances[index]
		case covered >= 1:
			distances[index] = -insideDistances[index]
		default:
			distances[index] = findEdgeOffset(gradients[index], covered)
		}
	}
	return distances
}

// measureOutsideDistances answers, for each texel of a grid width by height
// wholly outside a region, the distance from its centre to the region's
// edge, no more than distanceLimit, and zero for the others: the region is the art
// that coverage and its gradients describe, or, where inside is set, what
// lies outside it. The distance is the least, over the texels the region
// covers at all, its seeds, of the distance to the part of its edge a seed
// holds. Each seed starts as its own, and sweeps down and up the grid,
// each texel taking a neighbour's seed where its edge lies nearer and
// within distanceLimit, until none does, as dead reckoning propagates it, measuring
// to the seed's edge rather than its centre, since a seed whose centre is
// nearer may hold its edge farther.
func measureOutsideDistances(coverage []float32, gradients []m.Vec2, width, height int, inside bool, distanceLimit float32) []float32 {
	seeds := make([]int32, len(coverage))
	distances := make([]float32, len(coverage))
	for index, covered := range coverage {
		if inside {
			covered = 1 - covered
		}
		seeds[index] = int32(index)
		if covered <= 0 {
			seeds[index] = -1
			distances[index] = distanceLimit
		}
	}
	// A sweep down the grid takes seeds from the neighbours each texel
	// follows, and one up from those opposite.
	neighbourOffsets := [4][2]int{{-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	for changed := true; changed; {
		changed = false
		for _, direction := range [2]int{1, -1} {
			for step := range seeds {
				index := step
				if direction < 0 {
					index = len(seeds) - 1 - step
				}
				if seeds[index] == int32(index) {
					continue
				}
				x := index % width
				y := index / width
				for _, offset := range neighbourOffsets {
					neighbourX := x + direction*offset[0]
					neighbourY := y + direction*offset[1]
					if neighbourX < 0 || neighbourX >= width || neighbourY < 0 || neighbourY >= height {
						continue
					}
					seed := seeds[neighbourY*width+neighbourX]
					if seed < 0 || seed == seeds[index] {
						continue
					}
					covered := coverage[seed]
					gradient := gradients[seed]
					if inside {
						covered = 1 - covered
						gradient = gradient.Negate()
					}
					seedOffset := m.Vec2{X: float32(x - int(seed)%width), Y: float32(y - int(seed)/width)}
					if distance := measureToTexelEdge(seedOffset, covered, gradient); distance < distances[index] {
						seeds[index] = seed
						distances[index] = distance
						changed = true
					}
				}
			}
		}
	}
	return distances
}

// measureToTexelEdge answers the distance from a point, offset from a
// texel's centre, outside the region that covers covered of the texel, to
// the part of the region's edge within the texel: a wholly covered texel's
// square, or the straight edge crossing it, its normal along gradient,
// which points into the region, placed by its coverage as findEdgeOffset
// places it, clipped to the texel. An edge of no gradient is placed along
// the direction to the point instead, as Gustavson and Strand place it.
func measureToTexelEdge(offset m.Vec2, covered float32, gradient m.Vec2) float32 {
	if covered >= 1 {
		outside := offset.Abs().SubS(0.5)
		return m.Vec2{X: max(outside.X, 0), Y: max(outside.Y, 0)}.Length()
	}
	if gradient == (m.Vec2{}) {
		return offset.Length() + findEdgeOffset(offset, covered)
	}
	// normal points out of the region, and tangent along its edge, which
	// passes through edgeNearestCentre; the edge spans from tangentStart to
	// tangentEnd along the tangent within the texel, between where it
	// crosses each axis's two sides.
	normal := gradient.Normalize().Negate()
	tangent := m.Vec2{X: -normal.Y, Y: normal.X}
	edgeNearestCentre := normal.MulS(-findEdgeOffset(gradient, covered))
	tangentStart := float32(math.Inf(-1))
	tangentEnd := float32(math.Inf(1))
	for _, axisComponents := range [2][2]float32{{tangent.X, edgeNearestCentre.X}, {tangent.Y, edgeNearestCentre.Y}} {
		if axisComponents[0] == 0 {
			continue
		}
		lowSideCrossing := (-0.5 - axisComponents[1]) / axisComponents[0]
		highSideCrossing := (0.5 - axisComponents[1]) / axisComponents[0]
		tangentStart = max(tangentStart, min(lowSideCrossing, highSideCrossing))
		tangentEnd = min(tangentEnd, max(lowSideCrossing, highSideCrossing))
	}
	tangentPosition := offset.Dot(tangent)
	if tangentStart <= tangentEnd {
		tangentPosition = min(max(tangentPosition, tangentStart), tangentEnd)
	} else {
		tangentPosition = 0
	}
	return offset.Sub(edgeNearestCentre.Add(tangent.MulS(tangentPosition))).Length()
}

// findEdgeOffset answers the distance from a texel's centre to a straight
// edge crossing the texel, coverage of it inside the edge, whose normal
// lies along direction: positive where the centre lies outside the edge,
// its coverage under ½, as Gustavson and Strand's edge function answers. A
// wholly covered texel's edge is its side, or its corner, facing the
// direction, so a texel outside measures to the art's own texels.
func findEdgeOffset(direction m.Vec2, coverage float32) float32 {
	if direction.X == 0 || direction.Y == 0 {
		return 0.5 - coverage
	}
	// largerComponent and smallerComponent are the direction's, as a unit
	// vector, by magnitude.
	unitDirection := direction.Abs().Normalize()
	largerComponent := max(unitDirection.X, unitDirection.Y)
	smallerComponent := min(unitDirection.X, unitDirection.Y)
	// cornerCoverage is the coverage below which the edge cuts the texel's
	// corner alone, its area a triangle's.
	cornerCoverage := 0.5 * smallerComponent / largerComponent
	componentProduct := 2 * largerComponent * smallerComponent
	switch {
	case coverage < cornerCoverage:
		return 0.5*(largerComponent+smallerComponent) - float32(math.Sqrt(float64(componentProduct*coverage)))
	case coverage < 1-cornerCoverage:
		return (0.5 - coverage) * largerComponent
	default:
		return -0.5*(largerComponent+smallerComponent) + float32(math.Sqrt(float64(componentProduct*(1-coverage))))
	}
}
