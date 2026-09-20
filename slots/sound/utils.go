package sound

import (
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound/internal/types"
)

// ClipWithResource names a Clip by a storage path. The bytes are read inside
// sound's flush, on the tick the Play that needs them was recorded.
func ClipWithResource(path string) ClipRef { return types.ClipWithResource(path) }

// ClipWithBytes names a Clip by encoded Ogg the caller already holds. Build the
// Blob once and keep it: a fresh one every call names an asset nothing can ask
// for twice.
func ClipWithBytes(ogg assets.Blob) ClipRef { return types.ClipWithBytes(ogg) }
