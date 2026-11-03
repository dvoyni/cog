package ecs

import (
	"fmt"
	"iter"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// Keyed is a value of keyed refs, which answers the key that finds its ref:
// a mesh its asset path, a text itself.
type Keyed[K comparable] interface {
	Key() K
}

// Text is a string keyed by itself, for keyed refs of text: names, tags and
// labels that many components share.
type Text string

// Key answers t.
func (t Text) Key() Text {
	return t
}

// keyMap is the refs of each key of keyed refs.
type keyMap[K comparable, T Keyed[K]] map[K]Ref[T]

// KeyedRefs writes world W's refs holding T, each found by its value's key:
// there is at most one ref per key. It holds the write on them and a read on
// the world.
type KeyedRefs[W WorldID, K comparable, T Keyed[K]] struct {
	refs *refsResource[W, T]
	keys map[K]Ref[T]
}

// KeyedRefsLookup reads world W's keyed refs holding T. It holds a read on
// them and on the world, so several lookups run in parallel.
type KeyedRefsLookup[W WorldID, K comparable, T Keyed[K]] struct {
	refs *refsResource[W, T]
	keys map[K]Ref[T]
}

// RegisterKeyedRefs registers world W's refs holding T under name, found by
// key K, a kernel resource of their own. It is called in a plugin's Init,
// after W is registered.
//
// It is a composition error, and panics, for W not to be registered, for
// refs of T or name to be registered in W twice, plain or keyed, or for the
// allocation range to be empty.
func RegisterKeyedRefs[W WorldID, K comparable, T Keyed[K]](configurator *kernel.Configurator, name RefName, options RefsOptions) {
	registerRefs[W, T](configurator, name, options).keys = keyMap[K, T]{}
}

// Declare declares the write on W's keyed refs of T and a read on world W.
// It is a composition error, and panics, for the refs not to be keyed by K.
func (KeyedRefs[W, K, T]) Declare(access *kernel.Access) KeyedRefs[W, K, T] {
	access.Read[*worldResource[W]]()
	refs := access.Write[*refsResource[W, T]]().Get()
	return KeyedRefs[W, K, T]{refs: refs, keys: keysOf[W, K](refs)}
}

// Put returns the ref of value's key, or stores value in a new ref for it.
// A ref found keeps its value: Update replaces it. It answers the zero Ref
// once the allocation range is used up.
func (r KeyedRefs[W, K, T]) Put(value T) Ref[T] {
	key := value.Key()
	if h, ok := r.keys[key]; ok {
		return h
	}
	h := r.refs.new(value)
	if h != (Ref[T]{}) {
		r.keys[key] = h
	}
	return h
}

// PutAt stores value in a ref at h's index and generation, for mirrors and
// for loading a save, found by its key from then on. h's index must lie
// outside the allocation range and hold no live ref, nor one released this
// window, and no ref may have value's key.
func (r KeyedRefs[W, K, T]) PutAt(h Ref[T], value T) error {
	key := value.Key()
	if _, ok := r.keys[key]; ok {
		return fmt.Errorf("ecs: placing ref %d:%d, whose key %v has a ref", h.index, h.generation, key)
	}
	if err := r.refs.place(h, value); err != nil {
		return err
	}
	r.keys[key] = h
	return nil
}

// Update stores value in the ref of its key, and reports whether there is
// one. DeltaRefs reports the ref changed; the components holding it do not
// change.
func (r KeyedRefs[W, K, T]) Update(value T) bool {
	return updateKeyed(r.refs, r.keys, value)
}

// Find answers the ref of key, or false if there is none.
func (r KeyedRefs[W, K, T]) Find(key K) (Ref[T], bool) {
	h, ok := r.keys[key]
	return h, ok
}

// Get returns the value of ref h, or nil if h is released or zero. Writing
// through it is not a change: Update is. The value's key must not change
// through it: the ref stays found by its old key, which a release then never
// frees, and the release frees the new key instead, from whichever ref has
// it.
func (r KeyedRefs[W, K, T]) Get(h Ref[T]) *T {
	return r.refs.get(h)
}

// Alive reports whether ref h is alive.
func (r KeyedRefs[W, K, T]) Alive(h Ref[T]) bool {
	return r.refs.slot(h) != nil
}

// Release frees ref h and its key, and reports whether it was alive. Every
// handle to it reads nothing at once, and a Put of its key makes a new ref.
func (r KeyedRefs[W, K, T]) Release(h Ref[T]) bool {
	return releaseKeyed(r.refs, r.keys, h)
}

// ReleaseKey frees the ref of key, as Release does, and reports whether
// there was one.
func (r KeyedRefs[W, K, T]) ReleaseKey(key K) bool {
	h, ok := r.keys[key]
	return ok && releaseKeyed(r.refs, r.keys, h)
}

// All yields every live ref and its value, in index order. Releasing one
// during the walk moves no other; one put during it may or may not be
// yielded.
func (r KeyedRefs[W, K, T]) All() iter.Seq2[Ref[T], *T] {
	return r.refs.all()
}

// Declare declares a read on W's keyed refs of T and on world W. It is a
// composition error, and panics, for the refs not to be keyed by K.
func (KeyedRefsLookup[W, K, T]) Declare(access *kernel.Access) KeyedRefsLookup[W, K, T] {
	access.Read[*worldResource[W]]()
	refs := access.Read[*refsResource[W, T]]().Get()
	return KeyedRefsLookup[W, K, T]{refs: refs, keys: keysOf[W, K](refs)}
}

// Find answers the ref of key, or false if there is none.
func (l KeyedRefsLookup[W, K, T]) Find(key K) (Ref[T], bool) {
	h, ok := l.keys[key]
	return h, ok
}

// Get returns the value of ref h, or nil if h is released or zero, to read
// only: other lookups may read it at the same time.
func (l KeyedRefsLookup[W, K, T]) Get(h Ref[T]) *T {
	return l.refs.get(h)
}

// Alive reports whether ref h is alive.
func (l KeyedRefsLookup[W, K, T]) Alive(h Ref[T]) bool {
	return l.refs.slot(h) != nil
}

// All yields every live ref and its value, to read only, in index order.
func (l KeyedRefsLookup[W, K, T]) All() iter.Seq2[Ref[T], *T] {
	return l.refs.all()
}

// keysOf answers the key map of refs, panicking if they are not keyed by K.
func keysOf[W WorldID, K comparable, T Keyed[K]](refs *refsResource[W, T]) map[K]Ref[T] {
	keys, ok := refs.keys.(keyMap[K, T])
	if !ok {
		panic(fmt.Sprintf("ecs: refs of %v in world %v are not keyed by %v", reflect.TypeFor[T](), reflect.TypeFor[W](), reflect.TypeFor[K]()))
	}
	return keys
}

// updateKeyed stores value in the ref of its key in refs, and reports
// whether there is one.
func updateKeyed[W WorldID, K comparable, T Keyed[K]](refs *refsResource[W, T], keys map[K]Ref[T], value T) bool {
	h, ok := keys[value.Key()]
	if !ok {
		return false
	}
	slot := refs.slot(h)
	slot.value = value
	refs.markChanged(h, slot)
	return true
}

// releaseKeyed frees ref h of refs and its key, and reports whether it was
// alive.
func releaseKeyed[W WorldID, K comparable, T Keyed[K]](refs *refsResource[W, T], keys map[K]Ref[T], h Ref[T]) bool {
	refs.settleReservations()
	slot := refs.slot(h)
	if slot == nil {
		return false
	}
	delete(keys, slot.value.Key())
	return refs.release(h)
}
