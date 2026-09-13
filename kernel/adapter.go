package kernel

import (
	"fmt"
	"reflect"
)

// adapterDeclaration is one plugin's RequireAdapter or CollectAdapters for one
// interface. bind receives every contribution for that interface, in plugin
// order, and fills the typed handle the declaration returned.
type adapterDeclaration struct {
	iface    reflect.Type
	port     PluginName
	collects bool
	bind     func(contributions []adapterContribution)
}

// adapterContribution is one ProvideAdapter call.
type adapterContribution struct {
	plugin  PluginName
	adapter any
}

// RequiredAdapter is the handle RequireAdapter returns. It is bound at
// composition and valid from Start onwards.
type RequiredAdapter[T any] struct{ binding *requiredBinding[T] }

type requiredBinding[T any] struct {
	bound   bool
	adapter T
}

// Get returns the one Adapter bound for T. It takes no lock: an Adapter is a
// plain value, and its thread rules are the Port interface's. It panics before
// composition has bound the handle.
func (h RequiredAdapter[T]) Get() T {
	if h.binding == nil || !h.binding.bound {
		panic(unboundAdapter[T]())
	}
	return h.binding.adapter
}

// CollectedAdapters is the handle CollectAdapters returns. It is bound at
// composition and valid from Start onwards.
type CollectedAdapters[T any] struct{ binding *collectedBinding[T] }

type collectedBinding[T any] struct {
	bound    bool
	adapters []ContributedAdapter[T]
}

// ContributedAdapter is one collected Adapter and the plugin that provided it.
type ContributedAdapter[T any] struct {
	Plugin  PluginName
	Adapter T
}

// Get returns every Adapter bound for T, in plugin order, as a fresh slice. It
// panics before composition has bound the handle.
func (h CollectedAdapters[T]) Get() []ContributedAdapter[T] {
	if h.binding == nil || !h.binding.bound {
		panic(unboundAdapter[T]())
	}
	return append([]ContributedAdapter[T](nil), h.binding.adapters...)
}

func unboundAdapter[T any]() string {
	return fmt.Sprintf("kernel: adapter handle for %v read before composition bound it", reflect.TypeFor[T]())
}

// RequireAdapter declares that this plugin needs exactly one Adapter for the
// interface T. Composition binds it after every Register; none fails with
// ErrMissingAdapter and several with ErrDuplicateAdapter. The binding adds no
// plugin dependency. It panics if T is not an interface type.
func (r *Registrar) RequireAdapter[T any]() RequiredAdapter[T] {
	binding := &requiredBinding[T]{}
	r.declareAdapter[T](false, func(contributions []adapterContribution) {
		if len(contributions) != 1 {
			return
		}
		binding.adapter = contributions[0].adapter.(T)
		binding.bound = true
	})
	return RequiredAdapter[T]{binding: binding}
}

// CollectAdapters declares that this plugin takes every Adapter provided for
// the interface T, zero included. Composition binds them after every Register,
// in plugin order, each with the name of the plugin that provided it. The
// binding adds no plugin dependency. It panics if T is not an interface type.
func (r *Registrar) CollectAdapters[T any]() CollectedAdapters[T] {
	binding := &collectedBinding[T]{}
	r.declareAdapter[T](true, func(contributions []adapterContribution) {
		binding.adapters = make([]ContributedAdapter[T], 0, len(contributions))
		for _, contribution := range contributions {
			binding.adapters = append(binding.adapters, ContributedAdapter[T]{
				Plugin: contribution.plugin, Adapter: contribution.adapter.(T),
			})
		}
		binding.bound = true
	})
	return CollectedAdapters[T]{binding: binding}
}

// ProvideAdapter contributes adapter for the interface T, spelled explicitly so
// the compiler checks that adapter implements it. An Adapter no plugin requires
// or collects is not an error. A nil adapter is refused with ErrNilAdapter and
// contributes nothing; a typed nil, such as a nil pointer, is not nil. It panics
// if T is not an interface type.
func (r *Registrar) ProvideAdapter[T any](adapter T) {
	id := adapterInterface[T]("ProvideAdapter")
	if any(adapter) == nil {
		r.registry.errs = append(r.registry.errs, ErrNilAdapter{Plugin: r.owner, Interface: id})
		return
	}
	r.registry.adapterContributions[id] = append(r.registry.adapterContributions[id],
		adapterContribution{plugin: r.owner, adapter: adapter})
}

func (r *Registrar) declareAdapter[T any](collects bool, bind func([]adapterContribution)) {
	declaration := "RequireAdapter"
	if collects {
		declaration = "CollectAdapters"
	}
	id := adapterInterface[T](declaration)
	for _, existing := range r.registry.adapterDeclarations {
		if existing.iface == id && existing.port == r.owner {
			r.registry.errs = append(r.registry.errs, ErrDuplicateRegistration{
				Kind: "adapter declaration", Type: id, Owner: r.owner, Existing: existing.port,
			})
			return
		}
	}
	r.registry.adapterDeclarations = append(r.registry.adapterDeclarations, &adapterDeclaration{
		iface: id, port: r.owner, collects: collects, bind: bind,
	})
}

// adapterInterface returns T's type, panicking when T is not an interface: an
// Adapter is keyed by the contract it implements, never by a concrete type.
func adapterInterface[T any](declaration string) reflect.Type {
	id := reflect.TypeFor[T]()
	if id.Kind() != reflect.Interface {
		panic(fmt.Sprintf("kernel: %s type argument %v is not an interface type", declaration, id))
	}
	return id
}

// bindAdapters hands every declaration the contributions for its interface and
// reports each required interface that has none or several. Contributions were
// appended during sequential registration, so they are already in plugin order.
func (r *registry) bindAdapters() []error {
	var errs []error
	for _, declaration := range r.adapterDeclarations {
		contributions := r.adapterContributions[declaration.iface]
		if !declaration.collects {
			switch len(contributions) {
			case 0:
				errs = append(errs, ErrMissingAdapter{Port: declaration.port, Interface: declaration.iface})
				continue
			case 1:
			default:
				errs = append(errs, ErrDuplicateAdapter{
					Port: declaration.port, Interface: declaration.iface, Contributors: contributors(contributions),
				})
				continue
			}
		}
		declaration.bind(contributions)
	}
	return errs
}

func contributors(contributions []adapterContribution) []PluginName {
	names := make([]PluginName, 0, len(contributions))
	for _, contribution := range contributions {
		names = append(names, contribution.plugin)
	}
	return names
}
