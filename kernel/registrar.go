package kernel

import (
	"reflect"
	"slices"
)

// registry stores the resources, commands, and subscriptions available to an
// Engine. It is built during sequential plugin registration; afterward Run
// finalizes ordering and the dispatch paths only read it.
type registry struct {
	resources     map[reflect.Type]*resource
	commands      map[reflect.Type]*command
	subscriptions map[reflect.Type][]subscription
	publications  map[reflect.Type]*publicationPlan
	// adapterDeclarations and adapterContributions are appended in registration
	// order, which is plugin order, and bound by finalize.
	adapterDeclarations  []*adapterDeclaration
	adapterContributions map[reflect.Type][]adapterContribution
	// err is the first fault registration found. Composition stops at it: one
	// error, named where it happened, beats a list whose order has to be
	// manufactured.
	err error
}

// fail records the first fault and keeps it. Later ones are what the first
// knocked over, and saying them would bury it.
func (r *registry) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// Registrar is a plugin-scoped capability used only during registration.
type Registrar struct {
	registry *registry
	owner    PluginName
	// allowed is the owner's dependency closure: itself and every plugin it
	// transitively declares. It is what Dependency checks against, so a value is
	// read only from a plugin already registered by the time the reader is.
	allowed map[PluginName]struct{}
}

// Dependency returns the initial value of resource T, owned by this plugin or by
// a plugin it declares a dependency on. It is the one resource read registration
// permits, and it exists for values a plugin needs to register against rather
// than to run with, such as the authority a Component's Store enrols in.
//
// It is sound because dependencies register first: when the reader registers,
// every plugin it declares has already registered and initialised what it owns,
// and nothing runs concurrently with registration. What it returns is the value
// as registration left it, so keep what it returns only if it is a pointer the
// owner never replaces with Write.Set.
//
// It answers ErrUnavailableDependency when T has no initial value yet or its
// owner is not a declared dependency, and Register propagates that as the
// initialization failure Run returns. It cannot collect and carry on the way
// the other registration faults do: it owes the caller a T, and a plugin that
// went on to use a zero one would report whatever it tripped over next instead
// of the declaration that was wrong.
func (r *Registrar) Dependency[T any]() (T, error) {
	id := reflect.TypeFor[T]()
	cell := r.registry.resources[id]
	if cell == nil || !cell.initialized {
		var zero T
		return zero, ErrUnavailableDependency{Plugin: r.owner, Resource: id}
	}
	if _, ok := r.allowed[cell.owner]; !ok {
		var zero T
		return zero, ErrUnavailableDependency{Plugin: r.owner, Resource: id, Owner: cell.owner}
	}
	return cell.value.(T), nil
}

// InitResource initializes the resource identified by T and records its owner.
func (r *Registrar) InitResource[T any](res T) {
	id := reflect.TypeFor[T]()
	cell := r.registry.resources[id]
	if cell != nil && cell.initialized {
		r.registry.fail(ErrDuplicateRegistration{
			Kind: "resource", Type: id, Owner: r.owner, Existing: cell.owner,
		})
		return
	}
	if cell == nil {
		cell = &resource{typ: id}
		r.registry.resources[id] = cell
	}
	cell.value = res
	cell.initialized = true
	cell.owner = r.owner
}

// HandleCommand registers factory using TCommand as its identity. The factory
// runs once here: its Lock binds the command's resource handles and declares
// its locks, and its Execute is cached for every later invocation.
func (r *Registrar) HandleCommand[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](factory TCommand) {
	id := reflect.TypeFor[TCommand]()
	if existing := r.registry.commands[id]; existing != nil {
		r.registry.fail(ErrDuplicateRegistration{
			Kind: "command", Type: id, Owner: r.owner, Existing: existing.owner,
		})
		return
	}
	lock, execute := factory()
	access := newResourceAccess(r.registry.resources, id)
	if lock != nil {
		lock(*access)
	}
	cmd := &command{id: id, owner: r.owner, boundary: "command " + TypeName(id), resources: access, execute: execute}
	cmd.invocations.New = func() any { return new(commandContext[TRequest, TResponse]) }
	r.registry.commands[id] = cmd
}

// Subscribe registers factory using TSubscription as its identity and returns
// its ordering configuration. As with HandleCommand, the factory runs once here.
func (r *Registrar) Subscribe[
	TSubscription SubscriptionConstraint[TEvent], TEvent any,
](factory TSubscription) *Ordering[TEvent] {
	eventType := reflect.TypeFor[TEvent]()
	id := reflect.TypeFor[TSubscription]()

	lock, observe := factory()
	access := newResourceAccess(r.registry.resources, id)
	if lock != nil {
		lock(*access)
	}
	sub := &Ordering[TEvent]{
		id:        id,
		owner:     r.owner,
		boundary:  "subscription " + TypeName(id),
		resources: access,
		observe:   observe,
	}

	existing := r.registry.subscriptions[eventType]
	for _, task := range existing {
		if task.orderID() != id {
			continue
		}
		r.registry.fail(ErrDuplicateRegistration{
			Kind: "subscription", Type: id, Owner: r.owner, Existing: task.(*Ordering[TEvent]).owner,
		})
		return sub
	}
	r.registry.subscriptions[eventType] = append(existing, sub)
	return sub
}

// finalize validates resources, checks that every declared lock is owned by the
// locking plugin or one of its declared dependencies, resolves Uses declarations,
// binds Adapters to the Ports that declared them, and compiles each event's
// subscription DAG.
// It stops at the first fault and answers with it. The walks below are ordered
// by type rather than by map iteration, so the first fault a broken
// composition hits is the same one on every run — which is what the old sort
// over the joined error messages was really for.
func (r *registry) finalize(dependencies map[PluginName]map[PluginName]struct{}) error {
	if r.err != nil {
		return r.err
	}
	for _, resourceType := range sortedTypes(r.resources) {
		if !r.resources[resourceType].initialized {
			return ErrMissingResource{Type: resourceType}
		}
	}
	// Coupling is checked before Uses widens the lock sets: a declared dispatch
	// couples the handler to the command, never to the resources behind it.
	for _, id := range sortedTypes(r.commands) {
		cmd := r.commands[id]
		if err := r.checkCoupling(dependencies, cmd.owner, cmd.resources); err != nil {
			return err
		}
	}
	for _, eventType := range sortedTypes(r.subscriptions) {
		for _, task := range r.subscriptions[eventType] {
			owner, access := task.coupling()
			if err := r.checkCoupling(dependencies, owner, access); err != nil {
				return err
			}
		}
	}
	if err := r.resolveUses(); err != nil {
		return err
	}
	if err := r.bindAdapters(); err != nil {
		return err
	}
	for _, eventType := range sortedTypes(r.subscriptions) {
		plan, cycle := buildPublicationPlan(r.subscriptions[eventType])
		if cycle != nil {
			return ErrSubscriptionCycle{EventType: eventType, SubscriptionTypes: cycle}
		}
		r.publications[eventType] = plan
	}
	return nil
}

// usesState tracks a command's position in the depth-first walk of Uses edges.
type usesState uint8

const (
	usesUnvisited usesState = iota
	usesVisiting
	usesResolved
)

// resolveUses binds every Uses declaration to its registered command and folds
// that command's lock closure into the declaring handler's set, so a handler
// holds the locks of everything it dispatches. Commands are walked depth-first,
// which makes the union transitive and exposes cycles.
func (r *registry) resolveUses() error {
	var failure error
	state := make(map[reflect.Type]usesState, len(r.commands))
	var path []reflect.Type

	var resolveCommand func(cmd *command)
	resolveAccess := func(declaring reflect.Type, access *ResourceAccess) {
		for _, id := range sortedTypes(access.uses) {
			target, registered := r.commands[id]
			if !registered {
				if failure == nil {
					failure = ErrUsingUnknownCommand{Declaring: declaring, Command: id}
				}
				continue
			}
			access.uses[id].command = target
			resolveCommand(target)
			access.absorb(target.resources)
		}
	}
	resolveCommand = func(cmd *command) {
		switch state[cmd.id] {
		case usesResolved:
			return
		case usesVisiting:
			if failure == nil {
				failure = ErrUsingCommandCycle{
					Commands: append(append([]reflect.Type(nil), path...), cmd.id),
				}
			}
			return
		}
		state[cmd.id] = usesVisiting
		path = append(path, cmd.id)
		resolveAccess(cmd.id, cmd.resources)
		path = path[:len(path)-1]
		state[cmd.id] = usesResolved
	}

	for _, id := range sortedTypes(r.commands) {
		resolveCommand(r.commands[id])
	}
	for _, eventType := range sortedTypes(r.subscriptions) {
		for _, task := range r.subscriptions[eventType] {
			_, access := task.coupling()
			resolveAccess(task.orderID(), access)
		}
	}
	return failure
}

// sortedTypes orders a type-keyed map so composition walks it the same way every
// run, which keeps reported cycles stable.
func sortedTypes[T any](values map[reflect.Type]T) []reflect.Type {
	ids := make([]reflect.Type, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, compareTypes)
	return ids
}

// checkCoupling reports every locked resource whose owner the locking plugin did
// not declare a dependency on. Uninitialized cells are skipped because
// ErrMissingResource already names them.
func (r *registry) checkCoupling(
	dependencies map[PluginName]map[PluginName]struct{}, owner PluginName, access *ResourceAccess,
) error {
	if access == nil {
		return nil
	}
	allowed := dependencies[owner]
	check := func(resourceType reflect.Type) error {
		cell := r.resources[resourceType]
		if cell == nil || !cell.initialized || cell.owner == owner {
			return nil
		}
		if _, ok := allowed[cell.owner]; ok {
			return nil
		}
		return ErrUndeclaredDependency{Plugin: owner, Owner: cell.owner, Resource: resourceType}
	}
	for _, resourceType := range sortedTypes(access.read) {
		if err := check(resourceType); err != nil {
			return err
		}
	}
	for _, resourceType := range sortedTypes(access.write) {
		if err := check(resourceType); err != nil {
			return err
		}
	}
	return nil
}
