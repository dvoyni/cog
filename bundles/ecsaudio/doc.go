// Package ecsaudio records Entities into sound. An Entity declares what it
// sounds like and where it is; the binding turns that into plays, parameter
// updates and stops on sound's queue, and a Voice attached to an Entity dies
// with it.
//
// It is a binding and nothing else: two Components of its own and one recording
// System, which is what the ecs prefix means in this repo - ecsscene,
// ecsphysics2d. No
// commands, no state a game addresses, and no arithmetic. Every equation is
// sound's, and this package computes nothing.
//
// It records into the same sound.Queue a game without ECS records into. Neither
// face reimplements the other and a game may use both at once, which is what
// makes the explosion case - a sound that must outlive the Entity that made it -
// the queue face's rather than a flag here.
//
// # The three declarations
//
// Emitter is what an Entity sounds like: a sound.ClipRef and the sound.Params it
// plays with. m.Transform is where it is heard from, with Scale ignored because
// scale means nothing to audio. It is not this package's: the ecs plugin
// registers its one Store, the same one ecsscene draws from, so a game keeps one
// placement per Entity and never copies it between a renderer's and a mixer's.
// It is m.Transform and never reached through scene, because that would make
// every game with sound depend on the renderer. The axes need no conversion:
// sound faces -Z with +Y up, as the ECS spotlight does. Listener is the Tag
// marking the one Entity the world is heard from.
//
// Emitter and Transform are two Components rather than one for the reason
// ecsphysics2d keeps Force apart from Position: a System copying transforms
// every tick must not serialise against the System that changes a Clip once an
// hour. One Component would make them one lock.
//
// An Emitter with no Transform is a non-positional Voice - background music, a
// UI click - which is what "heard from nowhere in particular" already describes.
//
// # The correspondence, and why an entry outlives its Voice
//
// The Components are the intent; the binding owns the correspondence. It keeps a
// plugin-owned Entity-to-Voice table, deliberately not a Component, and
// reconciles once a tick: an Emitter with no entry is played, an entry is
// restated with SetVoice, a changed Clip is a Stop and a fresh Play, and an
// entry whose Entity stopped matching - despawned, or its Emitter removed - is a
// Stop and a dropped entry.
//
// An entry outlives its Voice. A one-shot that finished leaves an entry holding
// a dead handle and the binding does not re-play: were the rule instead "an
// Emitter with no live Voice gets a Play", every one-shot in the game would
// restart forever, because a finished Voice is exactly a Voice that is no longer
// live. A Voice stolen under sound's cap is treated the same way and for the
// same reason. Re-triggering the same Clip on the same Entity is
// remove-then-re-add, or the queue face.
//
// That is also why the System reads no sound resource but the queue. Every
// operation is a no-op on a Voice that is gone, so a stale handle needs no guard
// and no query, and reading the live Voice view would widen the lock set for
// nothing.
//
// # The System
//
// One System, ordered as ecsaudio.RecordOnUpdate. Its lock set is the queue for
// write, the table for write, and a read query over the Components. It writes no
// Component and takes no structural lock, which is the whole point of the table
// being plugin-owned: a frame that spawns a hundred emitters would otherwise be
// a hundred structural changes for bookkeeping nobody outside the binding reads.
//
// # The Listener
//
// The binding writes SetListener from the Transform of the Entity carrying the
// Listener Tag, copying Position and Rotation across. sound never reads a camera
// and neither does this; a game that wants the Listener on its camera puts the
// Tag on the camera's Entity.
//
// With no such Entity the binding writes nothing, leaving the Listener where it
// was: resetting to the origin would swing every positional sound in the world
// the instant a listener Entity is despawned mid-level. With two or more it
// reports ErrManyListeners once and takes the lowest Entity, every tick and not
// only the first, because an arbitrary pick that changed with iteration order is
// a sound bug nobody can reproduce. A Listener on an Entity with no Transform is
// ignored, and counts as none.
//
// In a 2D game the Tag does not go on the player sprite: a sprite's rotation is
// about Z, which leaves the Listener's forward at (0,0,-1) at every angle. It
// goes on an Entity that exists to be heard from, whose Rotation is the fixed
// m.QuatRotationX(-math.Pi/2) sound specifies for 2D.
//
//	world.Spawn(
//		ecsaudio.Listener{},
//		m.Transform{
//			Position: playerPos,
//			Rotation: m.QuatRotationX(-math.Pi / 2),
//		},
//	)
//
// Nothing 2D-specific is added here: no mode, no field, no flag.
//
// ecsaudio is a Bundle. Its plugin, built by ecsaudioplugin.New, requires no
// Adapter and contributes none; register ecs and sound beside it. The Component
// registrations, the table and the one System are in internal/. The Components
// are plain data with no methods, so there is no internal/types.
//
// bundles/ecsaudio/docs/specs/ecsaudio.md is the specification this package is
// judged against.
package ecsaudio
