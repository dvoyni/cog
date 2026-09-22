package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// Lights are naive forward: one light list per pass, and every shaded fragment
// loops all of it. The honest consequence, stated rather than hidden: adding a
// light costs every shaded pixel in the pass, which is what makes the cap
// load-bearing rather than decorative. Culling is what makes the cap
// survivable - a level with 40 lights of which 6 are on screen works perfectly.
//
// The array holds point and spot lights only. The sun and hemispheric ambient
// are per-camera fields on CameraDescr, which is what keeps the record
// branchless and 48 bytes with no kind field: packing the sun as a directional
// entry would cost an explicit discriminator, since infinite range is already
// taken, and the unification is unachievable anyway, because hemispheric
// ambient is normal-dependent rather than a direction and could never join the
// loop.
//
// Intensity is unitless - radiance at one world unit - not photometric. Targets
// are 8-bit sRGB, tonemapping is out of scope, so shading lands directly in 0..1
// with no exposure control anywhere; a 60 W bulb is ~64 cd and every frame
// would be pure white. When HDR lands this becomes photometric by redefining
// the unit and changing nothing else.

// LightKind is which of the two punctual lights a LightDescr describes.
// PointLight and SpotLight set it themselves; it is exposed so an Op can be
// read back.
type LightKind uint8

const (
	LightPoint LightKind = iota
	LightSpot
)

// LightDescr is one punctual light, point or spot, over the one struct: call
// sites stay explicit through PointLight and SpotLight, a hand-written point
// light leaves the cone fields zero, and the per-camera light buffer is
// homogeneous without scene converting between two structs.
//
// Every zero is a default. Intensity zero means 1. Range zero means infinite,
// glTF's own default - a forgotten Range yields a light that reaches too far,
// which you see immediately, rather than a silently skipped light. OuterCone
// zero means pi/4, glTF's default; InnerCone zero is a real value, falloff
// from the axis.
type LightDescr struct {
	Position  m.Vec3
	Direction m.Vec3  // direction of travel; ignored for a point light
	Color     m.Color // linear
	Intensity float32 // zero means 1
	Range     float32 // world units; zero means infinite
	InnerCone float32 // radians; zero is a real value
	OuterCone float32 // radians; zero means pi/4
	Kind      LightKind
}

// MaxLights is the per-pass cap, and it is a fixed constant rather than a
// Config knob: a knob needs documented interaction rules, and the answer to "I
// need 40 lights" is clustered lighting, not a number that makes the naive loop
// slower. Being fixed is also what lets the shader declare
// `lights: array<SceneLight, 16>` rather than a runtime-sized array.
const MaxLights = 16

// ModelLight is one KHR_lights_punctual light a model file declares, in the
// model's own space, with its node's flattened transform already applied.
//
// Lights are exposed as data and nothing converts one automatically. A file's
// lights are authored for the file, not for the scene it is dropped into: a
// lamp prop placed forty times would silently blow the sixteen-light per-pass
// cap, and which of a level's lights matter is the app's judgement, not the
// loader's. So an app reads these and declares the ones it wants through
// PointLight and SpotLight, at whatever world transform it drew the model at.
type ModelLight struct {
	Name string
	// Directional marks a glTF directional light, which scene has no recording
	// call for at all - the one directional light scene shades with is the
	// camera's own sun. Descr.Direction is the only placement such a light has.
	Directional bool
	// Descr is the light as scene's own recording calls take it, so declaring
	// one is PointLight(layers, light.Descr) with the position and direction
	// carried into world space.
	Descr LightDescr
}

// Light is the 48-byte packed light the shader loops over, with no kind
// field. A point light is a spot whose cone is always fully on: direction an
// actual zero vector, spotScale 0, spotOffset 1, so the cone term is
// saturate(0 + 1). The direction must be a real zero and never left
// uninitialised, because that trick relies on x * 0 == 0, which is false for
// NaN.
//
// invRange4 is 1/range^4 rather than range, so that the shader's
// saturate(1 - d^4 * invRange4) evaluates to exactly 1 when it is 0: no branch,
// no select, no special case for infinity. The residual cost is that an
// infinite-range light is unculled by construction and always survives to the
// cap.
//
// Field order and size must match SceneLight in builtin/scene/frame.wgsl.
type Light struct {
	Position  m.Vec3
	InvRange4 float32
	Direction m.Vec3
	SpotScale float32
	// Color is linear radiance with Intensity already premultiplied.
	Color      m.Vec3
	SpotOffset float32
}

// PackLight resolves one light's defaults into its packed record. The cone is
// precomputed here - spotScale = 1 / (cos inner - cos outer), spotOffset =
// -cos outer * spotScale - so the shader is one dot, one MAD and one saturate.
// An inner cone at or past the outer one, or a spot with no direction, is
// returned as the error a renderer reports; the light is skipped.
func PackLight(descr LightDescr) (Light, error) {
	color := radiance(descr.Color, descr.Intensity)
	light := Light{
		Position:   descr.Position,
		Color:      m.Vec3{X: color.X, Y: color.Y, Z: color.Z},
		SpotOffset: 1,
	}
	if descr.Range > 0 {
		light.InvRange4 = 1 / (descr.Range * descr.Range * descr.Range * descr.Range)
	}
	if descr.Kind != LightSpot {
		return light, nil
	}
	direction := descr.Direction.Normalize()
	if direction == (m.Vec3{}) {
		return Light{}, ErrSpotDirectionMissing{}
	}
	outer := descr.OuterCone
	if outer == 0 {
		outer = math.Pi / 4
	}
	if descr.InnerCone >= outer {
		return Light{}, ErrSpotConeInverted{InnerCone: descr.InnerCone, OuterCone: outer}
	}
	cosInner := float32(math.Cos(float64(descr.InnerCone)))
	cosOuter := float32(math.Cos(float64(outer)))
	light.Direction = direction
	light.SpotScale = 1 / max(cosInner-cosOuter, 1e-4)
	light.SpotOffset = -cosOuter * light.SpotScale
	return light, nil
}

// ContributionAt is what one light is worth at a point: its own falloff
// evaluated there - the same range window, cone and inverse-square the shader
// applies - times its colour's luminance. It is the cap's ranking, evaluated at
// the eye, so the lights dropped past MaxLights are exactly the ones
// contributing least where the camera stands.
//
// The max(d2, 1e-6) is a robustness guard for a light sitting on the surface
// being shaded, not a falloff parameter; it is mirrored from the shader so the
// two agree.
func ContributionAt(light *Light, point m.Vec3) float32 {
	toLight := light.Position.Sub(point)
	d2 := toLight.LengthSquared()
	direction := toLight.MulS(1 / float32(math.Sqrt(float64(max(d2, 1e-12)))))
	window := m.Clamp01(1 - d2*d2*light.InvRange4)
	cone := m.Clamp01(-direction.Dot(light.Direction)*light.SpotScale + light.SpotOffset)
	luminance := 0.2126*light.Color.X + 0.7152*light.Color.Y + 0.0722*light.Color.Z
	return luminance * window * cone / max(d2, 1e-6)
}
