package scene

import (
	"strings"

	"github.com/dvoyni/cog/gfx"
)

// UnloadModel queues one path's geometry, baked poses and material records for
// release at the next frame boundary. Unloading an absent path is a no-op, and
// a later draw or query of an unloaded path loads it again.
//
// It does not cascade to textures. With no refcount the lookup cannot know
// whether another resident model binds the same image by path, and freeing one
// that is still bound is a dead texture in a live bind group rather than a
// missing picture. UnloadTexture is the separate, deliberate lever.
//
// It is also the only retry lever there is. A failed path clears here and
// nowhere else, so recovering from a bad file - or from a path so malformed it
// never reached a load at all - is UnloadModel followed by Preload. There is no
// Retry, because a Retry that did not first free would be a second name for the
// idempotent load that already exists.
func (la LookupAccess) UnloadModel(path string) {
	if !la.Valid() {
		return
	}
	// The raw string is queued rather than the cleaned one, because an invalid
	// path has no cleaned form and is recorded in the table under exactly what
	// the caller passed. modelKey collapses the two cases again at the boundary.
	la.lookup.unloadModels = append(la.lookup.unloadModels, path)
}

// UnloadTexture queues one image path's GPU textures for release at the next
// frame boundary.
//
// Every texture the path baked goes, not one: colour space is part of a
// texture's cache key because it is the binding slot's property rather than the
// image's, so one file bound as both a base colour and a normal map is two GPU
// textures - and path is the whole of what a caller can name. For a glb the
// path names the container, so unloading it releases every image embedded in
// it.
//
// Nothing checks whether a resident model still binds them. This is the lever
// for a texture whose models are already gone, and using it while one is
// resident leaves that model's bind groups pointing at freed textures.
func (la LookupAccess) UnloadTexture(path string) {
	if !la.Valid() {
		return
	}
	la.lookup.unloadTextures = append(la.lookup.unloadTextures, path)
}

// UnloadAll queues every resident model and every cached texture for release at
// the next frame boundary. It is the level teardown, and it is a flag rather
// than a walk because the table it would walk can still change before the
// boundary arrives.
//
// Buffer-built meshes are not in it: those are the caller's own handles, minted
// by BakeMesh and released by ReleaseMesh, and a lookup-wide sweep has no way
// to tell the caller its refs went stale. Nor are scene's own unit meshes, the
// two default textures or the null skin, which are the plugin's and would have
// to be re-baked on the very next frame.
func (la LookupAccess) UnloadAll() {
	if la.Valid() {
		la.lookup.unloadEverything = true
	}
}

// applyUnloads frees everything the frame's callers gave up. It runs at the
// flush, before the frame's own draws are looked at and after the previous
// frame's commands have been submitted, which is the whole reason unloading is
// queued: a model freed at the call would be freed under draws the caller had
// already recorded against it.
//
// Buffers go through the same pending-release list ReleaseMesh uses, so
// drainMeshes frees them in the same pass; textures are freed directly, because
// nothing else in the plugin releases one.
func (l *Lookup) applyUnloads(releaseTexture func(gfx.TextureDescr)) {
	if l.unloadEverything {
		for path := range l.models {
			l.unloadModel(path)
		}
		for key, texture := range l.textures {
			releaseTexture(texture)
			delete(l.textures, key)
			delete(l.reported, textureReportKey(textureReportPath(key)))
		}
		l.unloadEverything = false
	}
	for _, path := range l.unloadModels {
		key, _ := modelKey(path)
		l.unloadModel(key)
	}
	l.unloadModels = l.unloadModels[:0]
	for _, path := range l.unloadTextures {
		key, ok := modelKey(path)
		if !ok {
			continue
		}
		l.unloadTexture(key, releaseTexture)
	}
	l.unloadTextures = l.unloadTextures[:0]
}

// unloadModel retires one table slot: its meshes and animation buffers are
// queued for release, its report keys are cleared, and the entry is reset to
// missing with a bumped generation.
//
// The slot is reset rather than deleted, and that is what the generation
// counter is for. A deleted slot would be re-minted at generation one by the
// next draw, and a load still in flight from before the unload would then match
// it and install as a ghost - a model nobody asked for, holding buffers nobody
// will free.
func (l *Lookup) unloadModel(key string) {
	entry, ok := l.models[key]
	if !ok {
		return
	}
	for _, ref := range entry.meshes {
		l.releaseMesh(ref)
	}
	animation := &entry.animation
	if animation.poseBytes > 0 {
		l.pendingReleases = append(l.pendingReleases, animation.poses, animation.skinJoints)
	}
	if animation.morphBytes > 0 {
		l.pendingReleases = append(l.pendingReleases, animation.morphDeltas)
	}
	l.clearModelReports(key)
	*entry = modelEntry{generation: entry.generation + 1}
}

// unloadTexture frees every texture the cache holds under one path, whatever
// colour space or embedded image index it was keyed by.
func (l *Lookup) unloadTexture(key string, releaseTexture func(gfx.TextureDescr)) {
	for cached, texture := range l.textures {
		if cached.path != key {
			continue
		}
		releaseTexture(texture)
		delete(l.textures, cached)
		delete(l.reported, textureReportKey(textureReportPath(cached)))
	}
}

// clearModelReports drops one model's report key and every selector key hanging
// off it, so a path that failed, was unloaded and fails again reports again.
//
// The selector keys carry the selector after a '#' precisely so that two typo'd
// names in one file are two reports; the cost is that clearing them is a prefix
// scan rather than one delete. It runs on unload only, which is a cold path.
func (l *Lookup) clearModelReports(key string) {
	delete(l.reported, modelReportKey(key))
	prefix := modelReportKey(key) + "#"
	for reported := range l.reported {
		if strings.HasPrefix(reported, prefix) {
			delete(l.reported, reported)
		}
	}
}
