// Package types declares the concrete types ecs's root aliases, and the
// machinery behind them: the Entity handle and the id authority, Entities; the
// Store and component registration; the System parameters (Query, Spawn,
// WriteableEntities, Get, Set, Remove, Read, Write, In and Resp) with the Query
// fill and the handler builders ToHandler and ToExecute; the filters; List; and
// validation mode.
//
// Every one of them is a type with methods or a generic the root aliases (type
// Entities = types.Entities, type Query[Q any] = types.Query[Q]). They stay
// concrete types - no probe, fill, allocation or despawn goes through an
// interface - and their exported methods are ecs's public API through the
// alias. The root's exported functions forward here. What ecs's internal/ needs
// beyond that goes through the plain function in friends.go: Go allows nothing
// outside bundles/ecs to import this package.
//
// Nothing declared here imports the root, which is what keeps the arrangement
// acyclic. Diagnostics name a type through kernel.TypeName, which renders a
// type declared here under ecs, the name a caller writes.
package types
