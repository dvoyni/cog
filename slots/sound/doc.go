// Package sound declares the sound Slot: Clips played as Voices, grouped on
// Buses, with all of the arithmetic computed here and none of it below. A game
// records operations into one Queue resource under its write lock, sound
// flushes that queue once a tick - drain, apply in recorded order, compute,
// Emit - and reads back three live views: the Voices, the Clips and the Device.
//
// It is the machine, not a convenience layer. bundles/ecsaudio drives the same
// queue from ECS and reimplements nothing; a game without ECS records into the
// queue directly, as a canvas game records into gfx.
//
// Three properties shape the whole of it. The queue speaks in position and
// Listener, never in gain and pan, which is what keeps a later HRTF effort from
// redrawing what a game says. A Voice is retained rather than re-declared - a
// music track seeked partway through is not derivable from a frame's
// declarations - which is why sound holds a table and gfx holds none. And the
// Adapter is a dumb sink: it is handed a gain matrix and a rate, and knows
// nothing of handles, Buses, priorities, positions, the cap or the equations.
//
// sound is a Slot: its plugin, built by soundplugin.New, requires exactly one
// Backend Adapter through BackendPort, which an Extension provides - otosound
// on desktop, jssound in a browser, nosound wherever nothing needs to be heard.
// A game composes one per platform, in build-tagged files; there is no knob
// selecting a backend, because which one runs is the target triple's answer.
//
// Nothing here fails. No operation returns an error, every operation is a no-op
// on a Voice that is gone, and the only failure a game observes is a Clip that
// ended its Voices with ReasonFailed.
//
// slots/sound/docs/specs/sound.md is the specification this package is judged
// against.
package sound
