package ecs

import (
	"reflect"

	"github.com/dvoyni/cog/bundles/ecs/internal"
)

// Entities is the id authority: it allocates indices, tracks their generations,
// answers whether a handle is alive, and holds a reference to every Store so a
// despawn can empty all of them. There is exactly one per Engine, and that is
// what makes an Engine the boundary of one simulation.
//
// It is a kernel resource, published by ecsimpl and by nothing else. Every
// handler that touches any Store declares read{*Entities}; spawning and
// despawning take it for write, which is why neither is a method a reader can
// reach: the authority to change which entities exist arrives through the
// write-locked promotions of this value — Spawn and WriteableEntities — and
// nowhere else. Alive is the one question it answers a reader.
type Entities = internal.Entities

// classOf reports what registration baked for a Component type, or nil if no
// plugin ever registered it. A Query asks this while it is planned, which is
// what turns "no such Component" into a composition failure naming the
// Component rather than a missing resource naming a store type nobody wrote.
//
// The authority keeps the record erased, because it cannot name this package's
// type; the assertion here is registration-time work and nothing a tick pays.
func classOf(en *Entities, componentType reflect.Type) *componentClass {
	class, _ := internal.EntitiesClassOf(en, componentType).(*componentClass)
	return class
}
