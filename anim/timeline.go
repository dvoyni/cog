package anim

import (
	"iter"
	"math"
	"reflect"

	"github.com/dvoyni/cog/m"
)

// slot identifies a track chain by its sequence type and id, so that tracks
// with different sequence types may share an id.
type slot struct {
	sequence reflect.Type
	id       any
}

func slotOf[S any](id any) slot {
	return slot{sequence: reflect.TypeFor[S](), id: id}
}

// track is one queued sequence with its span on the timeline.
type track struct {
	sequence any
	start    float32
	duration float32
	easing   Easing
	loop     bool
}

func (t track) end() float32 {
	return t.start + t.duration
}

func (t track) activeAt(time float32) bool {
	if t.loop {
		return time >= t.start
	}
	return time >= t.start && time <= t.end()
}

func (t track) finishedAt(time float32) bool {
	return !t.loop && time > t.end()
}

func (t track) progress(time float32) float32 {
	if t.duration <= 0 {
		return 1
	}
	progress := (time - t.start) / t.duration
	if t.loop && progress >= 0 {
		progress = float32(math.Mod(float64(progress), 1))
	} else {
		progress = m.Clamp01(progress)
	}
	return t.easing(progress)
}

// cue is a value waiting to fire once the clock reaches at.
type cue struct {
	at    float32
	value any
}

// Timeline is one chain of tracks and cues with its own clock; see the package
// documentation for the chain-point model. It is not a resource of its own:
// timelines are handed out by the Timelines resource and are valid only for the
// handler pass that took them. The zero value is an empty timeline at time zero,
// and a nil *Timeline is the no-op timeline.
type Timeline struct {
	time     float32
	chainEnd float32
	tracks   map[slot][]track
	pending  []cue
	fired    []any
}

// Time is the clock in seconds since the timeline was created or reset. A nil
// timeline reports zero.
func (tl *Timeline) Time() float32 {
	if tl == nil {
		return 0
	}
	return tl.time
}

// Rewind shifts the chain point, where the next chained Add, Cue, or Wait
// starts, by delta seconds. A negative delta overlaps the next track with the
// ones before it; rewinding by a track's full duration starts the next one
// together with it.
func (tl *Timeline) Rewind(delta float32) {
	if tl == nil {
		return
	}
	tl.chainEnd += delta
}

// Wait pushes the chain point duration seconds past the later of now and the
// current chain point, leaving a gap in the chain. The timeline is not idle
// until the gap has passed.
func (tl *Timeline) Wait(duration float32) {
	if tl == nil {
		return
	}
	tl.chainEnd = max(tl.time, tl.chainEnd) + duration
}

// Idle reports that nothing is scheduled: no one-shot track is active or
// pending, no cue is waiting to fire, and the chain point is not in the
// future. Looping tracks are ignored. A nil timeline is idle.
func (tl *Timeline) Idle() bool {
	if tl == nil {
		return true
	}
	if len(tl.pending) > 0 || tl.chainEnd > tl.time {
		return false
	}
	for _, tracks := range tl.tracks {
		for _, t := range tracks {
			if !t.loop {
				return false
			}
		}
	}
	return true
}

// Reset clears the timeline in place: the clock returns to zero and every
// track and cue is dropped.
func (tl *Timeline) Reset() {
	if tl == nil {
		return
	}
	*tl = Timeline{}
}

// FiredCues is the untyped view of the cues that fired on this tick, in the
// order they were queued. The slice is reused by the next advance, so callers
// must not retain it. Fired filters it by type.
func (tl *Timeline) FiredCues() []any {
	if tl == nil {
		return nil
	}
	return tl.fired
}

// advance steps the clock by dt seconds, promotes the cues that came due into
// the fired view (replacing the previous tick's), and drops finished tracks.
func (tl *Timeline) advance(dt float32) {
	tl.time += max(dt, 0)

	clear(tl.fired)
	tl.fired = tl.fired[:0]
	kept := tl.pending[:0]
	for _, c := range tl.pending {
		if c.at <= tl.time {
			tl.fired = append(tl.fired, c.value)
		} else {
			kept = append(kept, c)
		}
	}
	clear(tl.pending[len(kept):])
	tl.pending = kept

	tl.dropFinished()
}

func (tl *Timeline) dropFinished() {
	for key, tracks := range tl.tracks {
		kept := tracks[:0]
		for _, t := range tracks {
			if !t.finishedAt(tl.time) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(tl.tracks, key)
		} else {
			clear(tracks[len(kept):])
			tl.tracks[key] = kept
		}
	}
}

// current returns the track that determines the slot's value and its state:
// the active one if any (the newest when several overlap), otherwise the
// earliest still-pending one, otherwise nil.
func (tl *Timeline) current(key slot) (*track, State) {
	tracks := tl.tracks[key]

	var pending *track
	for index := len(tracks) - 1; index >= 0; index-- {
		t := &tracks[index]
		if t.activeAt(tl.time) {
			return t, StateActive
		}
		if t.start > tl.time && (pending == nil || t.start < pending.start) {
			pending = t
		}
	}

	if pending == nil {
		return nil, StateNotFound
	}
	return pending, StatePending
}

// Add queues sequence under the slot (type of S, id). Tracks under one slot
// append rather than replace, so a slot may hold tracks that play back to
// back. The new track starts at the chain point, or now when params.Immediate,
// and unless it loops the chain point moves to its end, so successive adds
// chain; Rewind pulls the chain point back to overlap them. Finished tracks
// are dropped on the next advance. A nil timeline drops the add.
func (tl *Timeline) Add[S Sequence[R], R any](id any, sequence S, params Params) {
	if tl == nil {
		return
	}
	if tl.tracks == nil {
		tl.tracks = map[slot][]track{}
	}

	easing := params.Easing
	if easing == nil {
		easing = Linear
	}

	start := tl.time
	if !params.Immediate {
		start = max(start, tl.chainEnd)
	}

	key := slotOf[S](id)
	tl.tracks[key] = append(tl.tracks[key], track{
		sequence: sequence,
		start:    start,
		duration: params.Duration,
		easing:   easing,
		loop:     params.Loop,
	})
	if !params.Loop {
		tl.chainEnd = start + params.Duration
	}
}

// Query returns the track that determines the slot's value, with its eased
// progress and state: StateActive while it plays, StatePending when it is
// queued but not started (progress is then the easing of 0), or StateNotFound
// when nothing is stored under the slot or the timeline is nil.
func (tl *Timeline) Query[S Sequence[R], R any](id any) (sequence S, progress float32, state State) {
	if tl == nil {
		return sequence, 0, StateNotFound
	}

	current, state := tl.current(slotOf[S](id))
	if current == nil {
		return sequence, 0, StateNotFound
	}

	sequence, ok := current.sequence.(S)
	if !ok {
		return sequence, 0, StateNotFound
	}
	return sequence, current.progress(tl.time), state
}

// Value returns the slot's current value, or fallback when nothing is stored
// under it.
func (tl *Timeline) Value[S Sequence[R], R any](id any, fallback R) R {
	sequence, progress, state := tl.Query[S, R](id)
	if !state.Found() {
		return fallback
	}
	return sequence.At(progress)
}

// Cue queues value to fire when the clock reaches the chain point, like a
// zero-duration track. The cue fires on the first advance that reaches its
// time, never on the tick that queued it, and is then visible through Fired
// for that one tick. A nil timeline drops the cue.
func (tl *Timeline) Cue(value any) {
	if tl == nil {
		return
	}
	at := max(tl.time, tl.chainEnd)
	tl.chainEnd = at
	tl.pending = append(tl.pending, cue{at: at, value: value})
}

// Fired yields the cues of type T that fired on this tick, in the order they
// were queued. Handlers that act on cues should drain them every tick they
// run, before any early return, since the next advance clears the view.
func (tl *Timeline) Fired[T any]() iter.Seq[T] {
	return func(yield func(T) bool) {
		if tl == nil {
			return
		}
		for _, value := range tl.fired {
			if typed, ok := value.(T); ok && !yield(typed) {
				return
			}
		}
	}
}
