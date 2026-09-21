package kernel

import (
	"reflect"
)

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

// RequireAdapter declares that this plugin needs exactly one Adapter for the
// required Port P. Composition binds it after every Register; none fails with
// ErrMissingAdapter and several with ErrDuplicateAdapter. The binding adds no
// plugin dependency.
func (r *Registrar) RequireAdapter[P RequiredPortConstraint[I], I any]() RequiredAdapter[I] {
	binding := &requiredBinding[I]{}
	r.declarePort[P, I](false, func(contributions []adapterContribution) {
		if len(contributions) != 1 {
			return
		}
		binding.adapter = contributions[0].value.(I)
		binding.bound = true
	})
	return RequiredAdapter[I]{binding: binding}
}

// CollectAdapters declares that this plugin takes every Adapter provided for the
// collected Port P, zero included. Composition binds them after every Register,
// in plugin order, each with the name of the plugin that provided it. The
// binding adds no plugin dependency.
func (r *Registrar) CollectAdapters[P CollectedPortConstraint[I], I any]() CollectedAdapters[I] {
	binding := &collectedBinding[I]{}
	r.declarePort[P, I](true, func(contributions []adapterContribution) {
		binding.adapters = make([]ContributedAdapter[I], 0, len(contributions))
		for _, contribution := range contributions {
			binding.adapters = append(binding.adapters, ContributedAdapter[I]{
				Plugin: contribution.plugin, Adapter: contribution.value.(I),
			})
		}
		binding.bound = true
	})
	return CollectedAdapters[I]{binding: binding}
}

// ProvideAdapter contributes adapter as the Adapter A, to the Port A is built
// on. The parameter has the type that Port is built on, so the compiler checks
// that adapter is one: an implementation of the Port's interface, or a value of
// its value type. An Adapter no plugin requires or collects is not an error. A
// nil adapter is refused with ErrNilAdapter and contributes nothing; a typed
// nil, such as a nil pointer, is not nil.
//
// Go infers type parameters from a call's arguments before it reads their
// constraints, so a concrete adapter for an interface Port must already have
// the interface type: convert it, as in
// ProvideAdapter[GfxBackend](gfx.Backend(device)), or pass a value declared
// with that type.
func (r *Registrar) ProvideAdapter[A AdapterConstraint[P], P portConstraint[K, I], K portKind, I any](adapter I) {
	id := reflect.TypeFor[A]()
	if any(adapter) == nil {
		r.registry.fail(ErrNilAdapter{Plugin: r.owner, Adapter: id})
		return
	}
	port := reflect.TypeFor[P]()
	r.registry.adapterContributions[port] = append(r.registry.adapterContributions[port],
		adapterContribution{plugin: r.owner, adapter: id, value: adapter})
}

func (r *Registrar) declarePort[P any, I any](collects bool, bind func([]adapterContribution)) {
	port := reflect.TypeFor[P]()
	for _, existing := range r.registry.adapterDeclarations {
		if existing.port == port && existing.owner == r.owner {
			r.registry.fail(ErrDuplicateRegistration{
				Kind: "port declaration", Type: port, Owner: r.owner, Existing: existing.owner,
			})
			return
		}
	}
	r.registry.adapterDeclarations = append(r.registry.adapterDeclarations, &adapterDeclaration{
		port: port, iface: reflect.TypeFor[I](), owner: r.owner, collects: collects, bind: bind,
	})
}
