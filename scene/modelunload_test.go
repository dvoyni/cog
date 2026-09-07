package scene

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
)

// residentSkinnedModel loads the skinned fixture and waits for residency. Its
// baked poses are what makes an unload observable without a GPU: TotalPoseBytes
// triggers no load of its own, so reading zero after an unload is the freeing
// rather than a query racing a reload.
func residentSkinnedModel(t testing.TB) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(*OpQueue) {})
	h.lookup(func(la LookupAccess) { la.Preload(modelPath) })
	h.frameUntil(t, "the model to become resident", func() bool {
		var state ModelState
		h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
		return state == ModelResident
	})
	return h
}

func (h *harness) totalPoseBytes() int {
	var total int
	h.lookup(func(la LookupAccess) { total = la.TotalPoseBytes() })
	return total
}

// An unload lands at the frame boundary, not at the call: the frame that asked
// for it has already recorded draws against the model, and freeing under them
// is exactly the dangle the deferral exists to prevent.
func TestUnloadModelAppliesAtTheFrameBoundary(t *testing.T) {
	h := residentSkinnedModel(t)
	if h.totalPoseBytes() == 0 {
		t.Fatal("the skinned fixture should have baked poses to free")
	}
	h.lookup(func(la LookupAccess) { la.UnloadModel(modelPath) })
	if h.totalPoseBytes() == 0 {
		t.Fatal("the unload landed inside the caller's own frame")
	}
	h.frame()
	if got := h.totalPoseBytes(); got != 0 {
		t.Fatalf("pose bytes = %d after the boundary, want the model freed", got)
	}
}

// A path that has been unloaded is missing, not failed: the next draw or query
// loads it again, which is what makes unload the retry lever.
func TestAnUnloadedPathReloadsOnTheNextQuery(t *testing.T) {
	h := residentSkinnedModel(t)
	h.lookup(func(la LookupAccess) { la.UnloadModel(modelPath) })
	h.frame()
	var state ModelState
	h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
	if state != ModelLoading {
		t.Fatalf("State after unload = %v, want the query to have started a fresh load", state)
	}
	h.frameUntil(t, "the model to come back", func() bool {
		h.lookup(func(la LookupAccess) { state = la.State(modelPath) })
		return state == ModelResident
	})
	if h.totalPoseBytes() == 0 {
		t.Fatal("the reload should have baked the poses again")
	}
}

// failed clears only on unload. Without that there is no way back from a typo
// at all, because a failed path enqueues nothing forever.
func TestUnloadClearsAFailedPathSoItRetries(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	const missing = "models/absent.glb"
	h.lookup(func(la LookupAccess) { la.Preload(missing) })
	h.frameUntil(t, "the load to fail", func() bool {
		var state ModelState
		h.lookup(func(la LookupAccess) { state = la.State(missing) })
		return state == ModelFailed
	})
	h.lookup(func(la LookupAccess) { la.UnloadModel(missing) })
	h.frame()
	var state ModelState
	h.lookup(func(la LookupAccess) { state = la.State(missing) })
	if state == ModelFailed {
		t.Fatal("the unload should have let the path retry")
	}
}

// An invalid path is recorded under the string the caller passed, so unloading
// that same string is what clears it - which is the recorded recovery from a
// typo once the string is fixed.
func TestUnloadClearsAnInvalidPath(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*OpQueue) {})
	const bad = "../escape.glb"
	h.lookup(func(la LookupAccess) { la.State(bad) })
	h.lookup(func(la LookupAccess) { la.UnloadModel(bad) })
	h.frame()
	var state ModelState
	h.lookup(func(la LookupAccess) { state = la.State(bad) })
	if state != ModelFailed {
		t.Fatalf("State = %v, want the path validated and failed afresh", state)
	}
	// Afresh means the report key cleared too, so the second failure is
	// visible rather than swallowed by the first.
	if len(h.errors()) != 2 {
		t.Fatalf("reports = %v, want one per validation", h.errors())
	}
}

// Unloading a path nobody has ever asked for is a no-op, and in particular does
// not mint an entry that a later State call would read as anything but missing.
func TestUnloadingAnAbsentPathIsANoOp(t *testing.T) {
	h := residentSkinnedModel(t)
	h.lookup(func(la LookupAccess) { la.UnloadModel("models/never-asked.glb") })
	h.frame()
	if h.totalPoseBytes() == 0 {
		t.Fatal("unloading an absent path freed something else")
	}
	if len(h.errors()) != 0 {
		t.Fatalf("reported %v, want silence", h.errors())
	}
}

// UnloadAll is the whole table at once, and nothing finer: it is the level
// teardown, so every model goes and every cached texture with them.
func TestUnloadAllClearsEveryModel(t *testing.T) {
	h := residentSkinnedModel(t)
	h.lookup(func(la LookupAccess) { la.UnloadAll() })
	h.frame()
	if got := h.totalPoseBytes(); got != 0 {
		t.Fatalf("pose bytes = %d, want everything freed", got)
	}
}

// The generation counter is the whole of the ghost rule: an unload while a load
// is in flight has to make the completing load throw its result away, because
// the entry it would install into belongs to nobody.
func TestAnUnloadWhileLoadingDiscardsTheCompletingLoad(t *testing.T) {
	lookup := newLookup(Config{})
	lookup.models = map[string]*modelEntry{modelPath: {state: ModelLoading, generation: 1}}
	lookup.unloadModels = append(lookup.unloadModels, modelPath)
	lookup.applyUnloads(func(gfx.TextureDescr) {})

	var reported []error
	lookup.installModel(func(err error) { reported = append(reported, err) },
		modelPath, 1, &loadedModel{}, nil, &gfx.ResourceQueue{})

	if entry := lookup.models[modelPath]; entry.state == ModelResident {
		t.Fatal("the completing load installed into an entry the unload had already retired")
	}
	if len(reported) != 0 {
		t.Fatalf("reported %v, want a silent discard", reported)
	}
}

// UnloadModel does not cascade to textures, because with no refcount it cannot
// know whether another resident model shares one by path. The wart is deliberate
// and asserted so nobody quietly fixes it into a use-after-free.
func TestUnloadModelDoesNotFreeTextures(t *testing.T) {
	lookup := newLookup(Config{})
	lookup.models = map[string]*modelEntry{modelPath: {state: ModelResident, generation: 1}}
	lookup.textures = map[textureKey]gfx.TextureDescr{{path: modelPath, image: 0}: {}}
	lookup.unloadModels = append(lookup.unloadModels, modelPath)

	freed := 0
	lookup.applyUnloads(func(gfx.TextureDescr) { freed++ })
	if freed != 0 || len(lookup.textures) != 1 {
		t.Fatalf("freed %d textures and %d remain; want the cache untouched", freed, len(lookup.textures))
	}

	// UnloadTexture is the lever that does free one, and it frees every
	// colour-space variant the path baked, because the path is the whole of
	// what a caller can name.
	lookup.textures[textureKey{path: modelPath, image: 0, srgb: true}] = gfx.TextureDescr{}
	lookup.unloadTextures = append(lookup.unloadTextures, modelPath)
	lookup.applyUnloads(func(gfx.TextureDescr) { freed++ })
	if freed != 2 || len(lookup.textures) != 0 {
		t.Fatalf("freed %d and %d remain; want both variants gone", freed, len(lookup.textures))
	}
}
