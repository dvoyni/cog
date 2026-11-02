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

// componentInfo is what the registry keeps of a component type, for code
// that works on rows without the type.
type componentInfo struct {
	ID    ComponentID
	Type  reflect.Type
	Size  uintptr
	Align uintptr
}

// RegisterComponent registers component type T in world W and creates its
// store, a kernel resource of its own. It is called in a plugin's Init, once
// per world and type, after W is registered.
//
// It is a composition error, and panics, for W not to be registered, for T to
// be registered in W twice, for T not to be pointer-free, or for W to hold no
// more component types.
func RegisterComponent[W WorldID, T any](configurator *kernel.Configurator) ComponentID {
	t := reflect.TypeFor[T]()
	if !container.IsPointerFree(t) {
		panic(fmt.Sprintf("ecs: component %v is not pointer-free", t))
	}
	w := registeredWorld[W](configurator)
	if _, ok := w.storesByType[t]; ok {
		panic(fmt.Sprintf("ecs: component %v is registered in world %v twice", t, reflect.TypeFor[W]()))
	}
	if len(w.stores) == maskBits[W]() {
		panic(fmt.Sprintf("ecs: world %v holds no more than %d component types", reflect.TypeFor[W](), maskBits[W]()))
	}
	s := newStoreOf[W, T](ComponentID(len(w.stores)))
	configurator.InitResource(s)
	w.stores = append(w.stores, &s.store)
	w.storesByType[t] = &s.store
	return s.component.ID
}
