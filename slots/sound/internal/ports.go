package internal

import "github.com/dvoyni/cog/kernel"

// BackendPort is the Port sound requires exactly one Adapter for: the mixer an
// Extension such as otosound, jssound or nosound provides. A composition
// without one fails with kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[Backend]
