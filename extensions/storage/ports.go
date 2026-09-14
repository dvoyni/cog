package storage

import "github.com/dvoyni/cog/kernel"

// PermanentFSPort is the Port storage requires exactly one Adapter for: the
// permanent filesystem a platform plugin such as diskfs or jsfs provides. A
// composition without one fails with kernel.ErrMissingAdapter.
type PermanentFSPort kernel.RequiredPort[PermanentFS]
