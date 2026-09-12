package ecs

import (
	"fmt"
	"reflect"
)

// In carries a per-tick value into a System without the System naming where it
// came from, and it is the answer to a System being welded to one event type.
//
// A System that names app.UpdateEvent can only ever be subscribed to
// app.UpdateEvent. The same gameplay cannot then be driven by a fixed-step
// tick, by a rollback re-simulation, or by a test harness publishing its own
// frames, without being written twice. The event belongs to the adapter, which
// is already generic over it; a System declares only what it consumes.
//
//	func advance(q *ecs.Query[AdvancedQ], dt *ecs.In[float64]) {
//	    step := dt.Get()                       // once, outside the loop
//	    for _, it := range q.All() { … }
//	}
//
//	ecs.ToHandler[app.UpdateEvent](world, advance, ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }))
//	ecs.ToHandler[FixedTick](world, advance,       ecs.Feed(func(e FixedTick) float64 { return e.Step }))
//
// Get must be read once outside the loop, and that is a usage rule rather than
// an implementation detail. In is a pointer to a cell the adapter writes, so a
// Get inside the loop is a load the compiler cannot hoist past the Component
// writes: it has no way to prove they do not alias. ecs/README.md carries the
// measured cost of forgetting.
//
// In declares no lock. It is not a route to the world; it is a value the
// adapter already had.
type In[T any] struct {
	// value is what the Feeder wrote for this tick. The cell is allocated once,
	// at registration, and written in place, so the projection boxes nothing.
	value T
	// bound records that a Feeder carrying this cell has been given to a System,
	// so the same Feeder handed to a second one is caught rather than silently
	// making two Systems share a cell no lock stands between.
	bound bool
}

// Get returns the value the adapter projected for this tick. Hoist it.
func (i *In[T]) Get() T { return i.value }

// input is what every *In[T] satisfies whatever T is, so the builder can tell a
// System naming an input no Feed supplies from a System naming a type the ECS
// does not hand out at all. The two are different mistakes and deserve
// different sentences.
//
// In is deliberately not a systemParam: the instance a System receives has to
// be the one its Feeder writes, so it can never be the one reflect.New would
// make. Leaving it out of that interface is what makes the fallthrough a
// diagnostic instead of a System reading zero every tick.
type input interface{ claim() bool }

// claim marks this cell as belonging to one System and reports whether it was
// free. It runs once per registration, never per tick.
func (i *In[T]) claim() bool {
	if i.bound {
		return false
	}
	i.bound = true
	return true
}

// Feeder is one projection from the event to one In, resolved at registration.
// It carries the In instance itself rather than a way to build one, because a
// generic cannot be instantiated from a reflect.Type — so the typed closure
// that fills the cell is baked here, where T is still a compile-time type, and
// the builder only ever matches types and copies a reflect.Value.
//
// It is generic in the event so a Feed written for one event cannot be handed
// to an adapter for another: the mismatch is a compile error rather than a
// reinterpreted struct.
type Feeder[E any] struct {
	// param is the parameter type this Feeder supplies, *In[T].
	param reflect.Type
	// value is the *In[T] instance itself, ready to be an argument.
	value reflect.Value
	// feed projects the event into that instance. It takes the adapter's stable
	// event cell by pointer, so a tick copies the event exactly once — into the
	// cell the event parameter would have read — and nothing is boxed.
	feed func(event *E)
}

// Feed builds the projection for one In[T]. Everything typed happens here, at
// registration, and what a tick pays is one indirect call with no allocation.
//
// Call it at each registration site. A Feeder hoisted into a variable and given
// to two Systems would have them share one cell with no lock between them, and
// ToHandler refuses that rather than leaving it to be found as a flake.
func Feed[E any, T any](project func(E) T) Feeder[E] {
	into := new(In[T])
	return Feeder[E]{
		param: reflect.TypeFor[*In[T]](),
		value: reflect.ValueOf(into),
		feed:  func(event *E) { into.value = project(*event) },
	}
}

// claim binds this Feeder to the System being built, reporting whether it was
// free. A zero Feeder — one a caller declared rather than built — is refused
// here too, because its projection is nil and its cell does not exist.
func (f Feeder[E]) claim(system reflect.Type) {
	if f.feed == nil {
		panic(fmt.Sprintf("ecs: System %s is given a zero ecs.Feeder; build one with ecs.Feed", system))
	}
	if !f.value.Interface().(input).claim() {
		panic(fmt.Sprintf(
			"ecs: System %s is given a Feed already bound to another System; call ecs.Feed at each registration site, because the Feeder carries the %s instance itself",
			system, f.param))
	}
}
