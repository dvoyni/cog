package ecsaudio

import (
	"github.com/dvoyni/cog/slots/sound"
)

// Emitter is what this Entity sounds like. Adding one starts a Voice, removing
// one stops it, and changing the Clip stops the old Voice and starts a new one;
// changing anything in Params restates the running Voice.
//
// It is rarely written. Params is sound's own, so an absent m.Maybe field means
// "the default" to the Play that starts the Voice and "unchanged" to every
// restatement after it - which is why an Emitter nothing touched costs a
// SetVoice that says nothing, and why the zero value of a Falloff or a Cone it
// does carry is W3C's defaults rather than the literal zeros. Both rules are
// sound's and neither is restated here; see sound.Falloff and sound.Cone.
//
// Position and Orientation are the two fields an m.Transform on the same Entity
// fills in. It supplies the Position, always; and the Orientation, only when
// the Emitter's Params carry a Cone. Orientation is withheld otherwise because
// absent is not the identity: sound reads a Positional Voice with no
// Orientation as equally loud in every direction, and a binding that sent the
// Rotation of every Transform would make every source in the game directional
// along an axis nobody chose. Both replace what Params said, because a
// Transform is the Entity's placement and Params are everything else about the
// sound. Its Scale means nothing to audio and is ignored.
//
// Two consequences of sound's one-way positional rule, worth stating because
// neither is guessable:
//
//   - Adding a Transform to an Entity whose Voice is already playing does not
//     make that Voice positional. It takes effect on the next Play.
//   - Removing a Transform does not make a positional Voice non-positional
//     either. It simply stops moving.
//
// An Emitter and a live Voice are not the same thing, and the difference is the
// one rule of this package worth learning: an entry in the binding's table
// outlives the Voice it names. A one-shot that finished, or a Voice stolen under
// sound's cap, leaves an Emitter sitting on an Entity with nothing playing, and
// the binding does not re-play it. Re-triggering the same Clip on the same
// Entity is remove-then-re-add, or sound's queue directly.
type Emitter struct {
	// Clip is what to play. A Clip that differs from the running Voice's - by
	// sound.ClipRef.Equal, since a ClipRef holding a Blob is not comparable -
	// stops that Voice and starts a new one.
	Clip sound.ClipRef
	// Params is how it sounds: Bus, Volume, Pitch, Loop, Paused, Falloff, Cone,
	// and Position and Orientation for an Entity with no Transform. A looping
	// sound is an Emitter whose Params loop; there is no separate concept, and
	// the same table entry tracks it.
	Params sound.Params
}

// Listener marks the one Entity the world is heard from. The binding copies its
// m.Transform whole - Position and Rotation both - into sound's Listener every
// tick.
//
// An Entity carrying it with no Transform is ignored, and counts as none; no
// Entity carrying it at all leaves the Listener where it was; two or more report
// ErrManyListeners once and the lowest Entity is heard from, on every tick and
// not just the first.
type Listener struct{}
