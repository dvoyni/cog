package internal

import (
	"errors"
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// The light record is 48 bytes with no kind field, and the frame block places
// the count and the array where the shader reads them: the count right after
// ambientGround, the array at the next 16-byte boundary.
func TestLightRecordIsFortyEightBytesAndTheFrameBlockHoldsSixteen(t *testing.T) {
	if size := unsafe.Sizeof(Light{}); size != 48 {
		t.Fatalf("Light is %d bytes, want 48", size)
	}
	var block FrameBlock
	if offset := unsafe.Offsetof(block.LightCount); offset != 288 {
		t.Errorf("lightCount is at offset %d, want 288", offset)
	}
	if offset := unsafe.Offsetof(block.Lights); offset != 304 {
		t.Errorf("lights is at offset %d, want 304", offset)
	}
	if len(block.Lights) != MaxLights || MaxLights != 16 {
		t.Errorf("the block holds %d lights, want the fixed 16", len(block.Lights))
	}
	if size := unsafe.Sizeof(block); size != 304+16*48 {
		t.Errorf("the frame block is %d bytes, want %d", size, 304+16*48)
	}
}

// A point light packs its direction as an actual zero vector, spotScale 0 and
// spotOffset 1, so the shader's cone term is saturate(0 + 1) with no NaN
// anywhere: x * 0 == 0 is false for NaN.
func TestAPointLightPacksAZeroDirectionAndAUnitCone(t *testing.T) {
	light, err := PackLight(LightDescr{
		Position: m.Vec3{X: 1, Y: 2, Z: 3},
		// A hand-written point light may carry junk in the spot fields; none
		// of it reaches the record.
		Direction: m.Vec3{X: 5}, InnerCone: 1, OuterCone: 0.5,
		Color: m.NewColorLinear(1, 0.5, 0.25, 1),
		Kind:  LightPoint,
	})
	if err != nil {
		t.Fatalf("packing a point light reported %v", err)
	}
	if light.Position != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Errorf("position is %v", light.Position)
	}
	if light.Direction != (m.Vec3{}) {
		t.Errorf("direction is %v, want an actual zero vector", light.Direction)
	}
	if light.SpotScale != 0 || light.SpotOffset != 1 {
		t.Errorf("cone is scale %v offset %v, want 0 and 1", light.SpotScale, light.SpotOffset)
	}
	if light.Color != (m.Vec3{X: 1, Y: 0.5, Z: 0.25}) {
		t.Errorf("color is %v, want the colour times an intensity of 1", light.Color)
	}
	if light.InvRange4 != 0 {
		t.Errorf("invRange4 is %v, want 0 for an infinite range", light.InvRange4)
	}
}

// Intensity premultiplies into the colour, and Range packs as 1/range^4.
func TestALightPremultipliesIntensityAndStoresInverseRangeToTheFourth(t *testing.T) {
	light, err := PackLight(LightDescr{
		Color: m.NewColorLinear(1, 1, 1, 1), Intensity: 3, Range: 2, Kind: LightPoint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if light.Color != (m.Vec3{X: 3, Y: 3, Z: 3}) {
		t.Errorf("color is %v, want the colour times 3", light.Color)
	}
	if !near(light.InvRange4, 1.0/16) {
		t.Errorf("invRange4 is %v, want 1/2^4", light.InvRange4)
	}
}

// A spot light packs a normalised direction and the CPU-side cone terms, so
// the shader is one dot, one MAD and one saturate.
func TestASpotLightPacksTheConeAsScaleAndOffset(t *testing.T) {
	inner, outer := float32(0.3), float32(0.6)
	light, err := PackLight(LightDescr{
		Direction: m.Vec3{Y: -2}, InnerCone: inner, OuterCone: outer,
		Color: m.NewColorLinear(1, 1, 1, 1), Kind: LightSpot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if light.Direction != (m.Vec3{Y: -1}) {
		t.Errorf("direction is %v, want the normalised direction of travel", light.Direction)
	}
	cosInner := float32(math.Cos(float64(inner)))
	cosOuter := float32(math.Cos(float64(outer)))
	scale := 1 / (cosInner - cosOuter)
	if !near(light.SpotScale, scale) || !near(light.SpotOffset, -cosOuter*scale) {
		t.Errorf("cone is scale %v offset %v, want %v and %v",
			light.SpotScale, light.SpotOffset, scale, -cosOuter*scale)
	}
	// At the axis the cone is fully on, at the outer edge fully off.
	if on := light.SpotScale*1 + light.SpotOffset; on < 1 {
		t.Errorf("the cone evaluates to %v on its axis, want at least 1", on)
	}
	if off := light.SpotScale*cosOuter + light.SpotOffset; !near(off, 0) {
		t.Errorf("the cone evaluates to %v at its outer edge, want 0", off)
	}
}

// A zero OuterCone means pi/4, glTF's default; a zero InnerCone is a real
// value - falloff from the axis - rather than a default.
func TestAZeroOuterConeMeansAQuarterPi(t *testing.T) {
	light, err := PackLight(LightDescr{Direction: m.Vec3{Z: -1}, Kind: LightSpot})
	if err != nil {
		t.Fatal(err)
	}
	cosOuter := float32(math.Cos(math.Pi / 4))
	scale := 1 / (1 - cosOuter)
	if !near(light.SpotScale, scale) || !near(light.SpotOffset, -cosOuter*scale) {
		t.Errorf("cone is scale %v offset %v, want inner 0 and outer pi/4", light.SpotScale, light.SpotOffset)
	}
}

// An inner cone at or past the outer one is returned as an error and the light
// skipped, as is a spot with no direction, whose cone would silently evaluate
// to zero everywhere.
func TestADegenerateSpotLightIsReportedAndSkipped(t *testing.T) {
	_, err := PackLight(LightDescr{Direction: m.Vec3{Z: -1}, InnerCone: 0.7, OuterCone: 0.5, Kind: LightSpot})
	var inverted ErrSpotConeInverted
	if !errors.As(err, &inverted) || inverted.InnerCone != 0.7 || inverted.OuterCone != 0.5 {
		t.Errorf("an inverted cone reported %v, want ErrSpotConeInverted{0.7, 0.5}", err)
	}
	_, err = PackLight(LightDescr{Direction: m.Vec3{Z: -1}, InnerCone: 0.5, OuterCone: 0.5, Kind: LightSpot})
	if !errors.As(err, &inverted) {
		t.Errorf("an inner cone equal to the outer reported %v, want ErrSpotConeInverted", err)
	}
	_, err = PackLight(LightDescr{Kind: LightSpot})
	var missing ErrSpotDirectionMissing
	if !errors.As(err, &missing) {
		t.Errorf("a spot with no direction reported %v, want ErrSpotDirectionMissing", err)
	}
}

// Contribution is the light's own falloff at the eye - range window times
// cone over distance squared - times its colour's luminance.
func TestContributionIsTheFalloffAtTheEyeTimesLuminance(t *testing.T) {
	white, _ := PackLight(LightDescr{Position: m.Vec3{Z: -2}, Range: 4, Color: m.NewColorLinear(1, 1, 1, 1), Kind: LightPoint})
	eye := m.Vec3{}
	// d2 = 4, window = 1 - 16/256, luminance 1.
	if got, want := ContributionAt(&white, eye), float32((1-16.0/256)/4); !near(got, want) {
		t.Errorf("contribution is %v, want %v", got, want)
	}
	// Out of range the window is zero.
	if got := ContributionAt(&white, m.Vec3{Z: 10}); got != 0 {
		t.Errorf("a light out of range contributes %v at the eye, want 0", got)
	}
	// Green weighs more than red in the luminance.
	green, _ := PackLight(LightDescr{Position: m.Vec3{Z: -2}, Color: m.NewColorLinear(0, 1, 0, 1), Kind: LightPoint})
	red, _ := PackLight(LightDescr{Position: m.Vec3{Z: -2}, Color: m.NewColorLinear(1, 0, 0, 1), Kind: LightPoint})
	if ContributionAt(&green, eye) <= ContributionAt(&red, eye) {
		t.Error("a green light contributes no more than a red one of the same intensity")
	}
	// A spot pointing away from the eye contributes nothing there.
	away, _ := PackLight(LightDescr{Position: m.Vec3{Z: -2}, Direction: m.Vec3{Z: -1}, Color: m.NewColorLinear(1, 1, 1, 1), Kind: LightSpot})
	if got := ContributionAt(&away, eye); got != 0 {
		t.Errorf("a spot facing away contributes %v at the eye, want 0", got)
	}
}
