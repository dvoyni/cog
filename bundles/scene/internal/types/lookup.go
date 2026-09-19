package types

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/gfx"
)

// Lookup is the single scene-owned persistent resource. It holds everything
// that outlives a frame — loaded models, baked pose and morph buffers, the
// texture cache, buffer-built meshes and scene's own unit meshes —
// plus the deferred bakes and buffer releases the flush applies at the frame
// boundary.
//
// It never retains a filesystem or GPU handle of its own. Query and mutate it
// only through a scoped LookupAccess or LookupDeviceAccess.
type Lookup struct {
	config Config
	// meshes is the dense mesh table every MeshRef indexes, and a ref's id is
	// its position in it plus one, so id 0 stays "no mesh". freeMeshes are the
	// slots ReleaseMesh gave back, reissued newest first.
	meshes     []MeshRecord
	freeMeshes []uint32
	// layouts interns the vertex layouts durable meshes were baked from.
	layouts layoutCache
	// staging holds the bytes BakeMesh and UpdateMesh copied out of their
	// callers, and pendingMeshes the uploads waiting on them. The arena is
	// handed to gfx wholesale at the flush and a fresh one grown after, rather
	// than reused: BakeBuffer takes the bytes without copying them, so reusing
	// the backing would rewrite an upload still in flight.
	staging       []byte
	pendingMeshes []pendingMesh
	// pendingReleases are the buffers ReleaseMesh and a model's free gave up,
	// released at the frame boundary so nothing the frame already recorded
	// draws from a dead buffer.
	pendingReleases []gfx.BufferDescr
	// unit holds scene's own meshes - the box, sphere and plane the debug
	// vocabulary draws - each baked on first use.
	unit [shapeCount]MeshRef
	// models is the model cache, keyed by the path that is a model's only cache
	// key, and textures the scene-owned texture cache every loaded model's
	// materials bind out of. They are two caches rather than two tiers of one,
	// and their params types are two types for the same reason. Neither is
	// refcounted: nothing unloads automatically, so there is nothing for a
	// count to drive.
	models   *assets.Cache[ModelDescrParams, modelUserData, *residentModel]
	textures *assets.Cache[textureDescrParams, textureUserData, gfx.TextureDescr]
	// poseBytes and morphBytes are the GPU memory every loaded model's baked
	// poses and morph deltas occupy, added by a load and subtracted by a free.
	// They are counters rather than a walk because the two queries reporting
	// them are what a memory HUD reads every frame, and because a cache has no
	// walk to offer.
	poseBytes  int
	morphBytes int
	// defaults are the two 1x1 textures every empty PBR slot binds, baked on
	// first use. hasDefaults rather than a zero test because a baked descriptor
	// has no reserved zero value.
	defaults    PbrDefaults
	hasDefaults bool
	// bundled is the bundled PBR material once per shader variant, built on
	// first use around the two default textures. It is not a package-level value because those textures
	// are baked resources: the backend may not be Ready() at startup, and a
	// texture baked then would either panic or silently not exist.
	bundled    [VariantCount]Material
	hasBundled bool
}

// NewLookup builds an empty Lookup at scene's default configuration; see
// scene.NewLookup.
func NewLookup() *Lookup { return NewSizedLookup(WithDefaults(Config{})) }

// NewSizedLookup builds an empty Lookup for config, which is already complete.
// The plugin creates its own this way from its resolved configuration.
func NewSizedLookup(config Config) *Lookup {
	return &Lookup{
		config:   config,
		models:   assets.New(modelLoader{}),
		textures: assets.New(textureLoader{}),
	}
}

// LookupAccess is the scoped facade for everything about a Lookup that neither
// loads a model nor frees a GPU texture: the mesh verbs, UnloadModel and the
// two memory totals. Acquire a *Lookup write dependency in a handler, build one
// with NewLookupAccess, and pass it to consumers for the duration of that
// handler. Never store the result: the handles behind it are valid only while
// the handler holds its lock.
//
// Two dependencies, and the split is why. A model load now runs inside the call
// that asks for it, so every verb that can load needs the filesystem and the
// resource queue at the call - and one consumer of this facade is an ECS System
// whose entire use of it is two BakeMesh calls. Making that System declare a
// gfx write to bake a cube would serialise it against canvas's flush, scene's
// flush and gfx, so the loading half is LookupDeviceAccess and this half costs
// its caller exactly what it always did.
type LookupAccess struct {
	kernel kernel.Kernel
	lookup *Lookup
}

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds
// the *Lookup write lock.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup) LookupAccess {
	return LookupAccess{kernel: k, lookup: lookup}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupAccess) Valid() bool { return la.lookup != nil }

// LookupDeviceAccess is the scoped facade for everything that needs the device:
// Preload, State and the model queries, every one of which loads, and the two
// unload verbs that free a GPU texture at the call.
//
// It is named for what it carries rather than for what it does, which is the
// convention canvas's facade of the same name follows from the opposite
// direction - there the loading half is the cheap one and the unloading half is
// the device's. The constraint is the device either way, and a consumer reading
// two plugins sees the same word.
//
// Build one inside a handler holding *Lookup write, storage.FileSystem read and
// *gfx.ResourceQueue write. The filesystem arrives already converted to an
// fs.FS, because handing a storage.FileSystem out as an interface boxes and
// that box is worth paying once a frame rather than once a call.
type LookupDeviceAccess struct {
	kernel    kernel.Kernel
	lookup    *Lookup
	fsys      fs.FS
	resources *gfx.ResourceQueue
}

// NewLookupDeviceAccess builds the device facade. Call it inside a handler that
// holds the *Lookup write lock, the filesystem read lock and the resource queue
// write lock.
func NewLookupDeviceAccess(
	k kernel.Kernel, lookup *Lookup, fsys fs.FS, resources *gfx.ResourceQueue,
) LookupDeviceAccess {
	return LookupDeviceAccess{kernel: k, lookup: lookup, fsys: fsys, resources: resources}
}

// Valid reports whether the facade is backed by a live Lookup.
func (la LookupDeviceAccess) Valid() bool { return la.lookup != nil }

// resolve is what every query here begins with: the model at path, loaded if it
// is not loaded yet, and whether there is one to answer about. Why there is not
// is State's business, and the report has already been made.
func (la LookupDeviceAccess) resolve(path string) (*residentModel, bool) {
	if !la.Valid() {
		return nil, false
	}
	model, err := la.lookup.model(la.kernel, la.fsys, la.resources, path)
	return model, err == nil
}

// ensureBundled builds the bundled PBR's four variants the first time something
// draws, baking the two 1x1 default textures they bind into every absent slot,
// and returns the same materials forever after.
//
// 1x1 rather than larger because uploads carry no row-alignment rule and for a
// constant texel every mip level is identical, so there is nothing to generate.
// Both are linear-format: 1.0 is a fixed point of the sRGB transfer curve, so
// the white texel reads 1.0 through an sRGB slot and a linear one alike, and
// the flat normal is not a picture at all.
func (l *Lookup) ensureBundled(bake bakeTextureFunc) [VariantCount]Material {
	if l.hasBundled {
		return l.bundled
	}
	if !l.hasDefaults {
		l.defaults = PbrDefaults{
			White:      bake(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}),
			FlatNormal: bake(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}),
		}
		l.hasDefaults = true
	}
	l.bundled, l.hasBundled = BundledPbr(l.defaults), true
	return l.bundled
}
