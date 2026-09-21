package types

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/assets"
)

// blobType is assets.Blob, the static byte run Storable admits by identity.
var blobType = reflect.TypeFor[assets.Blob]()

// componentClass is one Component type as registration left it: how wide one
// row is, and the two baked closures that declare a lock on its Store and hand
// back the erased view a Query fills from.
//
// The closures exist because a generic cannot be instantiated from a
// reflect.Type. RegisterComponent[C] is generic, so the generic call is written
// once where C is a compile-time type and kept here; registration-time
// reflection looks it up by reflect.Type afterwards and never needs the
// instantiation at all.
type componentClass struct {
	// store is the *Store[C] itself, for the handles that keep a pointer to it
	// at registration: a writer's check of the Store's watched kinds, and a
	// Hooks reader adding its kinds to them. Nothing reaches it while the engine
	// runs.
	store any
	// header is the same Store erased, for a writer's row copy for Changed,
	// which knows the Store only as bytes and holds its write lock whenever it
	// reads through this.
	header *storeHeader
	size   uintptr
	// trivial is the pointer-free answer for this Component type, and it is a
	// fast-path selector rather than a gate. A trivial row is copied by the
	// sized moves in fill, is left where it lies by a swap-remove, and sits in
	// a span the mark phase never walks. A non-trivial one takes copyValue and
	// is zeroed on removal, and both of those are correctness rather than
	// tuning: see copyValue.
	trivial bool
	// lists is every List within the Component, found once here: the offset
	// its backing array pointer sits at, and the Lists within its elements. It
	// is what validation mode stamps from, and it is nil for the overwhelming
	// majority of Components.
	lists []listSite
	// owner is the Component type's name, kept because the only place it is
	// wanted is a diagnostic and reaching back for a reflect.Type there would
	// mean keeping one on the hot struct.
	owner string
	// copyValue is the typed copy a read field's fill takes when the Component
	// is not trivial, and it exists for the garbage collector rather than for
	// speed. fill's sized moves write a row's bytes through an unsafe.Pointer,
	// which emits no write barrier; for a pointer-free row that is sound and
	// measured, and for a row holding a string or a List it is a pointer store
	// the collector never sees. This closure is an ordinary typed assignment,
	// so the compiler emits the barriers, and it is baked here for the same
	// reason declareSet is: a generic cannot be instantiated from a
	// reflect.Type, but it can be closed over where C is still a type.
	//
	// It is nil when the Component is trivial, which is what keeps the fill of
	// every Component that was legal before this one arrived unchanged.
	copyValue    func(dst, src unsafe.Pointer)
	declareRead  func(access kernel.ResourceAccess) func() *storeHeader
	declareWrite func(access kernel.ResourceAccess) func() *storeHeader
	// declareSet is what a Spawn binds per Component set field: the same write
	// declaration declareWrite makes, and the typed setter baked beside it.
	//
	// The write it declares is redundant for locking and is kept anyway. A Spawn
	// already holds write{*Entities}, which excludes every System in the frame,
	// so naming Store[C] as well adds no exclusion. It is kept as an *ownership*
	// declaration: it is what makes cog's composition check fire, so a plugin
	// spawning a Health must declare a dependency on Health's owner. Dropping it
	// would let any plugin fabricate any other plugin's Components with no
	// declared relationship, which is a bigger hole than the redundancy is a
	// cost.
	//
	// The field it returns carries a second setter that also records the Spawn
	// in the Store's Hook log, and the gate that chooses between them when a run
	// starts. The offset is the Spawn's to fill.
	declareSet func(access kernel.ResourceAccess) spawnField
}

// RegisterComponent declares that C is a Component of this world, and is the
// only thing that makes a Store for it exist. It checks the legality rule,
// creates the Store, enrols it with the authority so a despawn can empty it,
// hands it to the kernel as an ordinary resource of type *Store[C] — owned by
// the calling plugin — and bakes the per-type closures a Query is later planned
// against.
//
// Register a Component in the plugin that defines its Go type. That is what
// keeps the coupling check working on Component data: the Store is owned by
// that plugin, so a System elsewhere that locks it must declare a dependency on
// it, which the Go import graph already forces. Ownership by ecs would not make
// the check lenient, it would make it vacuous.
//
// m.Transform is the one exception, and it is vacuous on purpose. Where an
// Entity stands is read by every binding, so it is declared in libs/m and its
// Store is registered by the ecs plugin itself: one Store, so two Components
// can never describe one position without the scheduler relating them. The
// cost is that every plugin with Systems already depends on ecs, so a System
// writing m.Transform without declaring anything else is never caught at
// composition. Order its writers deliberately.
//
// Registration is explicit rather than derived, and half of that is forced: a
// Lock must bind every handle it will use and a declared resource with no
// initial value fails finalisation, so a Store that does not exist at
// registration cannot be locked and discovery-on-first-use is off the table.
//
// The world is the ecs plugin's *Entities, read through Registrar.Dependency, so
// the calling plugin must declare a dependency on ecs.
//
// ids is the peak population hint the Store reserves for; it is not a cap.
//
// It panics if C names mutable indirection, naming the offending field by path.
// The plugin boundary turns that into a composition failure naming the plugin.
func RegisterComponent[C any](registrar *kernel.Registrar, ids uint32) *Store[C] {
	en, err := registrar.Dependency[*Entities]()
	if err != nil {
		// The plugin boundary turns this into a composition failure naming the
		// plugin, the same way it does the Storable refusal below.
		panic(err)
	}
	componentType := reflect.TypeFor[C]()
	if err := Storable(componentType); err != nil {
		panic("ecs: " + err.Error())
	}
	store := NewStore[C](en, ids)
	registrar.InitResource(store)
	trivial := PointerFree(componentType) == nil
	class := &componentClass{
		store:   store,
		header:  store.erase(),
		size:    componentType.Size(),
		trivial: trivial,
		lists:   listSites(componentType),
		owner:   kernel.TypeName(componentType),
		declareRead: func(access kernel.ResourceAccess) func() *storeHeader {
			handle := access.GetRead[*Store[C]]()
			return func() *storeHeader { return handle.Get().erase() }
		},
		declareWrite: func(access kernel.ResourceAccess) func() *storeHeader {
			handle := access.GetWrite[*Store[C]]()
			return func() *storeHeader { return handle.Get().erase() }
		},
		// The value arrives as an address into the spawning handler's staging
		// buffer rather than as a C, because the caller holds the Component set
		// only as bytes at an offset: the deref here is where the Component's type
		// comes back, and it is sound because the offset was taken from the same
		// reflect.Type this class was baked for.
		declareSet: func(access kernel.ResourceAccess) spawnField {
			handle := access.GetWrite[*Store[C]]()
			return spawnField{
				set: func(e Entity, value unsafe.Pointer) {
					handle.Get().Set(e, *(*C)(value))
				},
				recorded: func(e Entity, value unsafe.Pointer) {
					handle.Get().setRecorded(e, *(*C)(value))
				},
				hooks: hookGate{watch: &store.watch, mask: recordsSpawn},
			}
		},
	}
	if !trivial {
		class.copyValue = func(dst, src unsafe.Pointer) { *(*C)(dst) = *(*C)(src) }
	}
	en.declare(componentType, class)
	return store
}

// Storable reports whether a type may be a Component. It is the registration
// gate, and the rule it checks is:
//
//	A Component contains no mutable indirection, transitively.
//
// Every pointer a Component holds, it holds to memory nothing can write. That
// admits numerics, bools, fixed-size arrays, Entity, structs of those, string,
// assets.Blob and m.List[T]; it refuses pointers, slices, maps, channels, funcs,
// interfaces and sync types.
//
// The rule it replaced was "a Component contains no pointers, transitively",
// which is a stronger statement than the design ever needed. Two of the three
// reasons given for it survive a string untouched. Copying: a string copy stays
// meaningful after the thing it was copied from is gone, because the bytes are
// immutable and the header keeps them alive. Serialisation: a string is
// trivially serialisable, and the only thing it costs a future encoder is that
// a row stops being a fixed width. The third reason, the collector, was always
// the thin one and is measured in the spec.
//
// What no longer survives unstated is the reason the rule really carried, which
// is the lock unit. A read yields a copy, and that is what makes read{C}
// sound for concurrent readers — but only where the copy is not itself a write
// handle. A string's is not. A []T's is: it shares the backing array, so a
// System holding nothing but read{C} could write the Store through it and no
// lock anywhere would name the write. That is why string is admitted outright
// and a slice is admitted only as a List, whose backing array is unexported and
// whose one mutator is checked. See list.go.
//
// assets.Blob is the one exception to that, and it is admitted on trust rather
// than on a property. A Blob is a pointer and a length whose contract is that
// nothing writes the bytes after construction, which is exactly the property a
// string has by construction - so a Blob honouring its contract is as safe to
// hand a reader as a string is. Nothing here can check the contract: Data()
// hands out a live slice, a write through it is an ordinary slice write with no
// method in front of it, and validation mode does not see it. What the shape
// does buy is that both fields are unexported, so a holder can read the run and
// cannot repoint it. It is admitted anyway because the engine's bytes - a
// texture's pixels, a buffer's contents, a parameter's raw layout - are static
// in practice, and a List would copy them on construction for a guarantee
// nothing downstream uses. Conversion is still free - NewBlob wraps a []byte
// without copying it and Data() unwraps one - but it is no longer implicit: a
// []byte does not assign to a Blob, so every holder names the conversion where
// it builds one. It is recognised by type identity, so a caller's own named
// []byte is still a slice and is still refused.
//
// The error names the offending field by path, because the field that fails is
// usually several structs down and naming only the Component is useless:
//
//	ecs: proto.PathedDrawable.Handle is a ptr, which is mutable indirection
//
// Variable-length data still has better answers than a List in most cases: a
// child entity with an owning reference, or a fixed-capacity array where the
// bound is small and real. Both cost the collector nothing at all, which a List
// does not.
func Storable(t reflect.Type) error { return storable(t, kernel.TypeName(t)) }

func storable(t reflect.Type, path string) error {
	if t == blobType {
		return nil
	}
	if isList(t) {
		return storable(listElem(t), path+"[_]")
	}
	switch t.Kind() {
	case reflect.String:
		// The one pointer a Component may hold outright, and it is admitted for
		// a property rather than as an exception: there is no operation on a
		// copy of a string that writes through to its bytes, so a read handing
		// one out hands out nothing a reader can use to mutate the Store.
		return nil
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return nil
	case reflect.Array:
		return storable(t.Elem(), path+"[_]")
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if err := storable(field.Type, path+"."+field.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf(
			"%s is a %s, which is mutable indirection: a Component may hold a pointer only to memory nothing can write, so a string is admitted, static bytes belong in an assets.Blob, a variable-length run belongs in an m.List, and everything else is a child Entity",
			path, t.Kind())
	}
}

// PointerFree reports whether a type contains no pointers, transitively. It was
// the registration gate and is now the fast-path selector [Storable] consults
// once per Component type: a pointer-free row is copied by the sized moves in
// fill, is left where it lies by a swap-remove, and sits in a span the mark
// phase never walks, while a row holding a string or a List takes a typed copy
// and is zeroed on removal.
//
// It is exported because the property is worth asserting about your own types.
// A Component that answers yes here is the cheapest thing this package can
// store, and nothing in the relaxed rule makes it less so.
//
// It forbids pointers, slices, maps, channels, funcs, interfaces and strings,
// and permits numerics, bools, fixed-size arrays, Entity, and structs of those.
func PointerFree(t reflect.Type) error { return pointerFree(t, kernel.TypeName(t)) }

func pointerFree(t reflect.Type, path string) error {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return nil
	case reflect.Array:
		return pointerFree(t.Elem(), path+"[_]")
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if err := pointerFree(field.Type, path+"."+field.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf(
			"%s is a %s, which is not pointer-free: a pointer-free Component carries no pointer of any kind, transitively",
			path, t.Kind())
	}
}
