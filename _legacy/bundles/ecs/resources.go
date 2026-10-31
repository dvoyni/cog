package ecs

import "github.com/dvoyni/cog/bundles/ecs/internal"

// Entities is the id authority: it allocates indices, tracks their generations,
// answers whether a handle is alive, and holds a reference to every Store so a
// despawn can empty all of them. There is exactly one per Engine, and that is
// what makes an Engine the boundary of one simulation.
//
// It is a kernel resource, published by the ecs plugin and by nothing else.
// Every handler that touches any Store declares read{*Entities}; spawning and
// despawning take it for write, which is why neither is a method a reader can
// reach: the authority to change which entities exist arrives through the
// write-locked promotions of this value — Spawn and WriteableEntities — and
// nowhere else. Alive is the one question it answers a reader.
type Entities = internal.Entities

// Store is the holding of every value of one Component type, one per registered
// type, and the unit a lock is taken on. RegisterComponent creates it and hands
// it to the kernel as a resource of type *Store[T], owned by the registering
// plugin.
//
// Len is the population; Has, Get and Ref probe one Entity, Set writes one and
// Remove takes one away. A Store must be reached as *Store[T]: a value store
// makes a kernel write handle's Get return a copy, so mutations through it are
// silently discarded. A System reaches a Store through a Query or an accessor,
// never through Read or Write.
type Store[T any] = internal.Store[T]
