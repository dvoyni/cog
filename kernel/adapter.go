package kernel

import (
	"fmt"
	"reflect"
	"strings"
)

// adapterDeclaration is one plugin's RequireAdapter or CollectAdapters for one
// Port. bind receives every contribution for that Port, in plugin order, and
// fills the typed handle the declaration returned.
type adapterDeclaration struct {
	port     reflect.Type
	iface    reflect.Type
	owner    PluginName
	collects bool
	bind     func(contributions []adapterContribution)
}

// adapterContribution is one ProvideAdapter call.
type adapterContribution struct {
	plugin  PluginName
	adapter reflect.Type
	value   any
}

// RequiredAdapter is the handle RequireAdapter returns, typed by the Port's
// interface. It is bound at composition and valid from Start onwards.
type RequiredAdapter[I any] struct{ binding *requiredBinding[I] }

type requiredBinding[I any] struct {
	bound   bool
	adapter I
}

// Get returns the one Adapter bound for the Port. It takes no lock: an Adapter
// is a plain value, and its thread rules are the Port interface's. It panics
// before composition has bound the handle.
func (h RequiredAdapter[I]) Get() I {
	if h.binding == nil || !h.binding.bound {
		panic(unboundAdapter[I]())
	}
	return h.binding.adapter
}

// CollectedAdapters is the handle CollectAdapters returns, typed by the Port's
// interface. It is bound at composition and valid from Start onwards.
type CollectedAdapters[I any] struct{ binding *collectedBinding[I] }

type collectedBinding[I any] struct {
	bound    bool
	adapters []ContributedAdapter[I]
}

// ContributedAdapter is one collected Adapter and the plugin that provided it.
type ContributedAdapter[I any] struct {
	Plugin  PluginName
	Adapter I
}

// Get returns every Adapter bound for the Port, in plugin order, as a fresh
// slice. It panics before composition has bound the handle.
func (h CollectedAdapters[I]) Get() []ContributedAdapter[I] {
	if h.binding == nil || !h.binding.bound {
		panic(unboundAdapter[I]())
	}
	return append([]ContributedAdapter[I](nil), h.binding.adapters...)
}

func unboundAdapter[I any]() string {
	return fmt.Sprintf("kernel: adapter handle for %s read before composition bound it", TypeName(reflect.TypeFor[I]()))
}

// RequireAdapter declares that this plugin needs exactly one Adapter for the
// required Port P. Composition binds it after every Register; none fails with
// ErrMissingAdapter and several with ErrDuplicateAdapter. The binding adds no
// plugin dependency. It panics if P is not built on an interface type.
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
// binding adds no plugin dependency. It panics if P is not built on an
// interface type.
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
// on. The parameter has that Port's interface type, so the compiler checks that
// adapter implements it. An Adapter no plugin requires or collects is not an
// error. A nil adapter is refused with ErrNilAdapter and contributes nothing; a
// typed nil, such as a nil pointer, is not nil. It panics if the Port is not
// built on an interface type.
//
// Go infers type parameters from a call's arguments before it reads their
// constraints, so a concrete adapter must already have the interface type:
// convert it, as in ProvideAdapter[GfxBackend](gfx.Backend(device)), or pass a
// value declared with that type.
func (r *Registrar) ProvideAdapter[A AdapterConstraint[P], P portConstraint[K, I], K portKind, I any](adapter I) {
	id := reflect.TypeFor[A]()
	if _, err := portInterface[I]("ProvideAdapter", id); err != nil {
		r.registry.fail(err)
		return
	}
	if any(adapter) == nil {
		r.registry.fail(ErrNilAdapter{Plugin: r.owner, Adapter: id})
		return
	}
	port := reflect.TypeFor[P]()
	r.registry.adapterContributions[port] = append(r.registry.adapterContributions[port],
		adapterContribution{plugin: r.owner, adapter: id, value: adapter})
}

func (r *Registrar) declarePort[P any, I any](collects bool, bind func([]adapterContribution)) {
	declaration := "RequireAdapter"
	if collects {
		declaration = "CollectAdapters"
	}
	port := reflect.TypeFor[P]()
	iface, err := portInterface[I](declaration, port)
	if err != nil {
		r.registry.fail(err)
		return
	}
	for _, existing := range r.registry.adapterDeclarations {
		if existing.port == port && existing.owner == r.owner {
			r.registry.fail(ErrDuplicateRegistration{
				Kind: "port declaration", Type: port, Owner: r.owner, Existing: existing.owner,
			})
			return
		}
	}
	r.registry.adapterDeclarations = append(r.registry.adapterDeclarations, &adapterDeclaration{
		port: port, iface: iface, owner: r.owner, collects: collects, bind: bind,
	})
}

// portInterface returns I's type, and the fault when I is not an interface: a
// Port is a contract its Adapters implement, never a concrete type. named is the
// Port or Adapter type the declaration was given, for the message.
//
// It reports rather than panicking because it has no value it owes anyone: the
// declaration simply does not happen, and composition answers with the reason.
func portInterface[I any](declaration string, named reflect.Type) (reflect.Type, error) {
	iface := reflect.TypeFor[I]()
	if iface.Kind() != reflect.Interface {
		return nil, ErrPortNotAnInterface{Declaration: declaration, Port: named, Interface: iface}
	}
	return iface, nil
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

func describeAdapters(contributions []adapterContribution) []AdapterDescription {
	adapters := make([]AdapterDescription, 0, len(contributions))
	for _, contribution := range contributions {
		adapters = append(adapters, AdapterDescription{Type: contribution.adapter, Plugin: contribution.plugin})
	}
	return adapters
}

// adapterList renders adapters as one bracketed, comma-separated list, each as
// its Adapter type followed by the providing plugin in parentheses.
func adapterList(adapters []AdapterDescription) string {
	rendered := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		rendered = append(rendered, fmt.Sprintf("%s (%s)", TypeName(adapter.Type), adapter.Plugin))
	}
	return "[" + strings.Join(rendered, ", ") + "]"
}
