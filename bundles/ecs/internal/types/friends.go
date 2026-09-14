package types

// The friend functions: what ecs's internal/ does to the authority's unexported
// state. Only packages under bundles/ecs can import this package, so these are
// not public API.

// NewEntities creates the authority for the ecs plugin, reserving room for ids
// indices. See newEntities.
func NewEntities(ids uint32) *Entities { return newEntities(ids) }
