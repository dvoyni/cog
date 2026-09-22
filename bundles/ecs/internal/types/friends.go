package types

import "github.com/dvoyni/cog/kernel"

// The friend functions: what ecs's internal/ does to the authority's unexported
// state. Only packages under bundles/ecs can import this package, so these are
// not public API.

// NewEntities creates the authority for the ecs plugin, reserving room for ids
// indices. See newEntities.
func NewEntities(ids uint32) *Entities { return newEntities(ids) }

// ShrinkCommand is the factory the ecs plugin registers ShrinkCmd with. See
// shrinkCommand.
func ShrinkCommand() (kernel.Lock, kernel.Execute[ShrinkRequest, ShrinkResponse]) {
	return shrinkCommand()
}

// CensusCommand is the factory the ecs plugin registers the census with.
// See censusCommand.
func CensusCommand() (kernel.Lock, kernel.Execute[CensusRequest, CensusResponse]) {
	return censusCommand()
}

// EntityCommand is the factory the ecs plugin registers the one-Entity read
// with. See entityCommand.
func EntityCommand() (kernel.Lock, kernel.Execute[EntityRequest, EntityResponse]) {
	return entityCommand()
}

// QueryCommand is the factory the ecs plugin registers the read by Component
// names with. See queryCommand.
func QueryCommand() (kernel.Lock, kernel.Execute[QueryRequest, QueryResponse]) {
	return queryCommand()
}
