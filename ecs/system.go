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
type systemParam interface {
	prepare(en *Entities, access kernel.ResourceAccess)
}

// ToHandler turns a plain Go func into the factory an ordinary cog subscription
// already takes, so the ECS contributes no registration API of its own:
//
//	registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](world, move)).After[GravitySystem]()
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
// A System is called once per tick and iterates its Queries itself. Arity is
// arbitrary — reflect.Value.Call costs no allocation from arity 0 to 12 — and a
// System returns nothing, which is a hard rule rather than a style preference:
// Call allocates for a callee that returns a value, so one is rejected here.
//
// What this build accepts is a *Query[Q] and, at most once, the event value
// itself. Naming the event is legal but is not the ordinary shape, because a
// System that names one can only ever be subscribed to that one; In and Feed,
// the accessors, Spawn and the resource handles arrive with their own tickets.
//
// A parameter it does not recognise is a panic at registration, which the
// plugin boundary reports as ErrPluginPanic naming the plugin. That is the
// third of the three answers the specs record for this diagnostic, and it is
// taken because the alternative — a sentence in the composition error list —
// needs a way for a plugin-side builder to reach kernel's private error list,
// which is an addition nothing else in the ECS needs. See the Gap in
// kernel/docs/specs/ecs-support.md.
func ToHandler[E any](en *Entities, system any) func() (kernel.Lock, kernel.Observe[E]) {
	if en == nil {
		panic("ecs: ToHandler needs the Entities the System runs against")
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

	// The event reaches the System through one stable cell written per tick, so
	// nothing is boxed and the arguments are built once rather than per call.
	event := new(E)
	eventType := reflect.TypeFor[E]()
	args := make([]reflect.Value, systemType.NumIn())
	var params []systemParam
	for i := range systemType.NumIn() {
		paramType := systemType.In(i)
		if paramType == eventType {
			args[i] = reflect.ValueOf(event).Elem()
			continue
		}
		param, ok := newSystemParam(paramType)
		if !ok {
			panic(fmt.Sprintf(
				"ecs: System %s takes %s, which is not something a System may take; a System takes Queries, the event value, and the handles later tickets add",
				systemType, paramType))
		}
		args[i] = reflect.ValueOf(param)
		params = append(params, param)
	}

	return func() (kernel.Lock, kernel.Observe[E]) {
		return func(access kernel.ResourceAccess) {
				// read{*Entities} unconditionally and first. Not only for Queries:
				// every route to a Store declares it, so the invariant a Despawn
				// rests on is closed by construction rather than by the accident
				// that everything happens to use a Query.
				access.GetRead[*Entities]()
				for _, param := range params {
					param.prepare(en, access)
				}
			}, func(_ kernel.Kernel, tick E) error {
				*event = tick
				fn.Call(args)
				return nil
			}
	}
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
