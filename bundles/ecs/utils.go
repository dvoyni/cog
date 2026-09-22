package ecs

import (
	"reflect"

	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// RegisterComponent declares that C is a Component of this world, and is the
// only thing that makes a Store for it exist. It checks Storable, creates the
// Store, enrols it with the authority so a despawn can empty it, and hands it
// to the kernel as a resource of type *Store[C] owned by the calling plugin.
//
// Register a Component in the plugin that defines its Go type, which declares a
// dependency on Name: the authority is read through Registrar.Dependency. The
// one exception is m.Transform, whose Store the ecs plugin registers itself. ids is
// the peak population hint the Store reserves for; it is not a cap. It panics
// if C names mutable indirection, naming the offending field by path.
func RegisterComponent[C any](registrar *kernel.Registrar, ids uint32) *Store[C] {
	return types.RegisterComponent[C](registrar, ids)
}

// NewStore creates a Store and enrols it with the authority, which is what lets
// a despawn empty it. ids is the peak population the app expects; it is a hint
// and not a cap. RegisterComponent is the sanctioned caller.
func NewStore[T any](en *Entities, ids uint32) *Store[T] { return types.NewStore[T](en, ids) }

// Storable reports whether a type may be a Component. The rule it checks is
// that a Component contains no mutable indirection, transitively: it admits
// numerics, bools, fixed-size arrays, Entity, structs of those, string, assets.Blob
// and m.List[T], and refuses pointers, slices, maps, channels, funcs, interfaces
// and sync types. The error names the offending field by path.
func Storable(t reflect.Type) error { return types.Storable(t) }

// PointerFree reports whether a type contains no pointers, transitively: the
// cheapest thing a Store holds, copied by sized moves and never scanned by the
// collector. It forbids pointers, slices, maps, channels, funcs, interfaces and
// strings, and permits numerics, bools, fixed-size arrays, Entity, and structs
// of those.
func PointerFree(t reflect.Type) error { return types.PointerFree(t) }

// ToHandler turns a plain Go func into the factory an ordinary cog subscription
// takes, so the ECS contributes no registration API of its own:
//
//	registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](registrar, move,
//	    ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }),
//	)).After[GravitySystem]()
//
// A System takes any number of *Query, *Spawn, *WriteableEntities,
// *DeferredDespawn, *Get, *Set, *Remove, *Hooks, *Read, *Write and *In; the
// kernel.Kernel value; at most once the event value; and, for a System
// registered with ToExecute, at most once the *Resp it answers through. It
// returns nothing. Its lock set is the union of what its parameters declare,
// computed once here. A signature outside that contract panics at registration,
// naming the System, and is reported as kernel.ErrPluginPanic naming the plugin.
func ToHandler[E any](
	registrar *kernel.Registrar, system any, feeds ...Feeder[E],
) func() (kernel.Lock, kernel.Observe[E]) {
	return types.ToHandler[E](registrar, system, feeds...)
}

// ToExecute is ToHandler's command twin: the same signature, classification and
// lock set, registered with HandleCommand. The System may name the request as a
// subscription names its event, and answers through a *Resp[Res] parameter;
// without one the command answers the zero value. The error is always nil.
//
//	registrar.HandleCommand[ResetCmd](ecs.ToExecute[ResetRequest, ResetResponse](registrar, reset,
//	    ecs.Feed(func(r ResetRequest) int { return r.Seed })))
func ToExecute[Req any, Res any](
	registrar *kernel.Registrar, system any, feeds ...Feeder[Req],
) func() (kernel.Lock, kernel.Execute[Req, Res]) {
	return types.ToExecute[Req, Res](registrar, system, feeds...)
}

// Feed builds the projection for one In[T] from the event E. Call it at each
// registration site: a Feeder handed to two Systems is refused.
func Feed[E any, T any](project func(E) T) Feeder[E] { return types.Feed[E, T](project) }
