// Package internal holds what ecs's contract root and ecsimpl share but no
// consumer may reach: the Entity handle and the id authority, Entities, whose
// construction is the whole of what ecsimpl does.
//
// Entity and Entities are declared here with their state unexported and
// aliased in the root (type Entities = internal.Entities). Both stay concrete
// types - no probe, allocation or despawn goes through an interface - and their
// exported methods (Entity.String, Entities.Alive) are ecs's public API through
// the alias. What the root and ecsimpl need beyond that goes through the plain
// functions in friends.go.
//
// Nothing declared here imports the root, which is what keeps the arrangement
// acyclic. So the authority holds what it needs of the root's machinery erased:
// an enrolled Store is the one call a despawn makes of it, and a Component's
// registration record is a value only the root reads back.
//
// Only the root and ecsimpl can import this package: Go allows nothing outside
// bundles/ecs to.
package internal
