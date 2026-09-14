package ecs

import "github.com/dvoyni/cog/bundles/ecs/internal/types"

// Entity is an opaque handle to one thing. It is comparable, copyable and
// usable as a map key, and the zero value means no Entity.
//
// It is a uint64 with the index in the low 32 bits and the generation in the
// high 32, and that split is deliberately not public contract: there is no
// exported Index or Generation. Both this and a struct of two uint32s are 8
// bytes and both are comparable, but the struct leaks its layout into every
// call site and can never be re-cut.
//
// Generations start at 1, so Entity(0) unambiguously means "no Entity" while
// index 0 stays an ordinary usable slot. Thirty-two bits of generation is about
// 4e9 reuses of one slot before a stale handle could alias.
//
// String renders one as its index and generation, "Entity(7v2)" being index 7
// at generation 2, because a log that cannot distinguish a recycled index from
// the handle that preceded it is useless; reading them back in code is what the
// type refuses.
type Entity = types.Entity

// NoEntity is the absent handle. Compare with ==.
const NoEntity = types.NoEntity

// Query is a System's means of iterating the Entities that have a set of
// Components. The set is the field list of Q, a struct type whose field types
// are the Components and whose field pointer-ness is the access mode:
//
//	type MoveQuery struct {
//	    Body     *Body     // write — yields the stored value itself
//	    Velocity Velocity  // read  — yields a copy
//	}
//
// A blank Without or With field narrows it without yielding anything. A Query
// is planned once, at registration, and reached only as a parameter of a
// System; All yields each matching Entity with a pointer to Q that is valid
// only for the current step.
type Query[Q any] = types.Query[Q]

// Without narrows a Query to the Entities that do not have T. It is written as
// a blank field, because it yields nothing into the Query struct:
//
//	type ActiveQuery struct {
//	    Body     *Body
//	    Velocity Velocity
//	    _        ecs.Without[Disabled]
//	}
//
// It still contributes read{T} to the System's lock set, because the probe
// reads that Store's sparse array. A filter can never drive a Query, so a
// Query needs at least one present-typed Component or Tag besides its filters.
type Without[T any] = types.Without[T]

// With narrows a Query to the Entities that do have T, without yielding T into
// the struct: the spelling for presence matched on but not read. It contributes
// read{T}, as Without does. Where T is a Tag, name it as an ordinary field
// instead, which costs no copy and can drive.
type With[T any] = types.With[T]

// Spawn creates Entities carrying a complete Component set, named as a struct
// type whose field types are the Components and whose value carries them:
//
//	func fire(sp *ecs.Spawn[Projectile]) {
//	    sp.New(Projectile{Body: Body{X: 1}, Velocity: Velocity{X: 10}})
//	}
//
// It declares write{*Entities}, a total barrier: Entities holds a reference to
// every Store, so one entry in the lock set excludes every System in the frame.
type Spawn[S any] = types.Spawn[S]

// WriteableEntities is the write-locked promotion of the id authority, and the
// only thing that can retire an Entity: Despawn empties every Store of it. It
// declares write{*Entities} and nothing besides.
type WriteableEntities = types.WriteableEntities

// Get reaches one Component of an Entity a System did not iterate to, such as
// the target a Reference names. It is the read half: Of yields a copy, and
// there is no Ref, so a read handle is never a write in disguise.
//
//	func home(q *ecs.Query[HomingQ], bodies *ecs.Get[Body]) {
//	    for _, it := range q.All() {
//	        target, ok := bodies.Of(it.Homing.Target)
//	        if !ok {
//	            continue            // the target is gone, or never had a Body
//	        }
//	        it.Velocity.V = target.Pos.Sub(it.Body.Pos).Normalized()
//	    }
//	}
//
// It declares read{*Store[T]} and read{*Entities}.
type Get[T any] = types.Get[T]

// Set is the write half of reaching another Entity: Of reads the way Get does,
// Ref hands out a pointer to the stored value, and UpdateFor inserts one where
// the Entity has none. It declares write{*Store[T]} and read{*Entities}.
type Set[T any] = types.Set[T]

// Remove takes a Component away from an Entity and is the inverse of
// Set.UpdateFor. It declares write{*Store[T]} and read{*Entities}.
type Remove[T any] = types.Remove[T]

// Read is the System parameter that names another plugin's kernel resource for
// read, and with Write the whole of the binding mechanism: a System that draws
// reaches that plugin through the frame-local resource it already publishes.
// Get returns the resource for the body of this System call, and for no longer.
// Neither names the ECS's own cells: *Entities and *Store[T] are refused.
type Read[T any] = types.Read[T]

// Write is Read's writing form, and the one a recording System takes. Get
// returns the resource for this System call; Set replaces it wholesale.
type Write[T any] = types.Write[T]

// In carries a per-tick value into a System without the System naming where it
// came from, so the same System can be driven by any event a Feed projects
// from:
//
//	func advance(q *ecs.Query[AdvancedQ], dt *ecs.In[float64]) {
//	    step := dt.Get()                       // once, outside the loop
//	    for _, it := range q.All() { … }
//	}
//
//	ecs.ToHandler[app.UpdateEvent](registrar, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
//
// Read Get once outside the loop: In is a cell the adapter writes, so a Get
// inside it is a load the compiler cannot hoist. In declares no lock.
type In[T any] = types.In[T]

// Feeder is one projection from the event E to one In, resolved at
// registration. Build one with Feed at each registration site.
type Feeder[E any] = types.Feeder[E]

// Resp is how a System invoked as a command answers: it takes *ecs.Resp[Res],
// where Res is the command's response type, and calls Set. Naming one is
// optional, and a System registered with ToHandler may not name one.
//
//	func count(request CountRequest, q *ecs.Query[CountQ], answer *ecs.Resp[CountResponse]) {
//	    reply := CountResponse{}
//	    for range q.All() { reply.N++ }
//	    answer.Set(reply)
//	}
type Resp[T any] = types.Resp[T]

// List is variable-length data a Component may hold: a fixed-length run of T
// that yields copies and can only be written through a method. A bare []T
// cannot be a Component, because a copy of one shares its backing array and a
// read-locked System could write the Store through it; a List's backing array
// is unexported, its constructors copy, At returns a copy and Set is checked
// under -tags ecs_validate. Its length is fixed at construction.
//
// A List's backing array is a heap allocation the collector scans; prefer [N]T
// wherever the bound is small and real.
type List[T any] = types.List[T]
