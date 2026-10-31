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
