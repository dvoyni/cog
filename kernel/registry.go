package kernel

import (
	"reflect"
	"slices"
)

// usesState tracks a command's position in the depth-first walk of Uses edges.
type usesState uint8

const (
	usesUnvisited usesState = iota
	usesVisiting
	usesResolved
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

// bindAdapters hands every declaration the contributions for its Port and
// reports each required Port that has none or several. Contributions were
// appended during sequential registration, so they are already in plugin order.
func (r *registry) bindAdapters() error {
	for _, declaration := range r.adapterDeclarations {
		contributions := r.adapterContributions[declaration.port]
		if !declaration.collects {
			switch len(contributions) {
			case 0:
				return ErrMissingAdapter{Plugin: declaration.owner, Port: declaration.port}
			case 1:
			default:
				return ErrDuplicateAdapter{
					Plugin: declaration.owner, Port: declaration.port, Adapters: describeAdapters(contributions),
				}
			}
		}
		declaration.bind(contributions)
	}
	return nil
}

// describeContention computes the conflict report from registry state that
// finalize has already frozen. Subscriptions are read from the subscription
// list rather than from the compiled publication plans, so the report survives
// a composition that failed to compile one event's DAG.
func (r *registry) describeContention() ContentionDescription {
	handlers := r.handlerAccesses()
	return ContentionDescription{
		Resources: resourceContention(r, handlers),
		Handlers:  handlerConflicts(handlers),
		Phases:    phaseContention(handlers),
	}
}

// handlerAccesses collects every handler holding a lock set, in the order every
// view renders them, so the whole report reads the same way on every run.
func (r *registry) handlerAccesses() []handlerAccess {
	handlers := make([]handlerAccess, 0, len(r.commands))
	commands := 0
	for _, id := range sortedTypes(r.commands) {
		cmd := r.commands[id]
		if cmd.resources == nil {
			continue
		}
		commands++
		handlers = append(handlers, handlerAccess{
			ref:    HandlerRef{Kind: "command", Type: cmd.id, Owner: cmd.owner},
			access: cmd.resources,
		})
	}
	for _, eventType := range sortedTypes(r.subscriptions) {
		for _, task := range r.subscriptions[eventType] {
			owner, access := task.coupling()
			if access == nil {
				continue
			}
			handlers = append(handlers, handlerAccess{
				ref: HandlerRef{
					Kind: "subscription", Type: task.orderID(), Owner: owner, Event: eventType,
				},
				phase:  subscriptionPhase(task),
				access: access,
			})
		}
	}
	slices.SortFunc(handlers[commands:], func(a, b handlerAccess) int { return compareRefs(a.ref, b.ref) })
	return handlers
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
