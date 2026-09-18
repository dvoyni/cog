package types

import (
	"strings"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// UnloadModel gives up one path's geometry, baked poses and material records.
// Unloading an absent path is a no-op, and a later draw or query of an unloaded
// path loads it again.
//
// It frees at the call, and the entry leaves the cache at the call, so a draw
// recorded earlier in the same tick reloads the model at the flush rather than
// drawing a freed one. A free followed by a get is a reload, not an error. The
// GPU buffers themselves still go through the pending-release queue the flush
// drains at the frame boundary, which is the same queue ReleaseMesh uses.
//
// It does not cascade to textures. With no refcount the lookup cannot know
// whether another loaded model binds the same image by path, and freeing one
// that is still bound is a dead texture in a live bind group rather than a
// missing picture. UnloadTexture is the separate, deliberate lever.
//
// It is also the only retry lever there is. A failed path clears here and
// nowhere else, so recovering from a bad file - or from a path so malformed it
// never reached a load at all - is UnloadModel followed by Preload. There is no
// Retry, because a Retry that did not first free would be a second name for the
// idempotent load that already exists.
//
// It needs no device, which is why it is here rather than on the device facade:
// a model's own handles are buffers, and buffers already have a queue.
func (la LookupAccess) UnloadModel(path string) {
	if !la.Valid() {
		return
	}
	// ModelKey collapses the two cases: a valid path unloads under its cleaned
	// form, and an invalid one has no entry to free but does have a report key
	// recorded under exactly what the caller passed.
	key, _ := ModelKey(path)
	la.lookup.clearModelReports(la.kernel, key)
	la.lookup.models.Free(la.kernel, modelDescr{Name: key}, modelUser{lookup: la.lookup})
}

// UnloadTexture frees one image path's GPU textures.
//
// Every texture the path baked goes, not one: colour space is part of a
// texture's cache key because it is the binding slot's property rather than the
// image's, so one file bound as both a base colour and a normal map is two GPU
// textures - and path is the whole of what a caller can name. For a glb the
// path names the container, so unloading it releases every image embedded in
// it.
//
// That is a predicate over entries rather than a key, which is what FreeWhere
// is: the caller cannot name the variants, and the descriptor the table keys
// already carries the answer. Each freed entry's own report key is forgotten
// with it, so the Library's read failure for a picture that has since been
// added can speak again.
//
// Nothing checks whether a loaded model still binds them. This is the lever for
// a texture whose models are already gone, and using it while one is loaded
// leaves that model's bind groups pointing at freed textures.
//
// Releasing a texture needs the queue at the call, which is what puts this verb
// on the device facade and leaves UnloadModel on the other one.
func (la LookupDeviceAccess) UnloadTexture(path string) {
	if !la.Valid() {
		return
	}
	key, ok := ModelKey(path)
	if !ok {
		return
	}
	l := la.lookup
	l.textures.FreeWhere(la.kernel, textureUser{resources: la.resources},
		func(d textureDescr, _ gfx.TextureDescr) bool { return d.Name == key })
	l.clearTextureReports(la.kernel, key)
}

// UnloadAll frees every loaded model and every cached texture. It is the level
// teardown, and it spares three things: buffer-built meshes, which are the
// caller's own handles minted by BakeMesh and released by ReleaseMesh, with no
// way for a lookup-wide sweep to tell the caller its refs went stale; scene's
// own unit meshes; and the two default textures, which are the plugin's and
// would have to be re-baked on the very next frame.
//
// It walks what is loaded at the call. A model asked for after it and before
// the frame ends was deliberately asked for, and survives.
func (la LookupDeviceAccess) UnloadAll() {
	if !la.Valid() {
		return
	}
	l := la.lookup
	l.models.FreeAll(la.kernel, modelUser{lookup: l})
	l.textures.FreeAll(la.kernel, textureUser{resources: la.resources})
	// Scene's own report keys are the kernel's rather than either cache's - a
	// selector key hangs off a path with a '#', and so does an embedded
	// picture's - so both families are cleared as families, in the one prefix
	// scan an unload is allowed.
	la.kernel.ForgetReportedErrors(func(reported string) bool {
		return strings.HasPrefix(reported, modelReportPrefix) ||
			strings.HasPrefix(reported, textureReportPrefix)
	})
}

// clearTextureReports drops one path's texture report key and every embedded
// image's key hanging off it, so a picture that was broken, was fixed and is
// asked for again reports again if it breaks again.
//
// It is the string-keyed half of report-once, and it is here rather than inside
// the cache because this namespace is the one dedup no cache can express: one
// broken image bound in two colour spaces is two entries and one fact. The
// cache forgets its own keys - the descriptors the Library reported its read
// failures under - inside FreeWhere.
func (l *Lookup) clearTextureReports(k kernel.Kernel, path string) {
	texture := textureReportKey(path)
	prefix := texture + "#"
	k.ForgetReportedErrors(func(reported string) bool {
		return reported == texture || strings.HasPrefix(reported, prefix)
	})
}

// clearModelReports drops one model's report key and every selector key hanging
// off it, so a path that failed, was unloaded and fails again reports again.
//
// The selector keys carry the selector after a '#' precisely so that two typo'd
// names in one file are two reports; the cost is that clearing them is a prefix
// scan rather than one delete, which is what ForgetReportedErrors is for. It
// runs on unload only, which is the cold path that method asks for.
//
// The cache forgets its own key - the descriptor the Library reported the read
// failure under - inside Free, which is the other half of report-once and is
// why Free takes a kernel at all.
func (l *Lookup) clearModelReports(k kernel.Kernel, key string) {
	model := modelReportKey(key)
	prefix := model + "#"
	k.ForgetReportedErrors(func(reported string) bool {
		return reported == model || strings.HasPrefix(reported, prefix)
	})
}
