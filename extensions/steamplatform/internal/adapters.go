package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/platform"
)

// PlatformBackend is the Adapter through which steamplatform fills platform's
// Backend Port.
type PlatformBackend kernel.Adapter[platform.BackendPort]
