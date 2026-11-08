package internal

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// decodeHalf is the reference reading of an IEEE half, for checking halfBits
// and reading distanceField's output back.
func decodeHalf(bits uint16) float32 {
	exponent := int(bits >> 10 & 0x1f)
	mantissa := float64(bits & 0x3ff)
	switch exponent {
	case 0:
		return float32(mantissa / 1024 * math.Pow(2, -14))
	case 31:
		return float32(math.Inf(1))
	default:
		return float32((1 + mantissa/1024) * math.Pow(2, float64(exponent-15)))
	}
}

func readField(t *testing.T, field []byte, width, height int) []float32 {
	t.Helper()
	if len(field) != width*height*distanceTexelBytes {
		t.Fatalf("field is %d bytes, want %d", len(field), width*height*distanceTexelBytes)
	}
	values := make([]float32, width*height)
	for i := range values {
		values[i] = decodeHalf(binary.LittleEndian.Uint16(field[i*distanceTexelBytes:]))
	}
	return values
}

func inkImage(width, height int, ink func(x, y int) bool) []byte {
	rgba := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if ink(x, y) {
				rgba[(y*width+x)*4+3] = 255
			}
		}
	}
	return rgba
}

// TestDistanceFieldMatchesBruteForce checks the separable transform against
// the definition, texel by texel: the distance from each texel's centre to the
// nearest ink texel's centre, less half a texel, and zero on ink.
func TestDistanceFieldMatchesBruteForce(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	for trial := 0; trial < 20; trial++ {
		width, height := 1+random.Intn(40), 1+random.Intn(40)
		density := random.Float64() * 0.1
		ink := make([]bool, width*height)
		for i := range ink {
			ink[i] = random.Float64() < density
		}
		field := readField(t, distanceField(inkImage(width, height, func(x, y int) bool { return ink[y*width+x] }), width, height), width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				best := math.Inf(1)
				for v := 0; v < height; v++ {
					for u := 0; u < width; u++ {
						if ink[v*width+u] {
							best = min(best, math.Hypot(float64(x-u), float64(y-v)))
						}
					}
				}
				want := float64(distanceFar)
				if best == 0 {
					want = 0
				} else if !math.IsInf(best, 1) {
					want = best - distanceEdgeOffset
				}
				got := float64(field[y*width+x])
				// A half keeps eleven significant bits.
				if math.Abs(got-want) > want/1024+1e-6 {
					t.Fatalf("trial %d, %dx%d, texel (%d,%d): distance %v, want %v", trial, width, height, x, y, got, want)
				}
			}
		}
	}
}

// TestDistanceFieldThresholdsAtHalfAlpha checks that ink is alpha at or above
// half, so an anti-aliased fringe below it is measured to rather than from.
func TestDistanceFieldThresholdsAtHalfAlpha(t *testing.T) {
	rgba := make([]byte, 3*1*4)
	rgba[0*4+3] = distanceInkAlpha
	rgba[1*4+3] = distanceInkAlpha - 1
	field := readField(t, distanceField(rgba, 3, 1), 3, 1)
	if field[0] != 0 || field[1] != 0.5 || field[2] != 1.5 {
		t.Fatalf("field %v, want [0 0.5 1.5]", field)
	}
}

// TestDistanceFieldWithoutInkIsFar checks that a slot with no ink stores the
// far value everywhere, which no reach brings inside a band.
func TestDistanceFieldWithoutInkIsFar(t *testing.T) {
	field := readField(t, distanceField(make([]byte, 5*4*4), 5, 4), 5, 4)
	for i, value := range field {
		if value != distanceFar {
			t.Fatalf("texel %d is %v, want %v", i, value, float32(distanceFar))
		}
	}
}

// TestHalfBitsRoundTrips checks the encoder against the reference decoder
// across normal, subnormal and saturated values.
func TestHalfBitsRoundTrips(t *testing.T) {
	for _, value := range []float32{0, 0.5, 1, 1.5, 2.25, 35.25, 1000, 4094, distanceFar} {
		if got := decodeHalf(halfBits(value)); got != value {
			t.Errorf("halfBits(%v) decodes to %v", value, got)
		}
	}
	// A subnormal half has a fixed step of 2^-24, so it is checked absolutely.
	if got := decodeHalf(halfBits(1e-5)); math.Abs(float64(got)-1e-5) > math.Pow(2, -25) {
		t.Errorf("halfBits(1e-5) decodes to %v", got)
	}
	for _, value := range []float32{0.1, 0.3, 3.14159, 17.3, 123.456, 2047.9, 60000} {
		got := decodeHalf(halfBits(value))
		if relative := math.Abs(float64(got-value)) / float64(value); relative > 1.0/2048 {
			t.Errorf("halfBits(%v) decodes to %v, off by %v", value, got, relative)
		}
	}
	if got := decodeHalf(halfBits(1e9)); got != distanceFar {
		t.Errorf("halfBits(1e9) decodes to %v, want saturation at %v", got, float32(distanceFar))
	}
	if halfBits(-1) != 0 || halfBits(float32(math.NaN())) != 0 {
		t.Errorf("negative and NaN must encode as zero")
	}
}

// TestEveryAtlasSlotUploadsItsDistanceField checks the companion is laid out
// exactly as the colour atlas is: one R16F array beside every colour array,
// sized alike, and one upload beside every colour upload, at the same layer and
// region, two bytes a texel - which is what lets the halo sample it at the
// sprite's own uv. Releasing the arrays releases both.
func TestEveryAtlasSlotUploadsItsDistanceField(t *testing.T) {
	filesystem := fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 3)}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * atlasTexelBytes * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 3}}, nil)
	})
	runFrame(k)

	if len(backend.allocations) == 0 || len(backend.distance.allocations) != len(backend.allocations) {
		t.Fatalf("distance arrays = %d beside %d colour arrays, want one each",
			len(backend.distance.allocations), len(backend.allocations))
	}
	for i, colour := range backend.allocations {
		distance := backend.distance.allocations[i].desc
		if distance.Format != gfx.FormatR16F || distance.Mipmaps ||
			distance.Width != colour.desc.Width || distance.Height != colour.desc.Height || distance.Layers != colour.desc.Layers {
			t.Fatalf("distance array %+v beside colour array %+v, want R16F of the same size with no mips", distance, colour.desc)
		}
	}
	if len(backend.distance.updates) != len(backend.updates) {
		t.Fatalf("distance uploads = %d beside %d colour uploads, want one each",
			len(backend.distance.updates), len(backend.updates))
	}
	for i, colour := range backend.updates {
		distance := backend.distance.updates[i]
		if distance.layer != colour.layer || distance.region != colour.region {
			t.Fatalf("distance upload at layer %d %+v, colour at layer %d %+v; want the same slot",
				distance.layer, distance.region, colour.layer, colour.region)
		}
		if want := colour.region.Width * colour.region.Height * distanceTexelBytes; len(distance.pixels) != want {
			t.Fatalf("distance upload is %d bytes, want %d", len(distance.pixels), want)
		}
	}
	// The sprite is opaque and extruded into its gutter, so its whole slot is
	// ink and every distance zero.
	sprite := backend.distance.updates[len(backend.distance.updates)-1]
	for i, value := range readField(t, sprite.pixels, sprite.region.Width, sprite.region.Height) {
		if value != 0 {
			t.Fatalf("texel %d of an opaque sprite's slot is %v from ink, want 0", i, value)
		}
	}

	probeLookupDevice(k, func(la LookupDeviceAccess) { la.UnloadAll() })
	runFrame(k)
	if len(backend.releasedTextures) == 0 || len(backend.distance.releases) != len(backend.releasedTextures) {
		t.Fatalf("distance releases = %d beside %d colour releases, want one each",
			len(backend.distance.releases), len(backend.releasedTextures))
	}
}

// TestTheHaloBindsTheDistanceFieldOfItsAtlas checks every halo draw binds
// canvasHaloDistance, and binds the companion of the array the draw samples.
func TestTheHaloBindsTheDistanceFieldOfItsAtlas(t *testing.T) {
	filesystem := fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 3)}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * atlasTexelBytes * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 3}}, nil)
		write.SetLayerMaterial(0, HaloMaterialSet(DefaultHaloProfile()))
	})
	runFrame(k)

	if backend.draws == 0 || len(backend.distance.bound) != backend.draws {
		t.Fatalf("distance bindings = %d over %d draws, want one a draw", len(backend.distance.bound), backend.draws)
	}
	companion := map[gfx.TextureID]gfx.TextureID{}
	for i, colour := range backend.allocations {
		companion[colour.id] = backend.distance.allocations[i].id
	}
	for i, bound := range backend.distance.bound {
		if want := companion[backend.textures[i].id]; bound.id != want {
			t.Fatalf("draw %d samples array %d and binds distance %d, want its companion %d",
				bound.draw, backend.textures[i].id, bound.id, want)
		}
	}
}
