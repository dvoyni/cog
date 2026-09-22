package types

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

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

// remover is a Remove parameter, naming the Store it removes from.
type remover interface {
	removes(en *Entities) *storeHeader
}

// ownedReader is a Hooks parameter, told which System it belongs to.
type ownedReader interface {
	ownedBy(writer uint32, system string)
}

// resolver is a parameter holding a handle its methods reach on every call. Its
// one method reads each bound kernel handle into a plain cached field, so the
// type assertion out of the kernel's any-typed resource cell is paid once an
// invocation rather than once a call.
//
// It runs once per invocation of the System, from systemCall.call, immediately
// before the System's func and while the handler holds every lock the signature
// declared: the cached value is the locked cell's value for the whole of that
// invocation, and for no longer. It never runs at registration, and prepare
// never reads a handle's value, because a value read from a handle is valid only
// while the handler holds its lock.
//
// Each invocation overwrites the cached fields and nothing clears them after.
// Clearing would be a second pass a tick for nothing: Exclusive already keeps a
// second invocation of the System from reaching them, so the only way to read a
// stale one is to use the parameter outside the invocation that resolved it,
// which is the same contract violation as keeping any value read from a handle.
//
// A parameter with no per-call handle does not implement it. A Query binds its
// Stores once a run in All, and Hooks its Store once a run in beginRun, so
// neither does.
type resolver interface {
	resolve()
}

// systemCall is one System as registration left it: the reflection is spent
// here, and a tick pays a store into each stable cell and the Call. E is the
// event a subscription is driven by or the request a command is invoked with;
// nothing below distinguishes them, which is why one builder serves both.
type systemCall[E any] struct {
	// entities is the authority the parameters are planned against in lock. It
	// is registration data here and nothing more: the body reaches the world only
	// through the handles lock binds.
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
	// gates are the writer handles' checks of their Stores' watched kinds, and
	// readers the Hooks parameters, each with work at the edges of a run. Both
	// are empty for a System that names neither, and cost it a length check.
	gates   []*hookGate
	readers []runEdges
	// spawns are the Spawn parameters' checks, one per Spawn, each covering
	// every Store its Component set carries.
	spawns []*spawnGate
	// resolvers are the parameters that read their handles into cached fields
	// once an invocation, just before the func. Empty for a System that names
	// none, such as one of pure Queries, which pays a length check for them.
	resolvers []resolver
	// armed says the gates and the Spawn gates have been checked. A Store's
	// watched kinds are written in one place, Hooks.prepare, and every reader
	// registers before any System runs, so what a check computes is fixed by
	// the time the first run makes it and is the same answer every run after.
	// The first run pays for the walk and no later one does, which is what
	// takes the check off the per-run cost of a writer nothing watches.
	//
	// It is not hoisted to lock, because that is where the answer is not yet
	// known: a writer may register before the reader that watches its Store.
	armed bool
	// copies are the System's row copies for Changed, one per Store it is handed
	// rows of with write access, shared by every handle on that Store and
	// compared when its run ends. Each one's gate is among gates.
	copies []*rowCopy
	// pace is Validation mode's count of this System's runs on the Hook logs it
	// can append to, and a release build never fills it. See systempace.go.
	pace systemPace
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
	// A System is not re-entrant, and the kernel is what makes that true rather
	// than the caller. Everything below this line is per-invocation state held in
	// one registration-time struct — args, driven, handle, ToExecute's single
	// Resp cell, and the handles each resolver reads into its cached fields — so
	// a second invocation entering while the first is inside would overwrite the
	// first one's arguments, the Stores and resources its accessors reach, and
	// its answer. It is declared for every System, not only the read-only ones a
	// write lock would not already serialise: it excludes a System against itself
	// alone, so it costs no parallelism against any other System, and an
	// unconditional line cannot drift as parameter kinds change.
	access.Exclusive()
	c.gates, c.readers, c.spawns, c.copies = c.gates[:0], c.readers[:0], c.spawns[:0], c.copies[:0]
	c.resolvers = c.resolvers[:0]
	c.armed = false
	// writer names this System on the Changed records its run end appends, and
	// is what its own Hooks readers skip.
	writer := c.entities.nextWriter()
	var system string
	if validate {
		system = systemName(c.fn)
		c.pace = systemPace{}
	}
	for _, param := range c.params {
		param.prepare(c.entities, access)
		if checked, ok := param.(gated); ok {
			c.gates = append(c.gates, checked.gate())
		}
		if spawner, ok := param.(spawnGated); ok {
			c.spawns = append(c.spawns, spawner.spawnGate())
		}
		if copier, ok := param.(rowCopier); ok {
			copier.rowCopies(func(slot **rowCopy) { *slot = c.shareRowCopy(*slot, writer) })
		}
		if reader, ok := param.(runEdges); ok {
			c.readers = append(c.readers, reader)
		}
		if owned, ok := param.(ownedReader); ok {
			owned.ownedBy(writer, system)
		}
		if resolving, ok := param.(resolver); ok {
			c.resolvers = append(c.resolvers, resolving)
		}
		if validate {
			c.enrolPace(param)
		}
	}
	if validate {
		for _, copied := range c.copies {
			c.pace.enrol(copied.store)
		}
	}
}

// enrolPace names to Validation mode's pace count the Stores a parameter lets
// the System append to the logs of. A Set and a *T Query field are named by
// their row copies, once every parameter is prepared.
func (c *systemCall[E]) enrolPace(param systemParam) {
	switch p := param.(type) {
	case *WriteableEntities, spawnGated:
		c.pace.every = true
	case remover:
		c.pace.enrol(p.removes(c.entities))
	}
}

// shareRowCopy is the row copy this System keeps for own's Store: the one an
// earlier handle on the same Store already holds, or own itself, enrolled.
func (c *systemCall[E]) shareRowCopy(own *rowCopy, writer uint32) *rowCopy {
	for _, shared := range c.copies {
		if shared.store == own.store {
			return shared
		}
	}
	own.writer = writer
	c.copies = append(c.copies, own)
	c.gates = append(c.gates, &own.gate)
	c.entities.enrolScratch(own.release)
	return own
}

// call runs the System once. The event lands in its cell before the projections
// read it, so a Feed sees exactly what a named event parameter would have seen,
// through the same address and with no boxing anywhere.
//
// The resolvers run last before the func, after the readers' run starts and
// Validation mode's pace, none of which reads a resolved field. Each overwrites
// what the previous invocation cached and nothing clears it afterwards; see
// resolver.
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
	// The watched kinds are fixed before any System runs, and a writer reads
	// them here rather than at registration, because the reader that watches
	// its Store may register after it. Fixed means fixed, so the first run is
	// the only one that reads them. A reader's copy is fixed before the body
	// and cleared after, so a System's own acts appear in its next run.
	if !c.armed {
		c.arm()
	}
	for _, reader := range c.readers {
		reader.beginRun()
	}
	if validate {
		c.pace.begin(c.entities)
	}
	for _, resolving := range c.resolvers {
		resolving.resolve()
	}
	c.fn.Call(c.args)
	// A change is recorded at the writer's run end, after every write the run
	// made, and after this System's readers took their copies, which is why none
	// of them is given it.
	//
	// taken is tested here and not only inside compare, because compare is far
	// past the inlining budget: a run that copied nothing would otherwise pay a
	// real call per Store to be told there is nothing to compare. compare keeps
	// its own guard, since the tests call it directly.
	for _, copied := range c.copies {
		if copied.taken {
			copied.compare()
		}
	}
	if validate {
		c.pace.end()
	}
	for _, reader := range c.readers {
		reader.endRun()
	}
}

// arm makes every writer handle's check of its Store's watched kinds, once, on
// the System's first run. It is a method rather than a loop in call so that
// call carries one test and one call it never makes again, instead of two
// loops it walks every run.
//
// What it computes cannot change afterwards: Hooks.prepare is the only writer
// of a Store's watched kinds, it runs at registration, and registration is
// closed before any System runs.
func (c *systemCall[E]) arm() {
	for _, gate := range c.gates {
		gate.check()
	}
	for _, spawn := range c.spawns {
		spawn.check()
	}
	c.armed = true
}

// ToHandler turns a plain Go func into the factory an ordinary cog subscription
// already takes, so the ECS contributes no registration API of its own:
//
//	registrar.Subscribe[MoveSystem](ecs.ToHandler[app.UpdateEvent](registrar, move,
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
// The registrar is the one the factory is registered with. It is taken for the
// world the parameters are planned against, read through Registrar.Dependency,
// which is why the System's plugin declares a dependency on ecs.
//
// See prepareSystem for the classification contract and what happens to a
// signature that breaks it.
func ToHandler[E any](
	registrar *kernel.Registrar, system any, feeds ...Feeder[E],
) func() (kernel.Lock, kernel.Observe[E]) {
	call := prepareSystem(registrar, system, feeds, "event", nil)
	return func() (kernel.Lock, kernel.Observe[E]) {
		return call.lock, func(handle kernel.Kernel, event E) {
			call.call(handle, event)
		}
	}
}

// ToExecute is ToHandler's command twin, and is what makes a System invocable as
// a command: the same signature, the same classification, the same lock set,
// registered with HandleCommand instead of Subscribe.
//
//	registrar.HandleCommand[ResetCmd](ecs.ToExecute[ResetRequest, ResetResponse](registrar, reset,
//	    ecs.Feed(func(r ResetRequest) int { return r.Seed })))
//
// The request is what the event is to a subscription: it may be named, and Feed
// projects out of it so the same System is invocable as a command and drivable
// by a tick without being written twice.
//
// The System still returns nothing — reflect.Value.Call allocates for a callee
// that does — so it answers through a *Resp[Res] parameter instead, which the
// builder recognises by type and injects. Naming one is optional: a command that
// is an order rather than a question takes no Resp and answers the zero value.
// See Resp.
//
// The type parameter is spelled Res rather than Resp only because Resp is the
// wrapper's own name and a type parameter would shadow it here.
//
// The error is always nil. A System has no way to fail that is not a panic, and
// a panic is already ErrPluginPanic; expected rejection belongs in the response,
// which is where the kernel asks for it anyway.
func ToExecute[Req any, Res any](
	registrar *kernel.Registrar, system any, feeds ...Feeder[Req],
) func() (kernel.Lock, kernel.Execute[Req, Res]) {
	// One cell, allocated here and read back on every invocation. It is the only
	// route a Resp instance reaches a System by, which is what makes the
	// parameter unambiguous: there is nothing else of that type to inject.
	answer := new(Resp[Res])
	call := prepareSystem(registrar, system, feeds, "request", answer)
	return func() (kernel.Lock, kernel.Execute[Req, Res]) {
		return call.lock, func(handle kernel.Kernel, request Req) Res {
			call.call(handle, request)
			return answer.take()
		}
	}
}

// prepareSystem is the classification, and the classification is contract.
//
// A System takes any number of *Query[Q], *Spawn[S], *DeferredSpawn[S],
// *WriteableEntities, *DeferredDespawn, *Get[T], *Set[T], *Remove[T],
// *Hooks[T, K], *Read[T], *Write[T] and *In[T]; the kernel.Kernel value; at
// most once the event or request value itself; and, for a command only, at most
// once the *Resp[Res] it answers through. Anything else is a composition-time
// failure naming the System's type.
//
// A System returns nothing, which is a hard rule rather than a style
// preference: reflect.Value.Call allocates for a callee that returns a value,
// so one is rejected here. Arity is otherwise arbitrary — Call costs no
// allocation from arity 0 to 12 — and there are no numbered type families
// anywhere in this design.
//
// The failure is a panic at registration, and that is the contract: the plugin
// boundary reports it as kernel.ErrPluginPanic naming the plugin, and
// composition fails. It is the route every ecs registration builder takes,
// Dependency's re-panicked error included, because each returns a value — the
// factory, or the *Store[C] — and so has no error to return. The ecs README's
// §What a signature may contain records why a reported error was withdrawn.
//
// driven names the value E is in the diagnostics: "event" for a subscription,
// "request" for a command. answer is the response cell a command reads back, and
// is nil for a subscription — which is the whole of what makes naming a Resp a
// refusal there. Those two arguments are the only things the builders differ by.
func prepareSystem[E any](
	registrar *kernel.Registrar, system any, feeds []Feeder[E], driven string, answer responseCell,
) *systemCall[E] {
	fn := reflect.ValueOf(system)
	if fn.Kind() != reflect.Func {
		panic(fmt.Sprintf("ecs: a System is a func, and %s is a %s", kernel.TypeName(fn.Type()), fn.Kind()))
	}
	systemType := fn.Type()
	if systemType.NumOut() != 0 {
		panic(fmt.Sprintf(
			"ecs: System %s returns %d value(s); a System returns nothing, because reflect.Value.Call allocates for a callee that does",
			kernel.TypeName(systemType), systemType.NumOut()))
	}

	call := &systemCall[E]{fn: fn, feeds: feeds}
	if len(feeds) > 0 {
		// The projections and a named event parameter share one cell, which is
		// the whole of why In costs nothing over naming the event.
		call.driven = new(E)
	}

	drivenType := reflect.TypeFor[E]()
	kernelType := reflect.TypeFor[kernel.Kernel]()
	args := make([]reflect.Value, systemType.NumIn())
	fed := make([]bool, len(feeds))
	named, answered := false, false

	for i := range systemType.NumIn() {
		paramType := systemType.In(i)
		switch {
		case paramType == drivenType:
			if named {
				panic(fmt.Sprintf("ecs: System %s names the %s value %s more than once",
					kernel.TypeName(systemType), driven, kernel.TypeName(drivenType)))
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
			if _, isResponse := responseOf(paramType); isResponse {
				if answer == nil {
					panic(fmt.Sprintf(
						"ecs: System %s takes %s, and an event has no response to write into; a System that answers is registered with ecs.ToExecute",
						kernel.TypeName(systemType), kernel.TypeName(paramType)))
				}
				slot, argument := answer.slot()
				if paramType != slot {
					panic(fmt.Sprintf(
						"ecs: System %s takes %s, but this command's response is %s",
						kernel.TypeName(systemType), kernel.TypeName(paramType), kernel.TypeName(answer.answers())))
				}
				if answered {
					panic(fmt.Sprintf("ecs: System %s names the response %s more than once",
						kernel.TypeName(systemType), kernel.TypeName(slot)))
				}
				answered = true
				// The Resp instance comes from the builder rather than from
				// reflect.New, because the builder is the only thing that reads it
				// back: an instance nobody collects is an answer thrown away.
				args[i] = argument
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
				kernel.TypeName(systemType), kernel.TypeName(feeds[i].param)))
		}
	}

	call.args = args
	// Last, so a signature outside the contract is refused before the world is
	// asked for: the refusal names the System, which is the more useful sentence.
	entities, err := registrar.Dependency[*Entities]()
	if err != nil {
		panic(err)
	}
	call.entities = entities
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
				kernel.TypeName(systemType), kernel.TypeName(paramType), kernel.TypeName(drivenType))
		}
	}
	return fmt.Sprintf(
		"ecs: System %s takes %s, which is not something a System may take; a System takes *ecs.Query, *ecs.Spawn, *ecs.DeferredSpawn, *ecs.WriteableEntities, *ecs.DeferredDespawn, *ecs.Get, *ecs.Set, *ecs.Remove, *ecs.Hooks, *ecs.Read, *ecs.Write, *ecs.In, the kernel.Kernel value, at most once the %s value %s, and for a command at most once the *ecs.Resp it answers through",
		kernel.TypeName(systemType), kernel.TypeName(paramType), driven, kernel.TypeName(drivenType))
}

// systemName is how a diagnostic names a System: its func's name without the
// import path, or its signature for a func the runtime cannot name.
func systemName(fn reflect.Value) string {
	if f := runtime.FuncForPC(fn.Pointer()); f != nil {
		name := f.Name()
		return name[strings.LastIndex(name, "/")+1:]
	}
	return kernel.TypeName(fn.Type())
}
