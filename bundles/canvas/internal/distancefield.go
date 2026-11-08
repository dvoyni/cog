package internal

import (
	"encoding/binary"
	"math"
)

// The halo's distance field: for every texel of a packed slot, how far it is
// to the nearest ink, in texels. The halo material reads it once per pixel
// where it used to search a disc of up to 192 taps for the same answer - the
// band it paints is falloff(distance to ink / reach), and the falloff never
// rises with distance, so the nearest ink is the only ink that matters.
//
// It is computed once per insertion, on the CPU, over the same padded rectangle
// the colour slot uploads, and stored in a companion texture laid out exactly
// like the colour atlas. Same array, same layer, same texel: the shader samples
// it at the sprite's own uv and no per-sprite record has to say where it is.
// SpriteInstance stays frozen.

const (
	// distanceInkAlpha is the alpha, of 255, at and above which a texel is
	// ink. Half is the edge an anti-aliased stroke draws, and it is the
	// threshold distance-field text uses for the same reason.
	distanceInkAlpha = 128
	// distanceEdgeOffset moves each distance from the ink texel's centre out
	// to its edge, where the stroke a reader sees actually ends.
	distanceEdgeOffset = 0.5
	// distanceFar is what a texel with no ink anywhere in its slot stores: the
	// largest finite half float, which every reach divides into a value past
	// the end of the band.
	distanceFar = 65504
	// distanceTexelBytes is one texel of the companion texture: an IEEE half.
	distanceTexelBytes = 2
	// distanceInfinity seeds the transform for a texel that is not ink. It only
	// has to exceed every squared distance a page can hold, 2*4096^2, by enough
	// that adding one stays above them.
	distanceInfinity = 1e20
)

// distanceField computes the distance field of a padded RGBA slot and encodes
// it as the companion texture's bytes: one little-endian half float per
// texel, the distance in texels from that texel's centre to the nearest ink
// edge, zero on ink.
//
// The transform is the exact Euclidean one of Felzenszwalb and Huttenlocher,
// "Distance Transforms of Sampled Functions" (2012): a one-dimensional lower
// envelope of parabolas down every column, then along every row of the result.
// Linear in the texel count, so a 512-square sprite costs a few milliseconds
// at load and a glyph next to nothing.
func distanceField(rgba []byte, width, height int) []byte {
	out := make([]byte, width*height*distanceTexelBytes)
	if width <= 0 || height <= 0 {
		return out
	}
	squared := make([]float32, width*height)
	for i := range squared {
		if rgba[i*4+3] >= distanceInkAlpha {
			squared[i] = 0
		} else {
			squared[i] = distanceInfinity
		}
	}
	n := max(width, height)
	line := make([]float32, n)
	result := make([]float32, n)
	vertices := make([]int, n)
	bounds := make([]float32, n+1)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			line[y] = squared[y*width+x]
		}
		lowerEnvelope(line[:height], result[:height], vertices, bounds)
		for y := 0; y < height; y++ {
			squared[y*width+x] = result[y]
		}
	}
	for y := 0; y < height; y++ {
		row := squared[y*width : (y+1)*width]
		copy(line, row)
		lowerEnvelope(line[:width], row, vertices, bounds)
	}
	for i, value := range squared {
		distance := float32(distanceFar)
		if value == 0 {
			distance = 0
		} else if value < distanceInfinity {
			distance = float32(math.Sqrt(float64(value))) - distanceEdgeOffset
		}
		binary.LittleEndian.PutUint16(out[i*distanceTexelBytes:], halfBits(distance))
	}
	return out
}

// lowerEnvelope is the one-dimensional squared distance transform: result[q]
// is the minimum over p of (q-p)^2 + f[p]. vertices and bounds are scratch of
// at least len(f) and len(f)+1, passed in so a whole image reuses one set.
func lowerEnvelope(f, result []float32, vertices []int, bounds []float32) {
	n := len(f)
	// intersect is where the parabola rooted at q overtakes the one rooted at
	// p. It is worked in float64: f holds distanceInfinity beside small squares,
	// and in float32 the squares would vanish into it.
	intersect := func(q, p int) float32 {
		fq, fp := float64(f[q])+float64(q*q), float64(f[p])+float64(p*p)
		return float32((fq - fp) / float64(2*q-2*p))
	}
	k := 0
	vertices[0] = 0
	// The first bound is minus infinity, so no intersection is ever below it
	// and the walk back never passes the first parabola.
	bounds[0] = -math.MaxFloat32
	bounds[1] = math.MaxFloat32
	for q := 1; q < n; q++ {
		s := intersect(q, vertices[k])
		for s <= bounds[k] {
			k--
			s = intersect(q, vertices[k])
		}
		k++
		vertices[k] = q
		bounds[k] = s
		bounds[k+1] = math.MaxFloat32
	}
	k = 0
	for q := 0; q < n; q++ {
		for bounds[k+1] < float32(q) {
			k++
		}
		p := vertices[k]
		d := float32(q - p)
		result[q] = d*d + f[p]
	}
}

// halfBits encodes a non-negative float as an IEEE 754 half, rounding to
// nearest, and saturating at the largest finite half. Negative input is not a
// distance and encodes as zero.
func halfBits(value float32) uint16 {
	if !(value > 0) {
		return 0
	}
	if value >= distanceFar {
		return 0x7bff
	}
	bits := math.Float32bits(value)
	exponent := int(bits>>23&0xff) - 127 + 15
	mantissa := bits & 0x7fffff
	if exponent <= 0 {
		// Subnormal half: the implicit one becomes explicit and the value
		// shifts down into the ten mantissa bits.
		if exponent < -10 {
			return 0
		}
		mantissa |= 0x800000
		shift := uint(14 - exponent)
		half := uint16(mantissa >> shift)
		if mantissa>>(shift-1)&1 != 0 {
			half++
		}
		return half
	}
	half := uint16(exponent)<<10 | uint16(mantissa>>13)
	if mantissa&0x1000 != 0 {
		// Rounding up may carry into the exponent, which is the right answer:
		// the next representable value is the next binade's first.
		half++
	}
	return half
}
