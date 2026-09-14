package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
)

// residentSkinnedModel loads the skinned fixture and waits for residency. Its
// baked poses are what makes an unload observable without a GPU: TotalPoseBytes
// triggers no load of its own, so reading zero after an unload is the freeing
// rather than a query racing a reload.
func residentSkinnedModel(t testing.TB) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(*scene.OpQueue) {})
	h.lookup(func(la scene.LookupAccess) { la.Preload(modelPath) })
	h.frameUntil(t, "the model to become resident", func() bool {
		var state scene.ModelState
		h.lookup(func(la scene.LookupAccess) { state = la.State(modelPath) })
		return state == scene.ModelResident
	})
	return h
}

func (h *harness) totalPoseBytes() int {
	var total int
	h.lookup(func(la scene.LookupAccess) { total = la.TotalPoseBytes() })
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
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
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
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
	h.frame()
	var state scene.ModelState
	h.lookup(func(la scene.LookupAccess) { state = la.State(modelPath) })
	if state != scene.ModelLoading {
		t.Fatalf("State after unload = %v, want the query to have started a fresh load", state)
	}
	h.frameUntil(t, "the model to come back", func() bool {
		h.lookup(func(la scene.LookupAccess) { state = la.State(modelPath) })
		return state == scene.ModelResident
	})
	if h.totalPoseBytes() == 0 {
		t.Fatal("the reload should have baked the poses again")
	}
}

// failed clears only on unload. Without that there is no way back from a typo
// at all, because a failed path enqueues nothing forever.
func TestUnloadClearsAFailedPathSoItRetries(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*scene.OpQueue) {})
	const missing = "models/absent.glb"
	h.lookup(func(la scene.LookupAccess) { la.Preload(missing) })
	h.frameUntil(t, "the load to fail", func() bool {
		var state scene.ModelState
		h.lookup(func(la scene.LookupAccess) { state = la.State(missing) })
		return state == scene.ModelFailed
	})
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(missing) })
	h.frame()
	var state scene.ModelState
	h.lookup(func(la scene.LookupAccess) { state = la.State(missing) })
	if state == scene.ModelFailed {
		t.Fatal("the unload should have let the path retry")
	}
}

// An invalid path is recorded under the string the caller passed, so unloading
// that same string is what clears it - which is the recorded recovery from a
// typo once the string is fixed.
func TestUnloadClearsAnInvalidPath(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*scene.OpQueue) {})
	const bad = "../escape.glb"
	h.lookup(func(la scene.LookupAccess) { la.State(bad) })
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(bad) })
	h.frame()
	var state scene.ModelState
	h.lookup(func(la scene.LookupAccess) { state = la.State(bad) })
	if state != scene.ModelFailed {
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
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel("models/never-asked.glb") })
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
	h.lookup(func(la scene.LookupAccess) { la.UnloadAll() })
	h.frame()
	if got := h.totalPoseBytes(); got != 0 {
		t.Fatalf("pose bytes = %d, want everything freed", got)
	}
}
