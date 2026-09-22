package types

import "github.com/dvoyni/cog/bundles/model"

// LightRecord is one recorded light: what the flush consumes.
type LightRecord struct {
	Layers LayerMask
	Descr  model.LightDescr
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
func (q *OpQueue) PointLight(layers LayerMask, light model.LightDescr) {
	light.Kind = model.LightPoint
	q.calls = append(q.calls, Op{Kind: OpPointLight, Layers: layers, Light: light})
	q.lights = append(q.lights, LightRecord{Layers: layers, Descr: light})
}

// SpotLight records a spot light for this frame: a point light with a cone
// about Direction, fully on inside InnerCone and off beyond OuterCone, with
// KHR_lights_punctual's smoothing between them, linear in cosine. Layers work
// as for PointLight.
func (q *OpQueue) SpotLight(layers LayerMask, light model.LightDescr) {
	light.Kind = model.LightSpot
	q.calls = append(q.calls, Op{Kind: OpSpotLight, Layers: layers, Light: light})
	q.lights = append(q.lights, LightRecord{Layers: layers, Descr: light})
}

// flushLights lists the lights the flush is consuming, in recording order.
func (q *OpQueue) flushLights() []LightRecord { return q.publishedLights }
