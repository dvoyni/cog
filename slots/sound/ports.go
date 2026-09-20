package sound

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/sound/internal/types"
)

// Backend is the interface sound's Adapter implements: a dumb, allocation-free
// sink with a fixed table of slots. It is told how many slots exist once,
// receives one batch per tick, multiplies a gain matrix it did not compute, and
// is asked two questions. It knows nothing of handles, Buses, priorities,
// positions, the cap or the equations.
//
// It is declared in internal/types, beside the Batch it is handed, and aliased
// here:
//
//	interface {
//		Voices(n int)
//		Emit(batch *Batch)
//		Device() Device
//		Prepare(token any, encoded assets.Blob) (PreparedClip, bool, error)
//		TakePrepared() []Prepared
//		Install(PreparedClip) (ClipID, error)
//		Destroy(ClipID)
//	}
//
// What an Adapter owes beyond the signatures is a numbered list in
// docs/specs/sound.md: nothing decodes on the thread that fills the device
// buffer, that thread allocates and locks nothing, the Device rate is never
// baked into a prepared Clip, every parameter change and every stop is
// declicked, a looping Voice is gapless, a prepared value is
// garbage-collectable, and a Device that could never be opened is reported once
// while a lost one is reported never.
type Backend = types.Backend

// BackendPort is the Port sound requires exactly one Adapter for: the mixer an
// Extension such as otosound, jssound or nosound provides. A composition
// without one fails with kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[Backend]
