package kernel

import (
	"reflect"
	"slices"
	"strings"
)

// registry stores the resources, commands, and subscriptions available to an
// Engine. It is built during sequential plugin registration; afterward Run
// finalizes ordering and the dispatch paths only read it.
type registry struct {
	resources     map[reflect.Type]*resource
	commands      map[reflect.Type]*command
	subscriptions map[reflect.Type][]subscription
	publications  map[reflect.Type]*publicationPlan
	errs          []error
}

// Registrar is a plugin-scoped capability used only during registration.
type Registrar struct {
	registry *registry
	owner    PluginName
}

// InitResource initializes the resource identified by T and records its owner.
func (r *Registrar) InitResource[T any](res T) {
	id := reflect.TypeFor[T]()
	cell := r.registry.resources[id]
	if cell != nil && cell.initialized {
		r.registry.errs = append(r.registry.errs, ErrDuplicateRegistration{
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
		r.registry.errs = append(r.registry.errs, ErrDuplicateRegistration{
			Kind: "command", Type: id, Owner: r.owner, Existing: existing.owner,
		})
		return
	}
	lock, execute := factory()
	access := newResourceAccess(r.registry.resources)
	if lock != nil {
		lock(*access)
	}
	cmd := &command{id: id, owner: r.owner, boundary: "command " + id.String(), resources: access, execute: execute}
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
	access := newResourceAccess(r.registry.resources)
	if lock != nil {
		lock(*access)
	}
	sub := &Ordering[TEvent]{
		id:        id,
		owner:     r.owner,
		boundary:  "subscription " + id.String(),
		resources: access,
		observe:   observe,
	}

	existing := r.registry.subscriptions[eventType]
	for _, task := range existing {
		if task.orderID() != id {
			continue
		}
		r.registry.errs = append(r.registry.errs, ErrDuplicateRegistration{
			Kind: "subscription", Type: id, Owner: r.owner, Existing: task.(*Ordering[TEvent]).owner,
		})
		return sub
	}
	r.registry.subscriptions[eventType] = append(existing, sub)
	return sub
}

// finalize validates resources, checks that every declared lock is owned by the
// locking plugin or one of its declared dependencies, resolves Uses declarations,
// and compiles each event's subscription DAG.
func (r *registry) finalize(dependencies map[PluginName]map[PluginName]struct{}) []error {
	errs := append([]error(nil), r.errs...)
	for resourceType, cell := range r.resources {
		if !cell.initialized {
			errs = append(errs, ErrMissingResource{Type: resourceType})
		}
	}
	// Coupling is checked before Uses widens the lock sets: a declared dispatch
	// couples the handler to the command, never to the resources behind it.
	for _, cmd := range r.commands {
		errs = append(errs, r.checkCoupling(dependencies, cmd.owner, cmd.resources)...)
	}
	for _, tasks := range r.subscriptions {
		for _, task := range tasks {
			owner, access := task.coupling()
			errs = append(errs, r.checkCoupling(dependencies, owner, access)...)
		}
	}
	errs = append(errs, r.resolveUses()...)
	for eventType, tasks := range r.subscriptions {
		plan, cycle := buildPublicationPlan(tasks)
		if cycle != nil {
			errs = append(errs, ErrSubscriptionCycle{EventType: eventType, SubscriptionTypes: cycle})
			continue
		}
		r.publications[eventType] = plan
	}
	// Map iteration makes the order arbitrary; sort so composition failures read
	// the same way every run.
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errs
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
func (r *registry) resolveUses() []error {
	var errs []error
	state := make(map[reflect.Type]usesState, len(r.commands))
	var path []reflect.Type

	var resolveCommand func(cmd *command)
	resolveAccess := func(declaring reflect.Type, access *ResourceAccess) {
		for _, id := range sortedTypes(access.uses) {
			target, registered := r.commands[id]
			if !registered {
				errs = append(errs, ErrUsingUnknownCommand{Declaring: declaring, Command: id})
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
			errs = append(errs, ErrUsingCommandCycle{Commands: append(append([]reflect.Type(nil), path...), cmd.id)})
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
	for _, tasks := range r.subscriptions {
		for _, task := range tasks {
			_, access := task.coupling()
			resolveAccess(task.orderID(), access)
		}
	}
	return errs
}

// sortedTypes orders a type-keyed map so composition walks it the same way every
// run, which keeps reported cycles stable.
func sortedTypes[T any](values map[reflect.Type]T) []reflect.Type {
	ids := make([]reflect.Type, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b reflect.Type) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

// checkCoupling reports every locked resource whose owner the locking plugin did
// not declare a dependency on. Uninitialized cells are skipped because
// ErrMissingResource already names them.
func (r *registry) checkCoupling(
	dependencies map[PluginName]map[PluginName]struct{}, owner PluginName, access *ResourceAccess,
) []error {
	if access == nil {
		return nil
	}
	allowed := dependencies[owner]
	var errs []error
	check := func(resourceType reflect.Type) {
		cell := r.resources[resourceType]
		if cell == nil || !cell.initialized || cell.owner == owner {
			return
		}
		if _, ok := allowed[cell.owner]; ok {
			return
		}
		errs = append(errs, ErrUndeclaredDependency{Plugin: owner, Owner: cell.owner, Resource: resourceType})
	}
	for resourceType := range access.read {
		check(resourceType)
	}
	for resourceType := range access.write {
		check(resourceType)
	}
	return errs
}
