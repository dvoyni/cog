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

// LightRecord is one recorded light: what the flush consumes.
type LightRecord struct {
	Layers LayerMask
	Descr  LightDescr
}

// PointLight records a point light for this frame. Kind is set here, over the
// caller's struct, so a hand-written LightDescr needs no Kind of its own.
//
// Layers decide which cameras' light buffers the light lands in - a light on
// layer 1 reaches every camera whose CullMask includes layer 1 - and nothing
// else. In particular they do not decide which objects the light illuminates:
// within a pass, every light in the buffer lights every draw. That would need a
// per-draw light list, which contradicts the frame block being bound once for
// the whole pass.
func (q *OpQueue) PointLight(layers LayerMask, light LightDescr) {
	light.Kind = LightPoint
	q.calls = append(q.calls, Op{Kind: OpPointLight, Layers: layers, Light: light})
	q.lights = append(q.lights, LightRecord{Layers: layers, Descr: light})
}

// SpotLight records a spot light for this frame: a point light with a cone
// about Direction, fully on inside InnerCone and off beyond OuterCone, with
// KHR_lights_punctual's smoothing between them, linear in cosine. Layers work
// as for PointLight.
func (q *OpQueue) SpotLight(layers LayerMask, light LightDescr) {
	light.Kind = LightSpot
	q.calls = append(q.calls, Op{Kind: OpSpotLight, Layers: layers, Light: light})
	q.lights = append(q.lights, LightRecord{Layers: layers, Descr: light})
}

// flushLights lists the lights the flush is consuming, in recording order.
func (q *OpQueue) flushLights() []LightRecord { return q.publishedLights }
