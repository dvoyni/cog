package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound"
)

// SoundBackend is the Adapter through which jssound fills sound's Backend Port.
type SoundBackend kernel.Adapter[sound.BackendPort]
