package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
)

// residentSkinnedModel loads the skinned fixture. Its baked poses are what
// makes a free observable without a GPU: TotalPoseBytes triggers no load of its
// own, so reading zero after an unload is the freeing rather than a query
// racing a reload.
func residentSkinnedModel(t testing.TB) *harness {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(*scene.OpQueue) {})
	h.device(func(la scene.LookupDeviceAccess) {
		la.Preload(modelPath)
		if err := la.State(modelPath); err != nil {
			t.Fatalf("Preload left %q unloaded: %v", modelPath, err)
		}
	})
	return h
}

func (h *harness) totalPoseBytes() int {
	var total int
	h.lookup(func(la scene.LookupAccess) { total = la.TotalPoseBytes() })
	return total
}

// An unload frees at the call. There is no queue in front of it any more: the
// entry leaves the cache where the caller stands, and what still lands at the
// frame boundary is only the GPU buffers, through the same pending-release list
// ReleaseMesh uses.
func TestUnloadModelFreesAtTheCall(t *testing.T) {
	h := residentSkinnedModel(t)
	if h.totalPoseBytes() == 0 {
		t.Fatal("the skinned fixture should have baked poses to free")
	}
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
	if got := h.totalPoseBytes(); got != 0 {
		t.Fatalf("pose bytes = %d after the call, want the model freed", got)
	}
}

// A path that has been unloaded is simply absent, and the next draw or query
// loads it again - which is what makes unload the retry lever. A free followed
// by a get is a reload, not an error.
func TestAnUnloadedPathReloadsOnTheNextQuery(t *testing.T) {
	h := residentSkinnedModel(t)
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(modelPath) })
	h.device(func(la scene.LookupDeviceAccess) {
		if err := la.State(modelPath); err != nil {
			t.Fatalf("State after unload = %v, want the query to have loaded it afresh", err)
		}
	})
	if h.totalPoseBytes() == 0 {
		t.Fatal("the reload should have baked the poses again")
	}
}

// A failed load clears only on unload. Without that there is no way back from a
// typo at all, because a failed path is cached as failed and never loads again.
func TestUnloadClearsAFailedPathSoItLoadsAgain(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*scene.OpQueue) {})
	const missing = "models/absent.glb"
	h.device(func(la scene.LookupDeviceAccess) { la.Preload(missing) })
	if len(h.errors()) != 1 {
		t.Fatalf("reports = %v, want the failed read reported once", h.errors())
	}
	h.device(func(la scene.LookupDeviceAccess) { la.Preload(missing) })
	if len(h.errors()) != 1 {
		t.Fatalf("reports = %v, want the second preload to find the cached failure", h.errors())
	}
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(missing) })
	h.device(func(la scene.LookupDeviceAccess) { la.Preload(missing) })
	if len(h.errors()) != 2 {
		t.Fatalf("reports = %v, want the unload to have let the path load and fail again", h.errors())
	}
}

// An invalid path is recorded under the string the caller passed, so unloading
// that same string is what clears it - which is the recorded recovery from a
// typo once the string is fixed. The path itself never enters the cache, so a
// typo stays a typo however many times it is asked.
func TestUnloadClearsAnInvalidPath(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, boundsModel(t))), func(*scene.OpQueue) {})
	const bad = "../escape.glb"
	h.device(func(la scene.LookupDeviceAccess) { la.State(bad) })
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel(bad) })
	var err error
	h.device(func(la scene.LookupDeviceAccess) { err = la.State(bad) })
	if _, ok := err.(scene.ErrModelPathInvalid); !ok {
		t.Fatalf("State = %v, want the path validated and refused afresh", err)
	}
	// Afresh means the report key cleared too, so the second failure is
	// visible rather than swallowed by the first.
	if len(h.errors()) != 2 {
		t.Fatalf("reports = %v, want one per validation", h.errors())
	}
}

// Unloading a path nobody has ever asked for is a no-op, and in particular does
// not disturb anything else in the cache.
func TestUnloadingAnAbsentPathIsANoOp(t *testing.T) {
	h := residentSkinnedModel(t)
	h.lookup(func(la scene.LookupAccess) { la.UnloadModel("models/never-asked.glb") })
	if h.totalPoseBytes() == 0 {
		t.Fatal("unloading an absent path freed something else")
	}
	if len(h.errors()) != 0 {
		t.Fatalf("reported %v, want silence", h.errors())
	}
}

// UnloadAll is the whole cache at once, and nothing finer: it is the level
// teardown, so every model goes and every cached texture with them. It carries
// the device because freeing a texture needs the queue at the call.
func TestUnloadAllClearsEveryModel(t *testing.T) {
	h := residentSkinnedModel(t)
	h.device(func(la scene.LookupDeviceAccess) { la.UnloadAll() })
	if got := h.totalPoseBytes(); got != 0 {
		t.Fatalf("pose bytes = %d, want everything freed", got)
	}
}
