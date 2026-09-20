package otosound

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound"
)

// SoundBackend is the Adapter through which otosound fills sound's Backend
// Port.
type SoundBackend kernel.Adapter[sound.BackendPort]
