package internal

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// storeCoreType is what every *Store[T] satisfies and nothing else does, which
// is how guardHandle recognises a Store without knowing its Component type.
var storeCoreType = reflect.TypeFor[storeCore]()

// Read and Write are the System parameters that name a kernel resource, and
// together they are the whole of the binding mechanism: there is no binding
// type, no adapter and no registration call of the ECS's own. A System that
// draws, plays a sound or steps physics reaches that plugin through the
// frame-local resource the plugin already publishes — gfx's *gfx.OpQueue, for
// ecsscene — and the ECS contributes nothing else.
//
//	func record(
//	    models *ecs.Query[modelQuery],             // the Components
//	    work   *ecs.Write[*scratch],               // the binding's own resource
//	    out    *ecs.Write[*gfx.OpQueue],           // the resource it draws into
//	) {
//	    s, queue := work.Get(), out.Get()
//	    for e, it := range models.All() { … }
//	}
//
// The lock they declare is the kernel's own, taken at registration like every
// other, so the resource joins the System's lock set beside its Query's Stores
// and is as visible in the signature as a Component is. Two Systems both
// writing one resource therefore serialise against each other whatever their
// Queries touch — which is a property of the bound plugin's API rather than of
// the ECS. gfx publishes one queue, so recording into gfx is one lock wide, and
// one recording System per bound plugin is the shape.
//
// Neither is a place to keep anything. The value is refreshed per tick and is
// valid only for the body of the System, under the kernel's standing rule that
// a value read from a handle lives only as long as the handler holds its lock.
// So Get returns the value resolved from the cell the lock covers at the start
// of the invocation, never one taken at registration: the cell is read once an
// invocation, just before the System's func, and every Get in the body returns
// that. See resolver.
//
// A Set on a Write updates that parameter's own resolved value, so a Get on the
// same parameter reads it back. Another parameter naming the same resource in
// the same signature — a second Write, or a Read beside a Write — does not see
// that Set until the next invocation: it returns what it resolved at the start
// of this one.
//
// The one thing they will not name is the ECS's own cells. See guardHandle.
type Read[T any] struct {
	handle kernel.Read[T]
	// resolved is handle's value for the current invocation.
	resolved T
}

// prepare declares the read and binds the handle. It runs once, at
// registration, inside the single call of the System's Lock.
func (r *Read[T]) prepare(_ *Entities, access kernel.ResourceAccess) {
	guardHandle[T]("Read")
	r.handle = access.GetRead[T]()
}

// resolve reads the resource out of its cell for this invocation.
func (r *Read[T]) resolve() { r.resolved = r.handle.Get() }

// Get returns the resource for the body of this System call, and for no longer:
// the value resolved at the start of the invocation.
func (r *Read[T]) Get() T { return r.resolved }

// Write is Read's writing form, and the one a recording System takes: a draw is
// appended to the queue, so recording is a write however read-only the gameplay
// behind it was. A write lock also authorises reads, so Get is here too.
type Write[T any] struct {
	handle kernel.Write[T]
	// resolved is handle's value for the current invocation, and what this
	// parameter's last Set wrote.
	resolved T
}

// prepare declares the write and binds the handle. It runs once, at
// registration.
func (w *Write[T]) prepare(_ *Entities, access kernel.ResourceAccess) {
	guardHandle[T]("Write")
	w.handle = access.GetWrite[T]()
}

// resolve reads the resource out of its cell for this invocation.
func (w *Write[T]) resolve() { w.resolved = w.handle.Get() }

// Get returns the resource for the body of this System call, and for no longer:
// the value resolved at the start of the invocation, or this parameter's own
// last Set since.
func (w *Write[T]) Get() T { return w.resolved }

// Set replaces the resource value, for the few resources that are reassigned
// wholesale rather than mutated in place. A Get on this parameter reads it
// back; another parameter naming the same resource sees it next invocation.
func (w *Write[T]) Set(value T) {
	w.handle.Set(value)
	w.resolved = value
}

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
			handle, kernel.TypeName(resourceType), component, component, component))
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
	return kernel.TypeName(rows.Type.Elem())
}
