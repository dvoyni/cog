package internal

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// The two Queries the recording System walks. Every field is a read - a value
// field yields a copy - because recording changes nothing about an Entity.
//
// Transform is deliberately not a field of emitterQuery. A Query matches an
// Entity having at least the Components it names, so naming it would drop every
// non-positional Emitter out of the walk; it is reached through an accessor
// instead, at one probe each.
//
// listenerQuery names Transform because a Listener Tag on an Entity with no
// Transform is ignored and counts as none, which is exactly what leaving it out
// of the walk means.
type (
	emitterQuery struct {
		Emitter Emitter
	}
	listenerQuery struct {
		Listener Listener
		Place    m.Transform
	}
)

// manyListenersKey is the key ErrManyListeners is reported under: a singleton
// condition names its own empty struct type, so that it shares a namespace with
// nothing.
type manyListenersKey struct{}

// recordSystem is the binding: every Emitter reconciled against the Voice it
// already has, and the Listener copied across, into sound's queue once a tick.
//
// Its signature is its whole lock set - the Stores it reads, the table it owns
// and sound's queue it writes. It writes no Component and names no
// WriteableEntities, so it takes no structural lock and has nothing to serialise
// against; and it reads no sound resource but the queue, because an entry
// outliving its Voice is the whole policy and a dead handle is
// indistinguishable from a live one as far as every operation is concerned.
//
// It declares no ordering: sound's flush is subscribed Last, so an
// ordinary-phase System already runs before it, and a game System that moves
// Transforms orders itself Before[ecsaudio.RecordOnUpdate].
func recordSystem(
	emitters *ecs.Query[emitterQuery],
	listeners *ecs.Query[listenerQuery],
	places *ecs.Get[m.Transform],
	correspondence *ecs.Write[*table],
	out *ecs.Write[*sound.Queue],
	k kernel.Kernel,
) {
	// Both handles are read once, outside the loops: neither value is a place
	// to keep anything past the body of this call.
	voices, queue := correspondence.Get(), out.Get()
	voices.begin()
	for e, it := range emitters.All() {
		place, placed := places.Of(e)
		held, known := voices.of(e)
		switch {
		case !known:
			play(queue, voices, e, it.Emitter, place, placed)
		case !held.clip.Equal(it.Emitter.Clip):
			// A changed Clip is a new sound, not a new parameter: there is no
			// field of Params that says which Clip, and there never will be.
			queue.Stop(held.voice)
			play(queue, voices, e, it.Emitter, place, placed)
		default:
			// An absent Maybe field means unchanged, so an Emitter nothing
			// touched costs one operation that says nothing - and a SetVoice on
			// the dead handle of a finished one-shot costs nothing at all.
			queue.SetVoice(held.voice, params(it.Emitter, place, placed && held.positional))
			voices.keep(e, held)
		}
	}
	voices.sweep(queue)
	setListener(queue, listeners, k)
}

// play starts the Voice an Emitter with no entry asks for, and records the
// correspondence. Whether the Play carried a Position is what the entry keeps,
// because that is what decides whether a Transform may move this Voice later.
func play(
	queue *sound.Queue, voices *table,
	e ecs.Entity, emitter Emitter, place m.Transform, placed bool,
) {
	started := params(emitter, place, placed)
	// The offset is zero because a Seek is not the binding's: an Emitter says
	// what to play, and where a playhead stands is sound's queue face.
	voices.put(e, queue.Play(emitter.Clip, 0, started), emitter.Clip, started.Position.Present())
}

// params is an Emitter's own Params with the Transform folded in: the Position
// always, and the Orientation only when the Emitter asked for a Cone.
//
// Withholding the Orientation is the whole of it. sound reads a Positional Voice
// with no Orientation as equally loud in every direction whatever its Cone says,
// so absent is not the identity, and a binding that sent the Rotation of every
// Transform would make every source in the game directional along an axis nobody
// chose. Asking for a Cone is the only way an author says otherwise.
//
// place is folded in only when fold is set, which is what keeps a Transform from
// spatializing a Voice that was played without one: sound's positional rule is
// one-way, so a Position arriving by SetVoice would make that Voice positional
// for the rest of its life.
func params(emitter Emitter, place m.Transform, fold bool) sound.Params {
	out := emitter.Params
	if !fold {
		return out
	}
	out.Position = m.Some(place.Position)
	if out.Cone.Present() {
		out.Orientation = m.Some(place.Rotation)
	}
	return out
}

// setListener copies the Transform of the Entity carrying the Listener Tag into
// sound's Listener, Position and Rotation both. sound never reads a camera and
// neither does this.
//
// With none, nothing is written and the Listener stays where it was: resetting
// to the origin would swing every positional sound in the world the instant a
// listener Entity is despawned mid-level, which is the one moment a game can
// least afford it. With two or more the lowest Entity is heard from, chosen the
// same way on every tick, and the condition is reported once.
func setListener(queue *sound.Queue, listeners *ecs.Query[listenerQuery], k kernel.Kernel) {
	lowest, place, count := ecs.NoEntity, m.Transform{}, 0
	for e, it := range listeners.All() {
		count++
		if lowest == ecs.NoEntity || e < lowest {
			lowest, place = e, it.Place
		}
	}
	if count == 0 {
		return
	}
	if count > 1 {
		k.ReportErrorOnce(manyListenersKey{}, ErrManyListeners{Count: count})
	}
	queue.SetListener(sound.ListenerParams{
		Position:    m.Some(place.Position),
		Orientation: m.Some(place.Rotation),
	})
}
