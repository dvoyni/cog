package ecs

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/kernel/container"
)

// ComponentID is a component type's place in its world, dense from 0 in
// registration order. Plugin order makes it the same on every run with the
// same plugins.
type ComponentID uint32

// RegisterComponent registers component type T in world W under name and
// creates its store, a kernel resource of its own. It is called in a plugin's
// Init, once per world and type, after W is registered.
//
// It is a composition error, and panics, for W not to be registered, for T or
// name to be registered in W twice, for T not to be pointer-free, for a
// marker to follow a field, for two fields to have one path, or for W to hold
// no more component types.
func RegisterComponent[W WorldID, T any](configurator *kernel.Configurator, name ComponentName) ComponentID {
	t := reflect.TypeFor[T]()
	if !container.IsPointerFree(t) {
		panic(fmt.Sprintf("ecs: component %v is not pointer-free", t))
	}
	w := registeredWorld[W](configurator)
	if _, ok := w.storesByType[t]; ok {
		panic(fmt.Sprintf("ecs: component %v is registered in world %v twice", t, reflect.TypeFor[W]()))
	}
	if _, ok := w.storesByName[name]; ok {
		panic(fmt.Sprintf("ecs: component name %q is registered in world %v twice", name, reflect.TypeFor[W]()))
	}
	if len(w.stores) == maskBits[W]() {
		panic(fmt.Sprintf("ecs: world %v holds no more than %d component types", reflect.TypeFor[W](), maskBits[W]()))
	}
	s := newStoreOf[W, T](ComponentID(len(w.stores)), name)
	configurator.InitResource(s)
	w.stores = append(w.stores, &s.storeResource)
	w.storesByType[t] = &s.storeResource
	w.storesByName[name] = &s.storeResource
	return s.component.ID
}
