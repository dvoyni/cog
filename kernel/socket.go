package kernel

import (
	"fmt"
	"reflect"
	"strings"
)

// ExclusiveSocket is the handle to a socket taking one binding, returned by
// DeclareExclusiveSocket.
type ExclusiveSocket[T any] interface {
	// Get answers the socket's binding, or the zero T for an optional socket
	// left unbound. It takes no lock: the binding is a plain value, its
	// thread rules T's own. It panics in Init, before the bindings resolve.
	Get() T
}

// Socket is the handle to a socket taking any number of bindings, returned
// by DeclareSocket.
type Socket[T any] interface {
	// GetAll answers the socket's bindings in plugin order, which the caller
	// must not change. It takes no lock, and panics in Init, before the
	// bindings resolve.
	GetAll() []T
}

// socket is a declared socket of T, both an ExclusiveSocket and a Socket.
type socket[T any] struct {
	// plugin declared the socket.
	plugin              PluginName
	exclusive, optional bool
	// values are the bindings in plugin order, nil until they resolve.
	values []T
}

// Get answers the one binding, or the zero T when there is none.
func (s *socket[T]) Get() T {
	values := s.GetAll()
	if len(values) == 0 {
		var zero T
		return zero
	}
	return values[0]
}

// GetAll answers the bindings in plugin order.
func (s *socket[T]) GetAll() []T {
	if s.values == nil {
		panic(fmt.Sprintf("kernel: socket %v read in Init, before the bindings resolve; read it from Start on", reflect.TypeFor[T]()))
	}
	return s.values
}

// declaredBy answers the plugin that declared s.
func (s *socket[T]) declaredBy() PluginName { return s.plugin }

// resolve takes the bindings of T, panicking on two of an exclusive socket or
// none of one that is not optional.
func (s *socket[T]) resolve(bindings []socketBinding) {
	t := reflect.TypeFor[T]()
	values := []T{}
	var plugins []string
	for _, binding := range bindings {
		if binding.socket == t {
			values = append(values, binding.value.(T))
			plugins = append(plugins, fmt.Sprintf("%q", binding.plugin))
		}
	}
	if s.exclusive && len(values) > 1 {
		panic(fmt.Sprintf("kernel: exclusive socket %v, declared by plugin %q, has %d bindings, from plugins %s",
			t, s.plugin, len(values), strings.Join(plugins, ", ")))
	}
	if !s.optional && len(values) == 0 {
		panic(fmt.Sprintf("kernel: socket %v, declared by plugin %q, has no binding", t, s.plugin))
	}
	s.values = values
}

// resolvableSocket is a socket of any T, as the kernel holds it until its
// bindings resolve.
type resolvableSocket interface {
	// declaredBy answers the plugin that declared the socket.
	declaredBy() PluginName
	// resolve takes the socket's own from bindings, every binding in plugin
	// order, panicking if they break its exclusive or optional.
	resolve(bindings []socketBinding)
}

// socketBinding is one value a plugin binds to the socket of a type.
type socketBinding struct {
	socket reflect.Type
	plugin PluginName
	value  any
}
