package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// lightBounds is the sphere a light is culled by: its position and range. An
// infinite range has no sphere and is never culled.
func lightBounds(descr model.LightDescr) (m.Sphere, bool) {
	if descr.Range <= 0 {
		return m.Sphere{}, false
	}
	return m.Sphere{Center: descr.Position, Radius: descr.Range}, true
}

// preparedLight is one recorded light resolved once per frame, before any
// camera looks at it: its packed record and its world sphere. Every pass then
// pays one sphere test and one contribution per light.
type preparedLight struct {
	record   model.Light
	layers   scene.LayerMask
	sphere   m.Sphere
	cullable bool
}

// prepareLights packs the frame's lights once. A degenerate light is reported
// here, once per frame rather than once per pass, and left out.
func prepareLights(report func(error), dst []preparedLight, lights []types.LightRecord) []preparedLight {
	dst = dst[:0]
	for i := range lights {
		record, err := model.PackLight(lights[i].Descr)
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

// selectLights culls the frame's lights for one pass and offers the survivors
// to the pass's selection, which keeps the MaxLights worth most at the eye.
// Culling is per pass because it is against the pass's own frustum: a camera's
// shadow pass and its screen pass can see different light sets, which is
// correct rather than surprising.
func selectLights(
	selection *model.LightSelection,
	frustum m.Frustum, eye m.Vec3, cullMask scene.LayerMask, lights []preparedLight,
) {
	selection.Reset()
	for i := range lights {
		light := &lights[i]
		if !types.LayerMaskDrawnBy(light.layers, cullMask) {
			continue
		}
		if light.cullable && !frustum.ContainsSphere(light.sphere.Center, light.sphere.Radius) {
			continue
		}
		selection.Offer(&light.record, model.ContributionAt(&light.record, eye))
	}
}

// frameLighting copies a camera's sun and ambient into the shape model's frame
// packer takes, field for field under the same names.
func frameLighting(descr scene.CameraDescr) model.FrameLighting {
	return model.FrameLighting{
		SunDirection:     descr.SunDirection,
		SunColor:         descr.SunColor,
		SunIntensity:     descr.SunIntensity,
		AmbientSky:       descr.AmbientSky,
		AmbientGround:    descr.AmbientGround,
		AmbientIntensity: descr.AmbientIntensity,
	}
}
