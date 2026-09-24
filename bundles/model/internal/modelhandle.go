package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// ModelHandle is a plain slot index into the Lookup's dense model table. A
// ModelRef resolves to one once, through the load facade, and from then on a
// read is one index: no path clean and no string hash.
//
// There is no generation. A handle goes stale only when its model is
// unloaded, and drawing an unloaded model is undefined behaviour, so a slot a
// free gave back is simply reissued to the next model loaded. The zero value
// is no model, because slot 0 is never issued.
type ModelHandle uint32

// admit gives a model that has just been installed its slot in the dense model
// table, reusing the slot a free gave back most recently. It runs inside Load,
// so only a model that is resident in full ever holds a handle.
func (l *Lookup) admit(key string, model *residentModel) {
	var handle ModelHandle
	if n := len(l.freeHandles); n > 0 {
		handle = l.freeHandles[n-1]
		l.freeHandles = l.freeHandles[:n-1]
		l.table[handle] = model
	} else {
		handle = ModelHandle(len(l.table))
		l.table = append(l.table, model)
	}
	model.key, model.handle = key, handle
	l.handles[key] = handle
}

// evict gives a freed model's slot back. A model that failed to load was never
// admitted and has nothing to give back.
func (l *Lookup) evict(model *residentModel) {
	if model.handle == 0 {
		return
	}
	l.table[model.handle] = nil
	delete(l.handles, model.key)
	l.freeHandles = append(l.freeHandles, model.handle)
	model.handle = 0
}

// resident is the model a handle names, or nil for the zero handle, a handle
// past the table and a slot that is free.
func (l *Lookup) resident(handle ModelHandle) *residentModel {
	if int(handle) >= len(l.table) {
		return nil
	}
	return l.table[handle]
}

// Resolve resolves a ModelRef to the handle of its model, loading the model if
// the cache holds no entry. ok is false when no model is resident for the
// path, and why is State's to report.
//
// Only the path is resolved. A handle names the model, and a draw's Scene and
// Node selectors are applied when the handle is read, so one handle serves
// every selector of one file.
//
// It is the load facade's resolve for a renderer that holds the Lookup for
// writing, the filesystem and the resource queue.
func (l *Lookup) Resolve(
	k kernel.Kernel, fsys fs.FS, resources *gfx.ResourceQueue, ref ModelRef,
) (ModelHandle, bool) {
	model, err := l.model(k, fsys, resources, ref.Path)
	if err != nil {
		return 0, false
	}
	return model.handle, true
}

// Resolve resolves a ModelRef to the handle of its model, loading the model if
// it is not loaded yet, exactly as a draw would. ok is false when no model is
// resident for the path, and State says why.
func (la LookupDeviceAccess) Resolve(ref ModelRef) (ModelHandle, bool) {
	if !la.Valid() {
		return 0, false
	}
	return la.lookup.Resolve(la.kernel, la.fsys, la.resources, ref)
}

// LookupReadAccess is the read facade: everything a draw reads from a
// resident model, by ModelHandle and by MeshRef, under a *Lookup read lock.
//
// It never loads. A model that is not resident is absent, and nothing it
// answers starts a load, bakes a mesh or touches a queue. That is what lets
// readers run side by side: everything it reads is immutable from install to
// unload, so a reader needs excluding only from the load facade, which loads
// and unloads under the write lock, and never from another reader.
//
// Build one with NewLookupReadAccess inside a handler that holds the *Lookup
// read lock, and never store it: the models behind it are valid only while
// that handler holds its lock.
type LookupReadAccess struct {
	lookup *Lookup
}

// NewLookupReadAccess builds the read facade. Call it inside a handler that
// holds the *Lookup read lock, or its write lock.
func NewLookupReadAccess(lookup *Lookup) LookupReadAccess {
	return LookupReadAccess{lookup: lookup}
}

// Valid reports whether the facade is backed by a live Lookup.
func (ra LookupReadAccess) Valid() bool { return ra.lookup != nil }

// Handle reports the handle of the model a ModelRef names, if that model is
// resident. It never loads: a path that was never loaded, failed to load or
// was unloaded is absent, and asking does not change that.
//
// It pays the path clean and one string hash, so a reader that reads the same
// model every frame resolves it once and keeps the handle.
func (ra LookupReadAccess) Handle(ref ModelRef) (ModelHandle, bool) {
	if !ra.Valid() {
		return 0, false
	}
	key, ok := ModelKey(ref.Path)
	if !ok {
		return 0, false
	}
	handle, ok := ra.lookup.handles[key]
	return handle, ok
}

// Resident reports whether a handle names a resident model.
func (ra LookupReadAccess) Resident(handle ModelHandle) bool {
	return ra.Valid() && ra.lookup.resident(handle) != nil
}

// View resolves one draw's Scene and Node selectors on the model a handle
// names: the primitive span, the model materials, NeverCull, the re-root and
// the animation. It is the same view the load facade's ModelView returns for
// the model's path. ok is false for a handle that names no resident model; a
// selector that matches nothing is err, which the caller reports under its
// ReportKey.
func (ra LookupReadAccess) View(
	handle ModelHandle, scene, node string,
) (view ModelView, err ModelSelectorError, ok bool) {
	if !ra.Valid() {
		return ModelView{}, nil, false
	}
	model := ra.lookup.resident(handle)
	if model == nil {
		return ModelView{}, nil, false
	}
	view, err = model.View(model.key, scene, node)
	return view, err, true
}

// Mesh resolves a durable MeshRef to its mesh record: the vertex and index
// buffers, IndexWidth, Topology, Layout, Standard, Indexed, Bounds and UV. A
// ref that is stale, released or not model's own is absent.
func (ra LookupReadAccess) Mesh(ref MeshRef) (MeshRecord, bool) {
	if !ra.Valid() {
		return MeshRecord{}, false
	}
	return ra.lookup.Mesh(ref)
}
