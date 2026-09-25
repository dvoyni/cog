package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// facing is the direction an unrotated Transform faces, which is the way
// m.LookAt aims.
var facing = m.Vec3{Z: -1}

// recordSystem is the recording System: every Entity with a Transform and a
// Model, Mesh, Light or Camera, drawn into gfx once a tick. It buckets the frame's instances into Batches by the keys
// the load System wrote, and for each camera and pass culls, applies the layer
// mask and filters each Batch's instances, then sorts, and packs one properties
// record and one instanced draw per Batch through model's packers.
//
// Its signature is its whole lock set. It reads the Stores, the load System's
// keys, the Lookup through the read facade and the viewport; it writes only
// its own scratch and gfx's queue. It declares no ordering beyond the load
// System's Before: gfx presents Last, so an ordinary-phase System already runs
// before it, and a game System that moves Transforms orders itself
// Before[scene.RecordOnUpdate].
func recordSystem(
	k kernel.Kernel,
	models *ecs.Query[modelQuery],
	meshes *ecs.Query[meshQuery],
	lights *ecs.Query[lightQuery],
	cameras *ecs.Query[cameraQuery],
	animations *ecs.Get[Animation],
	params *ecs.Get[Params],
	materials *ecs.Get[Material],
	keys *ecs.Read[*keyScratch],
	lookup *ecs.Read[*model.Lookup],
	viewport *ecs.Read[*gfx.Viewport],
	work *ecs.Write[*scratch],
	out *ecs.Write[*gfx.OpQueue],
) {
	s, keyed, view := work.Get(), keys.Get(), viewport.Get()
	// A frame before the backend is up, before the MainLoop has reported a
	// window, or while one is minimised, is skipped whole:
	// every screen-targeted pass would resolve an aspect of zero.
	if !keyed.ready || view == nil || view.WindowWidth <= 0 || view.WindowHeight <= 0 {
		return
	}
	read := model.NewLookupReadAccess(lookup.Get())
	s.begin(k, keyed, read.DefaultSceneShader(), out.Get())
	frame := frameInputs{
		read: read, keys: keyed,
		animations: animations, params: params, materials: materials,
	}
	for e, it := range models.All() {
		s.addModel(&frame, e, it)
	}
	for e, it := range meshes.All() {
		s.addMesh(&frame, e, it)
	}
	for _, it := range lights.All() {
		// The Component's Position and Direction are documented as ignored:
		// the Transform places the light.
		light := it.Light.Descr
		light.Position, light.Direction = it.Place.Position, m.Vec3{}
		if light.Kind == model.LightSpot {
			// m.Quat.Rotate reads the zero Quat as no rotation, so an
			// unrotated spot shines down -Z.
			light.Direction = it.Place.Rotation.Rotate(facing)
		}
		s.preparedLights = prepareLight(k, s.preparedLights, &light, it.Light.Layers)
	}
	for _, it := range cameras.All() {
		s.addCamera(k, it)
	}
	for i := range s.cameras {
		s.flushCamera(k, view, &s.cameras[i])
	}
	s.build.emit(out.Get())
	s.release()
}
