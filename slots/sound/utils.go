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

// DefaultFalloff is the falloff of a Positional Voice that was given none:
// W3C's PannerNode defaults, Ref 1 and Max 10000 and Rolloff 1 on the inverse
// model. An author moving one field starts here, so the other three are not
// silently zeroed - and the field to move is Ref, which is W3C's metres and a
// trap in any world that is not measured in them.
func DefaultFalloff() Falloff { return types.DefaultFalloff() }

// DefaultCone is the cone of a Voice that was given none: W3C's PannerNode
// defaults, which are 360 degrees of inner cone and so no cone at all.
func DefaultCone() Cone { return types.DefaultCone() }
