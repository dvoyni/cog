package ecs

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// systemParam is a parameter a System may take that reaches the world. Its one
// method runs at registration, inside the handler's single Lock call: it plans
// the parameter against the world and declares whatever it touches, which is
// what makes the signature the declaration.
//
// The interface is unexported and so is its method, which is what makes the
// classification a closed set: no package outside this one can add a System
// parameter, and the builder therefore never has to ask what a type means.
// Every parameter that reaches the world is a pointer to a type here that knows
// how to prepare itself, so the classification stays one assertion wide and a
// new handle is a new type rather than a new branch.
//
// A parameter that reaches nothing — the event value, the request value,
// kernel.Kernel, an In — is not a systemParam, because there is nothing for it
// to declare. Those are recognised by identity in the walk below.
type systemParam interface {
	prepare(en *Entities, access kernel.ResourceAccess)
}

// ToHandler turns a plain Go func into the factory an ordinary cog subscription
// already takes, so the ECS contributes no registration API of its own:
//
//	registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](world, move,
//	    ecs.Feed(func(e app.UpdateEvent) float64 { return e.Dt }),
//	)).After[GravitySystem]()
//
// Before, After, First, Last, ownership, Describe and every Err kind work
// unchanged, and the identity type is the ordinary kernel.Subscription[E] the
// author writes for any subscription. One subscription per System is also what
// gives the parallelism: each System's lock set is its own, and the existing
// scheduler runs disjoint ones concurrently with no new machinery.
//
// The System's lock set is the union of what its parameters declare, computed
// here by walking the func's parameter types. The reflection runs exactly once
// and never again; what the tick pays is the prepared call.
//
// Naming the event is legal and is not the ordinary shape. A System that names
// one can only ever be subscribed to that one, where the same gameplay should
// be drivable by a fixed-step tick, a rollback re-simulation or a test harness
// publishing its own frames. Feed projects what the System actually needs out
// of the event into an In instead, and the event stays with the adapter, which
// is already generic over it.
//
// See prepareSystem for the classification contract and what happens to a
// signature that breaks it.
func ToHandler[E any](en *Entities, system any, feeds ...Feeder[E]) func() (kernel.Lock, kernel.Observe[E]) {
	call := prepareSystem(en, system, feeds, "event")
	return func() (kernel.Lock, kernel.Observe[E]) {
		return call.lock, func(handle kernel.Kernel, event E) error {
			call.call(handle, event)
			return nil
		}
	}
}

// ToExecute is ToHandler's command twin, and is what makes a System invocable as
// a command: the same signature, the same classification, the same lock set,
// registered with HandleCommand instead of Subscribe.
//
//	registrar.HandleCommand[ResetCmd](ecs.ToExecute[ResetRequest, ResetResponse](world, reset,
//	    ecs.Feed(func(r ResetRequest) int { return r.Seed })))
//
// The request is what the event is to a subscription: it may be named, and Feed
// projects out of it so the same System is invocable as a command and drivable
// by a tick without being written twice.
//
// The response is the zero Resp and the error is always nil, because a System
// returns nothing — reflect.Value.Call allocates for a callee that does, which
// is a hard rule and not a style preference. So ToExecute is for a command that
// is an instruction rather than a question. A handler that must answer is not a
// System; it is an ordinary command with a Lock of its own, and writing it that
// way costs nothing the ECS was providing.
func ToExecute[Req any, Resp any](
	en *Entities, system any, feeds ...Feeder[Req],
) func() (kernel.Lock, kernel.Execute[Req, Resp]) {
	call := prepareSystem(en, system, feeds, "request")
	return func() (kernel.Lock, kernel.Execute[Req, Resp]) {
		return call.lock, func(handle kernel.Kernel, request Req) (Resp, error) {
			call.call(handle, request)
			var response Resp
			return response, nil
		}
	}
}

// systemCall is one System as registration left it: the reflection is spent
// here, and a tick pays a store into each stable cell and the Call. E is the
// event a subscription is driven by or the request a command is invoked with;
// nothing below distinguishes them, which is why one builder serves both.
type systemCall[E any] struct {
	entities *Entities
	fn       reflect.Value
	// args is built once and reused. Every element either addresses a stable
	// cell this struct owns or is a pointer to a parameter object allocated at
	// registration, so no argument is ever re-boxed.
	args   []reflect.Value
	params []systemParam
	feeds  []Feeder[E]
	// driven is the stable cell the event or request is written into. It is nil
	// when the signature names neither it nor an In fed from it, so a System of
	// pure Queries does not pay a struct copy a tick for a value nobody reads.
	driven *E
	// handle is the stable cell the kernel value is written into, nil unless the
	// signature names it.
	handle *kernel.Kernel
}

// lock is the System's kernel.Lock: it runs once, at registration, and declares
// the union of what the parameters declare. It loops, which is the sanctioned
// exemption from the straight-line rule, and it satisfies the three guarantees
// that rule is about — deterministic, because the extent is the System's Go
// signature; total, because the body can reach nothing its parameters do not
// name; final, because the walk happens inside this one call.
func (c *systemCall[E]) lock(access kernel.ResourceAccess) {
	// read{*Entities} unconditionally and first. Not only for Queries: every
	// route to a Store declares it, so the invariant a Despawn rests on is
	// closed by construction rather than by the accident that everything happens
	// to use a Query.
	//
	// A Spawn or a WriteableEntities promotes it below, and the write supersedes
	// this read rather than sitting beside it — which is what makes a structural
	// change a total barrier.
	access.GetRead[*Entities]()
	for _, param := range c.params {
		param.prepare(c.entities, access)
	}
}

// call runs the System once. The event lands in its cell before the projections
// read it, so a Feed sees exactly what a named event parameter would have seen,
// through the same address and with no boxing anywhere.
func (c *systemCall[E]) call(handle kernel.Kernel, driven E) {
	if c.driven != nil {
		*c.driven = driven
		for i := range c.feeds {
			c.feeds[i].feed(c.driven)
		}
	}
	if c.handle != nil {
		*c.handle = handle
	}
	c.fn.Call(c.args)
}

// prepareSystem is the classification, and the classification is contract.
//
// A System takes any number of *Query[Q], *Spawn[B], *WriteableEntities,
// *Get[T], *Set[T], *Remove[T], *Read[T], *Write[T] and *In[T]; the
// kernel.Kernel value; and at most once the event or request value itself.
// Anything else is a composition-time failure naming the System's type.
//
// A System returns nothing, which is a hard rule rather than a style
// preference: reflect.Value.Call allocates for a callee that returns a value,
// so one is rejected here. Arity is otherwise arbitrary — Call costs no
// allocation from arity 0 to 12 — and there are no numbered type families
// anywhere in this design.
//
// The failure is a panic at registration, which the plugin boundary reports as
// ErrPluginPanic naming the plugin. That is the third of the three answers the
// specs record for this diagnostic, and it is taken because the alternative — a
// sentence in the composition error list — needs a way for a plugin-side builder
// to reach kernel's private error list, which is an addition nothing else in the
// ECS needs. See the Gap in kernel/docs/specs/ecs-support.md, and
// https://github.com/dvoyni/cog/issues/279, which is where that is settled.
//
// driven names the value E is in the diagnostics: "event" for a subscription,
// "request" for a command. It is the only thing the two builders differ by.
func prepareSystem[E any](en *Entities, system any, feeds []Feeder[E], driven string) *systemCall[E] {
	if en == nil {
		panic("ecs: the handler builder needs the Entities the System runs against")
	}
	fn := reflect.ValueOf(system)
	if fn.Kind() != reflect.Func {
		panic(fmt.Sprintf("ecs: a System is a func, and %s is a %s", fn.Type(), fn.Kind()))
	}
	systemType := fn.Type()
	if systemType.NumOut() != 0 {
		panic(fmt.Sprintf(
			"ecs: System %s returns %d value(s); a System returns nothing, because reflect.Value.Call allocates for a callee that does",
			systemType, systemType.NumOut()))
	}

	call := &systemCall[E]{entities: en, fn: fn, feeds: feeds}
	if len(feeds) > 0 {
		// The projections and a named event parameter share one cell, which is
		// the whole of why In costs nothing over naming the event.
		call.driven = new(E)
	}

	drivenType := reflect.TypeFor[E]()
	kernelType := reflect.TypeFor[kernel.Kernel]()
	args := make([]reflect.Value, systemType.NumIn())
	fed := make([]bool, len(feeds))
	named := false

	for i := range systemType.NumIn() {
		paramType := systemType.In(i)
		switch {
		case paramType == drivenType:
			if named {
				panic(fmt.Sprintf("ecs: System %s names the %s value %s more than once",
					systemType, driven, drivenType))
			}
			named = true
			if call.driven == nil {
				call.driven = new(E)
			}
			// Addressable and stable: the tick writes through the cell, so a
			// struct-shaped event is never re-boxed per call.
			args[i] = reflect.ValueOf(call.driven).Elem()
		case paramType == kernelType:
			if call.handle == nil {
				call.handle = new(kernel.Kernel)
			}
			args[i] = reflect.ValueOf(call.handle).Elem()
		default:
			if index := feederFor(feeds, paramType); index >= 0 {
				if !fed[index] {
					feeds[index].claim(systemType)
					fed[index] = true
				}
				// The In instance comes from the Feeder rather than from
				// reflect.New, because the typed closure that fills it was bound
				// to that instance when Feed baked it.
				args[i] = feeds[index].value
				continue
			}
			param, ok := newSystemParam(paramType)
			if !ok {
				panic(refusal(systemType, paramType, drivenType, driven))
			}
			args[i] = reflect.ValueOf(param)
			call.params = append(call.params, param)
		}
	}
	for i := range feeds {
		if !fed[i] {
			panic(fmt.Sprintf(
				"ecs: System %s is given a Feed for %s, which it does not take; a Feed nothing consumes is a projection computed every tick and thrown away",
				systemType, feeds[i].param))
		}
	}

	call.args = args
	return call
}

// newSystemParam creates the value a System parameter of this type receives,
// reporting whether the type is one the ECS hands out at all. Every such
// parameter is a pointer to a type in this package that knows how to prepare
// itself, which is what keeps the classification one assertion wide.
func newSystemParam(paramType reflect.Type) (systemParam, bool) {
	if paramType.Kind() != reflect.Pointer {
		return nil, false
	}
	param, ok := reflect.New(paramType.Elem()).Interface().(systemParam)
	return param, ok
}

// feederFor is the index of the Feeder supplying this parameter type, or -1. A
// linear scan over a list that is empty or one long, walked once per parameter
// at registration.
func feederFor[E any](feeds []Feeder[E], paramType reflect.Type) int {
	for i := range feeds {
		if feeds[i].param == paramType {
			return i
		}
	}
	return -1
}

// refusal is the sentence a signature outside the contract gets. An In no Feed
// supplies is separated out because it is a different mistake from naming a type
// the ECS does not hand out at all, and the one the user is likelier to make
// twice: the parameter is right and the registration site is missing a line.
func refusal(systemType, paramType, drivenType reflect.Type, driven string) string {
	if paramType.Kind() == reflect.Pointer {
		if _, isInput := reflect.New(paramType.Elem()).Interface().(input); isInput {
			return fmt.Sprintf(
				"ecs: System %s takes %s, which no Feed supplies; pass ecs.Feed(func(%s) ...) beside the System at its registration site",
				systemType, paramType, drivenType)
		}
	}
	return fmt.Sprintf(
		"ecs: System %s takes %s, which is not something a System may take; a System takes *ecs.Query, *ecs.Spawn, *ecs.WriteableEntities, *ecs.Get, *ecs.Set, *ecs.Remove, *ecs.Read, *ecs.Write, *ecs.In, the kernel.Kernel value, and at most once the %s value %s",
		systemType, paramType, driven, drivenType)
}
