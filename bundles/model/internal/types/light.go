package types

import (
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
