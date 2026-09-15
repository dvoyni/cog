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
