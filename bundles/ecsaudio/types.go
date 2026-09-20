package ecsaudio

import (
	"github.com/dvoyni/cog/libs/m"
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
// Position and Orientation are the two fields the Transform fills in, and what
// an Emitter says about them is described there.
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

// Transform is where an Entity is heard from: m.Transform as a type of this
// package's own, exactly as ecsscene.Transform is. Scale means nothing to audio
// and is ignored.
//
// It is defined from m.Transform rather than aliased to it so that the Store's
// Go type belongs to this package: a System elsewhere that names it imports
// ecsaudio, and the import graph keeps forcing the plugin dependency the
// coupling check expects. Convert with m.Transform(t) and Transform(t).
//
// It is m.Transform and never scene.Transform, because reaching the transform
// through scene would make every game with sound depend on the renderer. The
// axes need no conversion: sound faces -Z with +Y up, as the ECS spotlight does,
// so a Transform a game already keeps for rendering is read straight across.
//
// On a Listener Entity it is copied whole - Position and Rotation both - into
// sound.ListenerParams.
//
// On an Emitter Entity it supplies the Position, always; and the Orientation,
// only when that Emitter's Params carry a Cone. Orientation is withheld
// otherwise because absent is not the identity: sound reads a Positional Voice
// with no Orientation as equally loud in every direction, and a binding that
// sent the Rotation of every Transform would make every source in the game
// directional along an axis nobody chose. Both replace what Params said, because
// a Transform is the Entity's placement and Params are everything else about the
// sound.
//
// Two consequences of sound's one-way positional rule, worth stating because
// neither is guessable:
//
//   - Adding a Transform to an Entity whose Voice is already playing does not
//     make that Voice positional. It takes effect on the next Play.
//   - Removing a Transform does not make a positional Voice non-positional
//     either. It simply stops moving.
type Transform m.Transform

// Listener marks the one Entity the world is heard from. The binding copies its
// Transform into sound's Listener every tick.
//
// An Entity carrying it with no Transform is ignored, and counts as none; no
// Entity carrying it at all leaves the Listener where it was; two or more report
// ErrManyListeners once and the lowest Entity is heard from, on every tick and
// not just the first.
type Listener struct{}
