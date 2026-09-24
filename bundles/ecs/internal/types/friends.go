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

// SpawnCommand is the factory the ecs plugin registers the spawn by name
// with. See spawnCommand.
func SpawnCommand() (kernel.Lock, kernel.Execute[SpawnRequest, SpawnResponse]) {
	return spawnCommand()
}

// DespawnCommand is the factory the ecs plugin registers the despawn by
// handle with. See despawnCommand.
func DespawnCommand() (kernel.Lock, kernel.Execute[DespawnRequest, DespawnResponse]) {
	return despawnCommand()
}

// UpdateCommand is the factory the ecs plugin registers the update by name
// with. See updateCommand.
func UpdateCommand() (kernel.Lock, kernel.Execute[UpdateRequest, UpdateResponse]) {
	return updateCommand()
}

// QueryCommand is the factory the ecs plugin registers the read by Component
// names with. See queryCommand.
func QueryCommand() (kernel.Lock, kernel.Execute[QueryRequest, QueryResponse]) {
	return queryCommand()
}
