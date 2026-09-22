package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
)

// A light's bounds are the sphere (Position, Range); an infinite range is
// unculled by construction.
func TestALightIsCulledByItsRangeSphereUnlessInfinite(t *testing.T) {
	sphere, cullable := lightBounds(model.LightDescr{Position: m.Vec3{X: 1}, Range: 4})
	if !cullable || sphere != (m.Sphere{Center: m.Vec3{X: 1}, Radius: 4}) {
		t.Errorf("a ranged light bounds as %v (cullable %v)", sphere, cullable)
	}
	if _, cullable := lightBounds(model.LightDescr{Position: m.Vec3{X: 1}}); cullable {
		t.Error("an infinite-range light is cullable; it should survive every frustum")
	}
}

// preparedPointLights builds n white point lights in front of the origin, the
// i-th at z = -(i+1), so the nearest to the eye contribute most.
func preparedPointLights(n int) []preparedLight {
	lights := make([]preparedLight, n)
	for i := range lights {
		lights[i] = preparePointLight(m.Vec3{Z: -float32(i + 1)}, 0, 0)
	}
	return lights
}

func preparePointLight(position m.Vec3, rng float32, layers scene.LayerMask) preparedLight {
	descr := model.LightDescr{Position: position, Range: rng, Color: m.NewColorLinear(1, 1, 1, 1), Kind: model.LightPoint}
	record, _ := model.PackLight(descr)
	sphere, cullable := lightBounds(descr)
	return preparedLight{record: record, layers: layers, sphere: sphere, cullable: cullable}
}

// forwardFrustum looks down -Z from the origin with Near 0.1 and Far 100.
func forwardFrustum() m.Frustum {
	return m.FrustumFromMat4(m.Perspective4(math.Pi/2, 1, 0.1, 100))
}

// Culling runs before the cap: a light whose range sphere lies outside the
// frustum is not packed, and an infinite-range light always survives.
func TestLightsAreCulledAgainstTheFrustumBeforeTheCap(t *testing.T) {
	lights := []preparedLight{
		preparePointLight(m.Vec3{Z: -5}, 1, 0),  // in front, in range
		preparePointLight(m.Vec3{Z: 5}, 1, 0),   // behind, culled
		preparePointLight(m.Vec3{Z: 5}, 0, 0),   // behind, but infinite range
		preparePointLight(m.Vec3{Z: 2}, 2.5, 0), // behind, but its sphere reaches the frustum
	}
	var selection model.LightSelection
	selectLights(&selection, forwardFrustum(), m.Vec3{}, 0, lights)
	if selection.Count() != 3 {
		t.Fatalf("kept %d lights, want 3: the culled one is the ranged light behind the camera", selection.Count())
	}
	for _, light := range selection.Lights() {
		if light.Position.Z == 5 && light.InvRange4 != 0 {
			t.Error("the ranged light behind the camera was kept")
		}
	}
}

// A light's layer mask is filtered against the camera's cull mask, which
// decides whose buffer the light lands in, and nothing else.
func TestALightLayerMaskSelectsCameras(t *testing.T) {
	lights := []preparedLight{
		preparePointLight(m.Vec3{Z: -5}, 0, scene.Layer(1)),
		preparePointLight(m.Vec3{Z: -5}, 0, scene.Layer(2)),
		preparePointLight(m.Vec3{Z: -5}, 0, 0), // every layer
	}
	var selection model.LightSelection
	selectLights(&selection, forwardFrustum(), m.Vec3{}, scene.Layer(2), lights)
	if selection.Count() != 2 {
		t.Fatalf("a camera on layer 2 packed %d lights, want the layer-2 light and the unmasked one", selection.Count())
	}
}

// The selection is a fixed insertion: a steady frame allocates nothing.
func TestASteadySelectionAllocatesNothing(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	lights := preparedPointLights(40)
	var selection model.LightSelection
	frustum := forwardFrustum()
	allocations := testing.AllocsPerRun(50, func() {
		selectLights(&selection, frustum, m.Vec3{}, 0, lights)
	})
	if allocations != 0 {
		t.Fatalf("a steady selection allocated %v times", allocations)
	}
}

// PointLight and SpotLight set Kind themselves over the one struct, and each
// is one Op.
func TestPointLightAndSpotLightRecordTheirKind(t *testing.T) {
	var q scene.OpQueue
	q.PointLight(scene.Layer(3), model.LightDescr{Position: m.Vec3{X: 1}, Kind: model.LightSpot})
	q.SpotLight(0, model.LightDescr{Direction: m.Vec3{Z: -1}})
	types.OpQueueBeginFlush(&q)
	ops := q.Ops(nil)
	if len(ops) != 2 || ops[0].Kind != scene.OpPointLight || ops[1].Kind != scene.OpSpotLight {
		t.Fatalf("recorded %+v, want a point light op then a spot light op", ops)
	}
	if ops[0].Light.Kind != model.LightPoint || ops[0].Layers != scene.Layer(3) || ops[0].Light.Position != (m.Vec3{X: 1}) {
		t.Errorf("the point light op is %+v; PointLight should set Kind over the caller's struct", ops[0])
	}
	if ops[1].Light.Kind != model.LightSpot {
		t.Errorf("the spot light op is %+v; SpotLight should set Kind", ops[1])
	}
	lights := types.OpQueueFlushLights(&q)
	if len(lights) != 2 || lights[0].Descr.Kind != model.LightPoint || lights[1].Descr.Kind != model.LightSpot {
		t.Errorf("the flush sees %+v", lights)
	}
}
