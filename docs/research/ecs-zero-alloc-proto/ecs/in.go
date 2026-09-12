package ecs

import (
	"reflect"
	"unsafe"
)

// In carries a per-tick value into a System without the System naming where it
// came from. It is the answer to a System being welded to one event type: a
// System that takes app.UpdateEvent can only ever be subscribed to
// app.UpdateEvent, so the same gameplay cannot be driven by a fixed-step tick,
// by a rollback re-simulation, or by a test harness that publishes its own
// frames.
//
// The event stays where the adapter is. ToHandler is already generic over the
// event, so it is the one place that has to know it; a System declares only
// what it consumes.
//
//	func Move(q *ecs.Query[MoveQ], dt *ecs.In[float32]) {
//	    for _, it := range q.All() { it.B.Px += it.B.Vx * float64(dt.Get()) }
//	}
//
//	ecs.ToHandler[app.UpdateEvent](en, Move,
//	    ecs.Feed(func(e app.UpdateEvent) float32 { return e.Dt }))
//
// Move names no event, so the same func also registers under a PhysicsTick with
// a different Feed and nothing else changed.
type In[T any] struct{ v T }

// Get returns the value the adapter projected for this tick.
func (i *In[T]) Get() T { return i.v }

// Feeder is one projection from the event to one In, resolved at registration.
// It is not generic, so the builder's reflection stays generic-free (cog#238),
// and it carries the In instance itself rather than a way to make one, because
// a generic cannot be instantiated from a reflect.Type (cog#234).
type Feeder struct {
	typ  reflect.Type
	val  reflect.Value
	feed func(event unsafe.Pointer)
}

// Feed builds the projection for one In[T]. Everything typed happens here, at
// registration: the closure it returns takes the event through the stable cell
// ToHandler already keeps, so nothing is boxed and nothing is allocated per
// tick.
func Feed[E any, T any](project func(E) T) Feeder {
	dst := new(In[T])
	return Feeder{
		typ: reflect.TypeFor[*In[T]](),
		val: reflect.ValueOf(dst),
		feed: func(event unsafe.Pointer) {
			dst.v = project(*(*E)(event))
		},
	}
}
