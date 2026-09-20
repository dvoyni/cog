package sound

import "github.com/dvoyni/cog/slots/sound/internal/types"

// Voice is an opaque handle to one playing sound. It is comparable, copyable
// and usable as a map key, and the zero value means no Voice. A handle is
// minted when a play is recorded, so it is usable in the same tick that
// recorded it, and every operation is a no-op on a Voice that is gone.
type Voice = types.Voice

// NoVoice is the absent handle. Compare with ==.
const NoVoice Voice = types.NoVoice

// Bus is a group of Voices the game declares, sharing one volume. Buses are
// flat: every one of them is directly under Master.
type Bus = types.Bus

// Master is the only Bus sound declares; a game declares the rest. It is zero,
// so an unset Bus lands on Master and a game that declares no Buses still has
// working audio and a working master slider.
const Master Bus = types.Master

// MaxBuses is how many Buses exist, Master among them. It is a fixed maximum
// rather than a configured one, and a Bus outside it is Master rather than an
// error, so a mis-declared constant does not silence a game.
const MaxBuses = types.MaxBuses

// ClipRef names a Clip: a path read through storage, or encoded Ogg bytes the
// caller already holds. Build one with ClipWithResource or ClipWithBytes and
// compare two with Equal.
type ClipRef = types.ClipRef

// Params is "start like this" to Play and "become this" to SetVoice. An absent
// field is the default to Play and unchanged to SetVoice.
type Params = types.Params

// ListenerParams is "become this" to SetListener. An absent field is unchanged.
type ListenerParams = types.ListenerParams

// DistanceModel names which of W3C's three distance models a Falloff uses.
type DistanceModel = types.DistanceModel

// The three W3C distance models. Inverse is the default, and is zero.
const (
	DistanceInverse     = types.DistanceInverse
	DistanceLinear      = types.DistanceLinear
	DistanceExponential = types.DistanceExponential
)

// Falloff is how a Positional Voice gets quieter with distance: W3C's
// PannerNode distance parameters, with the prefixes dropped. Its zero value is
// the W3C defaults, and units are the game's - set Ref to the world's scale.
type Falloff = types.Falloff

// Cone is how a Positional Voice gets quieter off its own axis: W3C's
// PannerNode cone parameters, with the prefixes dropped. Its zero value is the
// W3C defaults, which are no cone at all.
type Cone = types.Cone

// LoopRegion is the span a looping Voice repeats between, in seconds. It is a
// fact about a Clip rather than a parameter of a Voice, declared by the Clip's
// own Vorbis comments and never by the game.
type LoopRegion = types.LoopRegion

// VoiceInfo is one live Voice as Voices shows it.
type VoiceInfo = types.VoiceInfo

// VoiceSlot identifies a voice to an Adapter. It is always in [0, MaxVoices),
// minted and recycled by sound, which is what lets an Adapter's voice table be
// a fixed array indexed directly by it.
type VoiceSlot = types.VoiceSlot

// ClipID is an opaque handle minted by the Adapter. Zero means none.
type ClipID = types.ClipID

// VoiceParams is everything about how a voice sounds, as the Adapter sees it:
// a 2x2 gain matrix, a rate and a paused flag, and no equation below the seam.
type VoiceParams = types.VoiceParams

// VoiceStart begins one voice in one slot.
type VoiceStart = types.VoiceStart

// VoiceUpdate restates one slot's parameters.
type VoiceUpdate = types.VoiceUpdate

// Batch is one tick's operations, handed to an Adapter in a single Emit. It is
// owned by sound and valid only for the duration of that call.
type Batch = types.Batch

// PreparedClip is a Clip turned into something a Voice can be started from.
// Which kind it is - shared samples, or bytes and a way to read them - is the
// Adapter's own business and a game cannot tell.
type PreparedClip = types.PreparedClip

// Prepared is one completed prepare, drained at the top of a flush.
type Prepared = types.Prepared
