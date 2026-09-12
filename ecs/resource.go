package ecs

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// Read and Write are the System parameters that name a kernel resource, and
// together they are the whole of the binding mechanism: there is no binding
// type, no adapter and no registration call of the ECS's own. A System that
// draws, plays a sound or steps physics reaches that plugin through the
// frame-local resource the plugin already publishes — scene's *scene.OpQueue,
// gfx's *gfx.OpQueue — and the ECS contributes nothing else.
//
//	func recordDraws(
//	    q      *ecs.Query[DrawQ],                  // the Components
//	    models *ecs.Read[*scene.Names],            // a resource, read
//	    out    *ecs.Write[*scene.OpQueue],         // a resource, written
//	) {
//	    table, queue := models.Get(), out.Get()
//	    for _, it := range q.All() { … }
//	}
//
// The lock they declare is the kernel's own, taken at registration like every
// other, so the resource joins the System's lock set beside its Query's Stores
// and is as visible in the signature as a Component is. Two Systems both
// writing one resource therefore serialise against each other whatever their
// Queries touch — which is a property of the bound plugin's API rather than of
// the ECS. Scene publishes one queue, so scene recording is one lock wide, and
// one recording System per bound plugin is the shape.
//
// Neither is a place to keep anything. The value is refreshed per tick and is
// valid only for the body of the System, under the kernel's standing rule that
// a value read from a handle lives only as long as the handler holds its lock.
// That is why Get goes to the cell every call rather than to a copy taken at
// registration: the cell the lock covers is the only sanctioned route.
//
// The one thing they will not name is the ECS's own cells. See guardHandle.
type Read[T any] struct {
	handle kernel.Read[T]
}

// prepare declares the read and binds the handle. It runs once, at
// registration, inside the single call of the System's Lock.
func (r *Read[T]) prepare(_ *Entities, access kernel.ResourceAccess) {
	guardHandle[T]("Read")
	r.handle = access.GetRead[T]()
}

// Get returns the resource for the body of this System call, and for no longer.
func (r *Read[T]) Get() T { return r.handle.Get() }

// Write is Read's writing form, and the one a recording System takes: a draw is
// appended to the queue, so recording is a write however read-only the gameplay
// behind it was. A write lock also authorises reads, so Get is here too.
type Write[T any] struct {
	handle kernel.Write[T]
}

// prepare declares the write and binds the handle. It runs once, at
// registration.
func (w *Write[T]) prepare(_ *Entities, access kernel.ResourceAccess) {
	guardHandle[T]("Write")
	w.handle = access.GetWrite[T]()
}

// Get returns the resource for the body of this System call, and for no longer.
func (w *Write[T]) Get() T { return w.handle.Get() }

// Set replaces the resource value, for the few resources that are reassigned
// wholesale rather than mutated in place.
func (w *Write[T]) Set(value T) { w.handle.Set(value) }

// storeCoreType is what every *Store[T] satisfies and nothing else does, which
// is how guardHandle recognises a Store without knowing its Component type.
var storeCoreType = reflect.TypeFor[storeCore]()

// guardHandle refuses the ECS's own cells to a generic resource handle, and the
// refusal is soundness rather than tidiness.
//
// Store.Remove is exported, so ecs.Read[*Store[T]] would hand a read-locked
// System a mutator: a data race the kernel cannot see, because the lock set says
// read and the code writes. The accessors exist so that never has to happen —
// they take the right lock, they declare read{*Entities} with it, and they check
// the Component was registered at all.
//
// *Entities is refused for the matching reason on the other side: the authority
// arrives as WriteableEntities, which is the only thing that can retire an
// Entity and the only place that capability is visible in a signature.
func guardHandle[T any](handle string) {
	resourceType := reflect.TypeFor[T]()
	if resourceType == reflect.TypeFor[*Entities]() {
		panic(fmt.Sprintf(
			"ecs: %s[*ecs.Entities] names the ECS's own authority; a System reads it through its Queries and retires an Entity through *ecs.WriteableEntities",
			handle))
	}
	if resourceType.Implements(storeCoreType) {
		component := componentOfStore(resourceType)
		panic(fmt.Sprintf(
			"ecs: %s[%s] names a Component Store; reach one Component of an Entity through *ecs.Get[%s], *ecs.Set[%s] or *ecs.Remove[%s], which take the right lock and check the Component is registered",
			handle, resourceType, component, component, component))
	}
}

// componentOfStore names the Component a *Store[T] holds, for the diagnostic
// above. It reads the rows field rather than parsing the type's name, and falls
// back to a word rather than panicking inside a panic message.
func componentOfStore(storeType reflect.Type) string {
	if storeType.Kind() != reflect.Pointer {
		return "T"
	}
	rows, ok := storeType.Elem().FieldByName("dense")
	if !ok {
		return "T"
	}
	return rows.Type.Elem().String()
}
