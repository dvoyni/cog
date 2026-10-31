package internal

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
)

// lightBounds is the sphere a light is culled by: its position and range. An
// infinite range has no sphere and is never culled.
func lightBounds(descr *model.LightDescr) (m.Sphere, bool) {
	if descr.Range <= 0 {
		return m.Sphere{}, false
	}
	return m.Sphere{Center: descr.Position, Radius: descr.Range}, true
}

// preparedLight is one Light Entity resolved once per frame, before any camera
// looks at it: its packed record and its world sphere. Every pass then pays
// one layer test, one sphere test and one contribution per light.
type preparedLight struct {
	record   model.Light
	layers   LayerMask
	sphere   m.Sphere
	cullable bool
}

// prepareLight packs one light through model's packer. A degenerate light is
// reported here, once per frame rather than once per pass, and left out.
func prepareLight(
	report errorReporter, dst []preparedLight, descr *model.LightDescr, layers LayerMask,
) []preparedLight {
	record, err := model.PackLight(*descr)
	if err != nil {
		report.ReportError(err)
		return dst
	}
	sphere, cullable := lightBounds(descr)
	return append(dst, preparedLight{record: record, layers: layers, sphere: sphere, cullable: cullable})
}

// selectLights culls the frame's lights for one pass and offers the survivors
// to the pass's selection, which keeps the MaxLights worth most at the eye.
// Culling is per pass because it is against the pass's own frustum.
func selectLights(
	selection *model.LightSelection,
	frustum m.Frustum, eye m.Vec3, cullMask LayerMask, lights []preparedLight,
) {
	selection.Reset()
	for i := range lights {
		light := &lights[i]
		if !drawnBy(light.layers, cullMask) {
			continue
		}
		if light.cullable && !frustum.ContainsSphere(light.sphere.Center, light.sphere.Radius) {
			continue
		}
		selection.Offer(&light.record, model.ContributionAt(&light.record, eye))
	}
}

// frameLighting copies a Camera's sun and ambient into the shape model's frame
// packer takes, field for field under the same names.
func frameLighting(camera *Camera) model.FrameLighting {
	return model.FrameLighting{
		SunDirection:     camera.SunDirection,
		SunColor:         camera.SunColor,
		SunIntensity:     camera.SunIntensity,
		AmbientSky:       camera.AmbientSky,
		AmbientGround:    camera.AmbientGround,
		AmbientIntensity: camera.AmbientIntensity,
	}
}
