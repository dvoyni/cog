package sound

import (
	"github.com/dvoyni/cog/kernel"
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

// ClipInfoOf asks what sound knows about a Clip: its duration, channels and
// sample rate, and where it is between being named and being playable.
//
// Asking never starts a load. It reads sound's own table and never the Library,
// whose Get is the only read it has and loads on a miss - so a Clip nothing has
// named reports zero facts and ClipLoading, and stays there until something
// plays or preloads it.
//
// It is the question, and ClipInfo is the answer. The two cannot share a name
// in one package, so the function is the one that moved: gfx names its free
// functions for the question and its types for the answer - SnapshotViewOf
// returns a SnapshotView, TextureViewOf a TextureView - and this is that shape.
// The spec spells the function ClipInfo; it could not compile, and the name the
// spec really pins is the type's.
func ClipInfoOf(handle kernel.Read[*Clips], ref ClipRef) (ClipInfo, State) {
	return types.ClipsInfo(handle, ref)
}

// DefaultFalloff is the falloff of a Positional Voice that was given none:
// W3C's PannerNode defaults, Ref 1 and Max 10000 and Rolloff 1 on the inverse
// model. An author moving one field starts here, so the other three are not
// silently zeroed - and the field to move is Ref, which is W3C's metres and a
// trap in any world that is not measured in them.
func DefaultFalloff() Falloff { return types.DefaultFalloff() }

// DefaultCone is the cone of a Voice that was given none: W3C's PannerNode
// defaults, which are 360 degrees of inner cone and so no cone at all.
func DefaultCone() Cone { return types.DefaultCone() }
