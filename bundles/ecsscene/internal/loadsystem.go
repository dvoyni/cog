package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// loadSystem is the load System. It is the one ecsscene System that loads,
// and it drives model's bake and release queues: nothing else in an ecsscene
// app drains them, because an app runs ecsscene or scene and never both. The
// debug shapes' Systems hold the Lookup for writing too, to bake the shapes'
// meshes, and run before it, so a mesh they bake is drained this tick.
//
// It runs on what changed: every addition, change and removal of a Model,
// Mesh, Material or Params since its last run, each Entity once. For each it
// resolves the ModelRef to a ModelHandle, loading the model if needed, and
// computes the Batch keys into keyScratch. In a steady frame no Hook names
// anything, so it walks no Entity and hashes nothing; what is left is the
// drain of two empty queues.
//
// Everything it reads from the Stores is a read, so it keeps no System off
// them. What it writes is exclusive by nature: the Lookup, and the resource
// queue a load uploads through.
func loadSystem(
	k kernel.Kernel,
	modelHooks *ecs.Hooks[Model, ecs.HookAll],
	meshHooks *ecs.Hooks[Mesh, ecs.HookAll],
	materialHooks *ecs.Hooks[Material, ecs.HookAll],
	paramsHooks *ecs.Hooks[Params, ecs.HookAll],
	models *ecs.Get[Model],
	meshes *ecs.Get[Mesh],
	materials *ecs.Get[Material],
	params *ecs.Get[Params],
	lookupResource *ecs.Write[*model.Lookup],
	filesystem *ecs.Read[storage.FileSystem],
	resourceQueue *ecs.Write[*gfx.ResourceQueue],
	work *ecs.Write[*keyScratch],
) {
	s := work.Get()
	s.walked = 0
	for e := range modelHooks.All() {
		s.touch(e)
	}
	for e := range meshHooks.All() {
		s.touch(e)
	}
	for e := range materialHooks.All() {
		s.touch(e)
	}
	for e := range paramsHooks.All() {
		s.touch(e)
	}
	resources := resourceQueue.Get()
	// A load before the backend is up is refused and not cached, so the
	// Entities wait for the first frame that has one. Nothing is drained
	// either, and the recording System skips the frame too.
	s.ready = resources != nil && resources.Ready()
	if !s.ready {
		return
	}
	lookup := lookupResource.Get()
	// The bundled PBR bakes its two default textures the first time, and
	// the Lookup binds the same two forever after.
	if !s.hasBundled {
		s.bundled = lookup.EnsureBundledIngredients(func(width, height int, format gfx.TextureFormat, pixels []byte) gfx.TextureDescr {
			return resources.BakeTexture(width, height, format, pixels, true, false)
		})
		s.hasBundled = true
	}
	if len(s.pending) > 0 {
		keyer := keyer{
			k: k, lookup: lookup, resources: resources,
			// The one boxed filesystem a run pays for, and only a run that
			// has something to key.
			fsys:      fs.FS(filesystem.Get()),
			models:    models,
			meshes:    meshes,
			materials: materials,
			params:    params,
		}
		for _, e := range s.pending {
			keyer.key(s, e)
		}
		s.walked = len(s.pending)
		clear(s.pending)
		s.pending = s.pending[:0]
		clear(s.seen)
	}
	lookup.DrainMeshes(model.MeshBaker{
		Bake: func(data []byte) gfx.BufferDescr { return resources.BakeBuffer(data, false) },
		Rebake: func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr {
			return resources.ReBakeBuffer(buffer, data, false)
		},
		Release: resources.ReleaseBuffer,
	})
}
