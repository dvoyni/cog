package internal

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// read runs fn over the read facade, built over the Lookup the harness holds.
func (h *harness) read(fn func(model.LookupReadAccess)) {
	h.model(func(lookup *model.Lookup, _ kernel.Kernel, _ fs.FS, _ *gfx.ResourceQueue) {
		fn(model.NewLookupReadAccess(lookup))
	})
}

// The read facade never loads. Asked about a model that is on disk and was
// never drawn, it answers absent, and asking opens no file, bakes no pose and
// leaves the model absent for the next reader too.
func TestTheReadFacadeReportsAnUnloadedModelAbsentAndStartsNoLoad(t *testing.T) {
	files := counting(modelFiles(glb(t, skinnedModel(t))))
	h := newHarnessWithFS(t, files, func(*OpQueue) {})
	ref := model.ModelRef{Path: modelPath}
	for range 2 {
		h.read(func(ra model.LookupReadAccess) {
			if handle, ok := ra.Handle(ref); ok {
				t.Errorf("Handle = %d, want a model never loaded reported absent", handle)
			}
			if _, _, ok := ra.View(1, "", ""); ok {
				t.Error("View(1) answered, want an empty table to hold no model")
			}
		})
	}
	if got := files.opens[modelPath]; got != 0 {
		t.Errorf("the model was opened %d times, want the read facade to start no load", got)
	}
	if got := h.totalPoseBytes(); got != 0 {
		t.Errorf("pose bytes = %d, want nothing baked", got)
	}
}

// A ModelRef resolves to its handle once, through the load facade, and every
// later read by that handle is the view the load facade itself answers with:
// the same primitives, the same materials and the same animation, not copies.
func TestAModelRefResolvesToAHandleOnceAndReadsByHandleReturnTheSameView(t *testing.T) {
	files := counting(modelFiles(glb(t, skinnedModel(t))))
	h := newHarnessWithFS(t, files, func(*OpQueue) {})
	ref := model.ModelRef{Path: modelPath}
	var handle model.ModelHandle
	var loaded model.ModelView
	h.model(func(lookup *model.Lookup, k kernel.Kernel, fsys fs.FS, resources *gfx.ResourceQueue) {
		var ok bool
		if handle, ok = lookup.Resolve(k, fsys, resources, ref); !ok || handle == 0 {
			t.Fatalf("Resolve = %d, %v, want the model loaded and a handle", handle, ok)
		}
		var err model.ModelSelectorError
		loaded, err, ok = lookup.ModelView(k, fsys, resources, ref.Path, "", "")
		if !ok || err != nil {
			t.Fatalf("ModelView = %v, %v, want the resident model", err, ok)
		}
	})
	h.device(func(la model.LookupDeviceAccess) {
		if again, ok := la.Resolve(ref); !ok || again != handle {
			t.Errorf("a second Resolve = %d, %v, want the same handle %d", again, ok, handle)
		}
	})
	if got := files.opens[modelPath]; got != 1 {
		t.Errorf("the model was opened %d times, want one load", got)
	}
	for range 2 {
		h.read(func(ra model.LookupReadAccess) {
			if got, ok := ra.Handle(ref); !ok || got != handle {
				t.Errorf("the read facade's Handle = %d, %v, want %d", got, ok, handle)
			}
			view, err, ok := ra.View(handle, "", "")
			if !ok || err != nil {
				t.Fatalf("View by handle = %v, %v, want the resident model", err, ok)
			}
			if len(view.Primitives) == 0 || len(view.Primitives) != len(loaded.Primitives) ||
				&view.Primitives[0] != &loaded.Primitives[0] ||
				len(view.Materials) != len(loaded.Materials) ||
				(len(view.Materials) > 0 && &view.Materials[0] != &loaded.Materials[0]) ||
				view.Animation != loaded.Animation || view.NeverCull != loaded.NeverCull {
				t.Error("the view by handle is not the view the load facade answered with")
			}
			record, ok := ra.Mesh(view.Primitives[0].Mesh)
			if !ok || record.VertexCount == 0 {
				t.Errorf("Mesh of the view's first primitive = %+v, %v, want its resident record", record, ok)
			}
		})
	}
}

// Unloading gives the handle's slot back. The handle then reads as absent, and
// the model's path does too, until the load facade loads it again.
func TestAnUnloadedModelsHandleReadsAbsent(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, onePrimitiveModel(t))), func(*OpQueue) {})
	ref := model.ModelRef{Path: modelPath}
	var handle model.ModelHandle
	h.device(func(la model.LookupDeviceAccess) { handle, _ = la.Resolve(ref) })
	h.lookup(func(la model.LookupAccess) { la.UnloadModel(modelPath) })
	h.read(func(ra model.LookupReadAccess) {
		if ra.Resident(handle) {
			t.Error("the handle still names a resident model after its unload")
		}
		if _, ok := ra.Handle(ref); ok {
			t.Error("the unloaded path still has a handle")
		}
	})
}

// A path that fails to load is cached as failed, and is not resident: the read
// facade has no handle for it and the load facade resolves none.
func TestAFailedModelHasNoHandle(t *testing.T) {
	h := newHarnessWithFiles(t, fstest.MapFS{modelPath: {Data: []byte("not a model")}}, func(*OpQueue) {})
	ref := model.ModelRef{Path: modelPath}
	h.device(func(la model.LookupDeviceAccess) {
		if handle, ok := la.Resolve(ref); ok {
			t.Errorf("Resolve = %d, want a failed load to resolve to no handle", handle)
		}
	})
	h.read(func(ra model.LookupReadAccess) {
		if _, ok := ra.Handle(ref); ok {
			t.Error("the failed path has a handle")
		}
	})
}
