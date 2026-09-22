package internal

import (
	"math"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// sceneLight is the 48-byte packed light the shader loops over, with no kind
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
// Field order and size must match SceneLight in builtin/scene/scene.wgsl.
type sceneLight struct {
	Position  m.Vec3
	InvRange4 float32
	Direction m.Vec3
	SpotScale float32
	// Color is linear radiance with Intensity already premultiplied.
	Color      m.Vec3
	SpotOffset float32
}

// packLight resolves one light's defaults into its packed record. The cone is
// precomputed here - spotScale = 1 / (cos inner - cos outer), spotOffset =
// -cos outer * spotScale - so the shader is one dot, one MAD and one saturate.
// An inner cone at or past the outer one, or a spot with no direction, is
// returned as the error the flush reports; the light is skipped.
func packLight(descr scene.LightDescr) (sceneLight, error) {
	color := radiance(descr.Color, descr.Intensity)
	light := sceneLight{
		Position:   descr.Position,
		Color:      m.Vec3{X: color.X, Y: color.Y, Z: color.Z},
		SpotOffset: 1,
	}
	if descr.Range > 0 {
		light.InvRange4 = 1 / (descr.Range * descr.Range * descr.Range * descr.Range)
	}
	if descr.Kind != scene.LightSpot {
		return light, nil
	}
	direction := descr.Direction.Normalize()
	if direction == (m.Vec3{}) {
		return sceneLight{}, scene.ErrSpotDirectionMissing{}
	}
	outer := descr.OuterCone
	if outer == 0 {
		outer = math.Pi / 4
	}
	if descr.InnerCone >= outer {
		return sceneLight{}, scene.ErrSpotConeInverted{InnerCone: descr.InnerCone, OuterCone: outer}
	}
	cosInner := float32(math.Cos(float64(descr.InnerCone)))
	cosOuter := float32(math.Cos(float64(outer)))
	light.Direction = direction
	light.SpotScale = 1 / max(cosInner-cosOuter, 1e-4)
	light.SpotOffset = -cosOuter * light.SpotScale
	return light, nil
}

// lightBounds is the sphere a light is culled by: its position and range. An
// infinite range has no sphere and is never culled.
func lightBounds(descr scene.LightDescr) (m.Sphere, bool) {
	if descr.Range <= 0 {
		return m.Sphere{}, false
	}
	return m.Sphere{Center: descr.Position, Radius: descr.Range}, true
}

// contributionAt is what one light is worth at a point: its own falloff
// evaluated there - the same range window, cone and inverse-square the shader
// applies - times its colour's luminance. It is the cap's ranking, evaluated at
// the eye, so the lights dropped past 16 are exactly the ones contributing
// least where the camera stands.
//
// The max(d2, 1e-6) is a robustness guard for a light sitting on the surface
// being shaded, not a falloff parameter; it is mirrored from the shader so the
// two agree.
func contributionAt(light *sceneLight, point m.Vec3) float32 {
	toLight := light.Position.Sub(point)
	d2 := toLight.LengthSquared()
	direction := toLight.MulS(1 / float32(math.Sqrt(float64(max(d2, 1e-12)))))
	window := m.Clamp01(1 - d2*d2*light.InvRange4)
	cone := m.Clamp01(-direction.Dot(light.Direction)*light.SpotScale + light.SpotOffset)
	luminance := 0.2126*light.Color.X + 0.7152*light.Color.Y + 0.0722*light.Color.Z
	return luminance * window * cone / max(d2, 1e-6)
}

// preparedLight is one recorded light resolved once per frame, before any
// camera looks at it: its packed record and its world sphere. Every pass then
// pays one sphere test and one contribution per light.
type preparedLight struct {
	record   sceneLight
	layers   scene.LayerMask
	sphere   m.Sphere
	cullable bool
}

// prepareLights packs the frame's lights once. A degenerate light is reported
// here, once per frame rather than once per pass, and left out.
func prepareLights(report func(error), dst []preparedLight, lights []types.LightRecord) []preparedLight {
	dst = dst[:0]
	for i := range lights {
		record, err := packLight(lights[i].Descr)
		if err != nil {
			report(err)
			continue
		}
		sphere, cullable := lightBounds(lights[i].Descr)
		dst = append(dst, preparedLight{
			record: record, layers: lights[i].Layers, sphere: sphere, cullable: cullable,
		})
	}
	return dst
}

// lightSelection is one pass's chosen lights: a fixed array of the cap's size
// and the score each entry got in. It is filled by insertion - past the cap, a
// new light replaces the weakest entry if it beats it - with no sort and no
// allocation. The buffer's order is therefore not a ranking, which the shader
// does not need: it sums.
type lightSelection struct {
	lights [model.MaxLights]sceneLight
	scores [model.MaxLights]float32
	count  int
}

// selectLights culls and caps the frame's lights for one pass. Culling is per
// pass because it is against the pass's own frustum: a camera's shadow pass
// and its screen pass can see different light sets, which is correct rather
// than surprising. Past the cap, the excess is dropped silently: a 17th light
// is dynamic and camera-shaped - it appears when you turn around - so there is
// no natural "once" to report at, a per-frame report is pure noise, and the
// degradation is continuous by construction.
func (s *lightSelection) selectLights(
	frustum m.Frustum, eye m.Vec3, cullMask scene.LayerMask, lights []preparedLight,
) {
	s.count = 0
	for i := range lights {
		light := &lights[i]
		if !types.LayerMaskDrawnBy(light.layers, cullMask) {
			continue
		}
		if light.cullable && !frustum.ContainsSphere(light.sphere.Center, light.sphere.Radius) {
			continue
		}
		s.offer(&light.record, contributionAt(&light.record, eye))
	}
}

// offer inserts one light, replacing the weakest kept light once the array is
// full and only if this one beats it.
func (s *lightSelection) offer(light *sceneLight, score float32) {
	if s.count < model.MaxLights {
		s.lights[s.count], s.scores[s.count] = *light, score
		s.count++
		return
	}
	weakest := 0
	for i := 1; i < model.MaxLights; i++ {
		if s.scores[i] < s.scores[weakest] {
			weakest = i
		}
	}
	if score > s.scores[weakest] {
		s.lights[weakest], s.scores[weakest] = *light, score
	}
}
