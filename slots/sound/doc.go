// Package sound declares the sound Slot: Clips played as Voices, grouped on
// Buses, with all of the arithmetic computed here and none of it below. A game
// records operations into one Queue resource under its write lock, sound
// flushes that queue once a tick - drain, apply in recorded order, compute,
// Emit - and reads back five live views: the Voices, the Clips, the Buses, the
// Listener and the Device.
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
// # Positional audio
//
// One model serves both renderers. A position is always an m.Vec3, a facing
// always an m.Quat, and a 2D sound is a 3D sound lying on the Z=0 plane. There
// is no Vec2, no 2D mode and no pan. The axes are right-handed, forward -Z, up
// +Y, right +X, so an unrotated Listener is the W3C default Listener and a
// speaker and a spotlight on one Transform point the same way.
//
// The arithmetic is the W3C Web Audio API specification's, transcribed from the
// W3C document and adopted normatively, so that a future PannerNode backend
// agrees with our own mixer by construction rather than by careful matching.
// Every parameter's doc comment names the W3C parameter it corresponds to.
//
// Set Falloff.Ref to the world's own scale. Units are the game's and sound
// never learns one, so W3C's default Ref of 1 is metres, and off metre scale it
// is a trap: on the defaults a source at radius 4 is -12 dB and at radius 10 is
// -20 dB - correct, and far quieter than an author expects. A game measured in
// pixels declares its own Falloff once, as a constant, and passes it on every
// play. Start from DefaultFalloff so that moving one field does not silently
// zero the other three. Nothing about spatialization is per Bus: a Bus is a
// volume, and a node-graph backend would otherwise have to copy Bus state onto
// every node.
//
// A stereo Clip is panned rather than mixed down to mono, because mixing down
// is a departure a PannerNode backend would have to imitate by hand. What W3C's
// stereo arm does is pass one channel at unity and bleed the other into it, so
// a stereo source panned hard right is its left channel folded into its right
// and not its left channel silenced. Whether a positional Clip should be mono
// is the game's judgement and not the engine's; sound pans what it is given.
//
// A 2D game must rotate its Listener, once:
//
//	queue.SetListener(sound.ListenerParams{
//		Orientation: m.Some(m.QuatRotationX(-math.Pi / 2)),
//	})
//
// That is forward (0,-1,0), up (0,0,-1), right (1,0,0): with canvas's Y-down,
// ahead is up the screen, up is out of the screen, right is screen-right.
// Leaving it out is not a degraded pan but a broken one - panning projects the
// up axis away before taking a bearing, and the Z=0 plane contains +Y, so every
// sprite with positive X would be hard right whatever its Y, with the distances
// still correct in every case and nothing reporting anything. A directional
// emitter in the same game turns in-plane off the same quaternion:
// m.QuatRotationZ(theta).Mul(base).
//
// slots/sound/docs/specs/sound.md is the specification this package is judged
// against.
package sound
