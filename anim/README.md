# anim

`github.com/dvoyni/cog/anim` provides timelines of eased value tracks and
one-tick cues. A game declares what a value should do over time (a sequence,
a duration, an easing) and reads the current value each tick; the plugin
advances every timeline by the fixed step, so nothing else needs to tick.

## Files

`contract.go` holds the package documentation, `Params`, and `State`;
`easing.go` the easing curves; `sequence.go` the `Sequence` interface and
`Lerp`; `flipbook.go` the frame-list sequence; `resources.go` the alias for
the one resource; `timelines.go` its
implementation; `timeline.go` the `Timeline` type and its generic timeline
methods; `plugin.go` the plugin and its tick subscription.

## Dependencies

- Go packages: `github.com/dvoyni/cog/app`, `github.com/dvoyni/cog/kernel`,
    `github.com/dvoyni/cog/m`
- Plugin dependencies: none
- Configuration: none

## Plugin

```go
anim.New()
```

`Register` registers the `*Timelines` resource and subscribes
`UpdateEventHandler` to `app.UpdateEvent` in the First phase. Every tick it
advances each timeline by `Dt`, promotes the cues that came due into their
fired view, and drops finished tracks.

Handlers that read or write timelines run in the ordinary phase, so they see
the current tick's values and fired cues. A First-phase handler that needs them
must order itself `After[anim.UpdateEventHandler]()`.

## Resources

### `Timelines`

The package's one resource. It holds every timeline in the engine, keyed by an
arbitrary comparable value. Use a private marker struct per owner so keys
cannot collide across packages.

```go
var timelines kernel.Write[*anim.Timelines]   // in the handler's Lock

tl := timelines.Get().Get(boardKey{})  // create on demand (write lock)
tl := timelines.Get().Lookup(boardKey{})  // nil when absent (read lock is enough)
timelines.Get().Reset(boardKey{})   // clear in place; pointers stay valid
timelines.Get().Delete(boardKey{})  // detach; pointers keep working but stop advancing
```

A `*Timeline` is valid only for the handler pass that took it. A scene object
that spans many helpers keeps it in a field set on the way in and cleared on
the way out:

```go
state.Anims = timelines.Get().Get(boardKey{})
defer func() { state.Anims = nil }()
```

A nil `*Timeline` is the no-op timeline: adds and cues are dropped, queries
report nothing, `Idle` is true. Pass it where moves are replayed only for their
model mutations, such as an AI search.

## Timeline

`Timeline` is a plain type, not a resource: nothing locks one directly, and
handlers reach a timeline through the `Timelines` resource above.

A timeline is a clock with a chain point. Tracks are queued under a slot
identified by the sequence type and an id, so different sequence types may
share ids. Tracks under one slot append and play back to back.

```go
tl.Add(unitId, MoveSeq{Lerp: anim.LerpFloat(0, 1), From: a, To: b},
	anim.Over(0.4).WithEasing(anim.EaseCubicOut))

seq, progress, state := tl.Query[MoveSeq](unitId)  // state: NotFound, Pending, Active
alpha := tl.Value[FadeSeq](unitId, 1)               // fallback when nothing is queued
```

- A track starts at the chain point, or now with `Immediate`; unless it loops
  the chain point moves to its end, so successive adds narrate in order.
- `Rewind(-d)` pulls the chain point back to overlap the next track with the
  previous ones; rewinding a full duration starts them together.
- `Wait(d)` leaves a gap in the chain.
- `Query` returns the active track (the newest when several overlap), else the
  earliest pending one. Finished tracks are dropped on the next advance.
- `Loop` repeats forever with the duration as period; looping tracks are never
  dropped and do not count toward `Idle`.
- `Idle` reports that no one-shot track is active or pending, no cue is
  waiting, and the chain point has passed.
- `Reset` clears the timeline in place.

### Cues

A cue is a value queued at the chain point that the timeline hands back once
the clock reaches it, in place of a callback (a handler cannot run game code
under the anim plugin's lock).

```go
tl.Cue(resolveAttack{unit: id})
tl.Wait(0.5)
tl.Cue(endTurn{})

// In the owner's ordinary-phase handler, every tick, before any early return:
for cue := range tl.Fired[resolveAttack]() {
	resolve(cue.unit)
}
```

A cue never fires on the tick that queued it. It fires on the first advance
whose time reaches its position and is visible through `Fired` (typed) or
`FiredCues` (untyped) for exactly that tick; the next advance clears the view
whether or not anything read it. Cues fired together keep queue order. A
pending cue keeps the timeline non-idle; a fired one does not.

## Sequences And Easing

`Sequence[T]` is any type with `At(progress float32) T`. `Lerp[T]` mixes two
values with a `Mix` function; `LerpFloat`, `LerpAngle`, `LerpVec2`,
`LerpVec3`, and `LerpColor` wire the `m` package's mixes. Embed `Lerp` in a
named struct to give a track its own slot type and to carry payload for the
reader.

`Params` is declarative: `anim.Over(d)` with `WithEasing`, `WithLoop`,
`WithImmediate`, or a literal `anim.Params{Duration: d, Easing: e, Loop: true}`.

`Flipbook[T]` is a sequence of frames rather than a mix: it holds each frame
of `Frames` for an equal slice of the track, so the track's value is the frame
to draw. `FPS` is the rate the frames were authored at, and `Params()` returns
the track that plays them all once at it.

```go
type FlagWaveSeq struct{ anim.Flipbook[Sprite] }

book := anim.Flipbook[Sprite]{Frames: flagFrames, FPS: 30}
tl.Add(flagKey{}, FlagWaveSeq{book}, book.Params().WithLoop().WithImmediate())

frame := tl.Value[FlagWaveSeq](flagKey{}, flagFrames[0])
```

Easings: `Linear`, `EaseCubicIn`, `EaseCubicOut`, `EaseCubicInOut`, and the
combinators `Hold(fraction, easing)` (stay at 0, then ease over the rest) and
`Reverse(easing)`.
