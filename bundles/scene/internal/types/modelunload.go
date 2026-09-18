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
	la.lookup.unloadTexture(la.kernel, key, la.resources.ReleaseTexture)
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
	// The model report keys are the kernel's rather than the cache's - a
	// selector key hangs off a path with a '#' - so they are cleared as their
	// own family, in the one prefix scan an unload is allowed.
	la.kernel.ForgetReportedErrors(func(reported string) bool {
		return strings.HasPrefix(reported, "model:")
	})
	for key, texture := range l.textures {
		la.resources.ReleaseTexture(texture)
		delete(l.textures, key)
		la.kernel.ForgetReportedError(textureReportKey(textureReportPath(key)))
	}
}

// unloadTexture frees every texture the cache holds under one path, whatever
// colour space or embedded image index it was keyed by.
func (l *Lookup) unloadTexture(k kernel.Kernel, key string, releaseTexture func(gfx.TextureDescr)) {
	for cached, texture := range l.textures {
		if cached.path != key {
			continue
		}
		releaseTexture(texture)
		delete(l.textures, cached)
		k.ForgetReportedError(textureReportKey(textureReportPath(cached)))
	}
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
