# cog ecsaudio — specification

`github.com/dvoyni/cog/bundles/ecsaudio` is a Bundle that records Entities into
[`sound`](../../../../slots/sound/docs/specs/sound.md). An Entity declares what
it sounds like and where it is; the binding turns that into plays, parameter
updates and stops on `sound`'s queue, and a Voice attached to an Entity dies with
it.

**It is a binding and nothing else.** No commands, no state a game addresses, no
arithmetic. Two Components and one recording System, reading the Entity's
`m.Transform`, which is exactly what the
`ecs` prefix means in this repo — `ecsscene`, `ecsphysics2d`.

The design is bound by three requirements, in this order:

1. **The same queue, not a second machine.** `ecsaudio` records into `sound`'s
   `Queue`, which is where a game without ECS records too. Neither face
   reimplements the other, and a game may use both at once.
2. **No ECS System pays for it.** The binding takes no structural lock and
   writes no Component, and its Components are split so that the System copying
   transforms every tick never blocks the one changing a Clip.
3. **A despawn must not leave a sound behind.** A Reference to a despawned Entity
   resolves to nothing, so a Voice can never reach back; whatever the binding
   does about a despawn, it does from its own side.

Two properties fall out of taking those in that order. **The Components are the
intent and the binding owns the correspondence** — through a plugin-owned
`Entity → Voice` table that is deliberately *not* a Component. And **an entry
outlives its Voice**, which is the single rule that stops every one-shot in the
game restarting forever.

This document is assembled from
[The ECS binding: what an Entity declares, and what a despawn does to a voice](https://github.com/dvoyni/cog/issues/303),
with the authoring rule from
[where a 2D game's listener stands](https://github.com/dvoyni/cog/issues/470) and
the surface it drives from
[sound.md](../../../../slots/sound/docs/specs/sound.md); every section cites the
tickets it came from. Where a claim rests on something unverified it is marked
**Gap**; where assembling these decisions next to each other settled something no
ticket did, it is marked **Settled here**.

**Nothing of this is implemented**, and neither is `sound`.

**The package was called `bundles/audio` until
[#303](https://github.com/dvoyni/cog/issues/303).** `audio` would read as a peer
of `scene` and `canvas`, which are not bindings.

---

## Contents

- [Vocabulary](#vocabulary) · [The three declarations](#the-three-declarations)
- [The Listener](#the-listener) · [Direction of truth](#direction-of-truth)
- [Despawn](#despawn) · [The System and its lock set](#the-system-and-its-lock-set)
- [Authoring a 2D game](#authoring-a-2d-game)
- [Testing](#testing) · [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

**No new term.** **Voice**, **Clip**, **Listener** and **Positional Voice**
already carry everything here, and the binding is machinery rather than a concept
a game meets. `CONTEXT.md` gains nothing, deliberately.

The one word worth guarding is **Emitter**: it is the Component, not a concept
beside Voice. An Entity carrying one is an Entity that makes a sound; the sound
it makes is a Voice like any other.

---

## The three declarations

```go
// Emitter is what this Entity sounds like. Rarely written.
type Emitter struct {
	Clip   sound.ClipRef
	Params sound.Params
}

// Listener marks the one Entity the world is heard from.
type Listener struct{}
```

Where an Entity is heard from is its `m.Transform`, whose Store is the ecs
plugin's. ecsaudio declares no Transform of its own, and `Scale` is ignored.

### Why `Emitter` and `m.Transform` are separate Components

Not tidiness. `bundles/ecsphysics2d/components.go:25-30` gives the reason in its own
words — `Force` is its own Component *"so that a System adding Force does not
block the render copy reading Position"*. Here it is a System copying transforms
every tick against a System that changes a Clip once an hour. One Component would
serialise them.

### Where an Entity is heard from is `m.Transform`

ecsaudio registers no Transform. It reads the ecs plugin's `Store[m.Transform]`,
the same Store ecsscene draws from, so there is one placement in the engine
rather than one per consumer
([#468](https://github.com/dvoyni/cog/issues/468)). The cost of that shared
Store is the one stated in the ecs README's
[Component registration](../../../ecs/docs/README.md#component-registration):
the coupling check never catches an undeclared writer of it, so its writers are
ordered deliberately.

It is never reached through `scene`, which would make every game with sound
depend on the renderer. `libs/m` holds `Transform`, with `Forward()`, `Right()`
and `Up()`, for exactly this.

**The axes need no conversion.** `sound` faces −Z with +Y up, as the ECS
spotlight does, so a Transform the game already keeps for rendering is read
straight across. `Scale` is ignored.

### An `Emitter` with no `Transform`

A **non-positional Voice** — background music, a UI click — which is what *heard
from nowhere in particular* already describes.

Two consequences of `sound`'s one-way positional rule, worth stating because
neither is guessable:

- **Adding a `Transform` to an Entity whose Voice is already playing does not
  make that Voice positional.** It takes effect on the next `Play`.
- **Removing a `Transform` does not make a positional Voice non-positional
  either.** It simply stops moving.

### An Emitter's `Priority`

`Emitter.Params` is `sound.Params` whole, so an Emitter already carries
**`Priority`: the band [sound.md §Stealing](../../../../slots/sound/docs/specs/sound.md#stealing)
defines, and it reaches the Voice unchanged.** The binding neither drops it nor
supplies one of its own; an absent `Priority` is the default 0, exactly as it is
to a `Play` on the queue.

It is how an Entity's sound is kept playing under the cap. **Music one band up
is never stolen by effects at the default 0**, however many of them arrive:

```go
world.Spawn(ecsaudio.Emitter{
	Clip:   sound.ClipWithResource("music/level1.ogg"),
	Params: sound.Params{Loop: m.Some(true), Priority: m.Some(1)},
})
```

Nothing is added here to say it — no field, no flag — and a binding test holds
it ([Priority on emitters](https://github.com/dvoyni/cog/issues/502),
[#504](https://github.com/dvoyni/cog/issues/504)).

---

## The Listener

The binding writes `SetListener` from the `Transform` of the Entity carrying the
`Listener` Tag. Both degenerate cases are named rather than left to discovery:

- **None** — the binding writes **nothing**, leaving the Listener where it was.
  Resetting to the origin would swing every positional sound in the world the
  instant a listener Entity is despawned mid-level, which is the one moment a
  game can least afford it.
- **Two or more** — `ReportErrorOnce` with `ecsaudio.ErrManyListeners`, and take
  the **lowest Entity**. Deterministic, and the same every tick: an arbitrary
  pick that changed with iteration order would be a sound bug nobody could
  reproduce.

**A `Listener` on an Entity with no `Transform` is ignored, and counts as none.**

---

## Direction of truth

`ecsscene` can duck this question — *"scene keeps no per-entity state, so there
is no scene-side object for an Entity to be a copy of, and the Components are the
source of truth because there is no other candidate"*
(`bundles/ecsscene/doc.go:15-18`). **`sound` retains Voices**, so there is a
second candidate and the question is real, which is why
[The binding shape](https://github.com/dvoyni/cog/issues/246) handed it to the
bound plugin.

> **The Components are the intent; the binding owns the correspondence.**

The binding keeps a plugin-owned **`Entity → Voice` table** — *not* a Component —
and reconciles once a tick:

| state | action |
|---|---|
| `Emitter` present, no entry | `Play`, record the Voice |
| entry present | `SetVoice` with the params; absent `Maybe` fields mean unchanged, so an unchanged Emitter costs nothing |
| `Clip` changed | `Stop`, then `Play`. Detected by `ClipRef.Equal`, since a `ClipRef` is not comparable |
| entry present, `Emitter` gone | `Stop`, drop the entry |

### The trap this avoids

If the rule were *an `Emitter` with no live Voice gets a `Play`*, **every
one-shot in the game would restart forever**, because a finished Voice is exactly
a Voice that is no longer live. So:

> **An entry outlives its Voice.** A one-shot that finished leaves an entry
> holding a dead handle, and the binding does not re-play. Re-triggering the same
> Clip on the same Entity is remove-then-re-add, or the queue face.

A looping sound needs **no separate concept**: it is an `Emitter` whose `Params`
loop, and the same table entry tracks it. Since
[#471](https://github.com/dvoyni/cog/issues/471) that covers intro-into-loop
music too, for free — the loop point is a fact of the Clip, so an `Emitter`
naming a tagged Ogg loops the way that file says to with nothing added here.

### Stealing is invisible here

`sound` can end a Voice under the cap with nothing having despawned, so a table
entry's handle can go stale on its own. The binding treats that **exactly as it
treats a finished one-shot**: the entry stays, the handle is dead, nothing
restarts. A game that wanted a stolen ambience back re-adds the Component, which
restarts it from the head; one that wants it back in step records a `Play` on
the queue by the retry recipe in
[sound.md §Stealing](../../../../slots/sound/docs/specs/sound.md#stealing). A
steal is final, and an `Emitter` gains no start offset for it
([#503](https://github.com/dvoyni/cog/issues/503) §4).

This is why the binding cannot assume its handle is live, and why a stale handle
addressing nothing is load-bearing rather than merely tidy: every operation on
one is a no-op, so the reconcile needs no guard and no query.

---

## Despawn

**The Voice dies with the Entity.**

A despawned Entity stops matching the query, so it is the same row as the last
one above — **`Stop`, drop the entry**. No `ecs` Hook, no `Reference` to resolve,
and no reach-back from the Voice.

The scan that finds it is bounded by the table, which is bounded by `MaxVoices`
(64 by default), so it is a **fixed cost per tick** rather than a cost per
Entity.

### The explosion case

A sound on an Entity destroyed by its own explosion is served by **the other
face**: the game records a `Play` into `sound`'s queue, where the Voice was never
attached to anything and nothing can despawn it.

So the choice *does the Voice die, finish detached, or is it the game's choice?*
is answered **the game's choice, expressed by which face it used** — never by a
flag.

---

## The System and its lock set

**One System**, ordered as `RecordOnUpdate`, matching `ecsscene`'s single
recording System.

**Its lock set** is `Write[*sound.Queue]`, plus a read query over `Emitter`,
optional `Transform`, and the `Listener` Tag.

**It writes no Component.** That is the whole point of the table being
plugin-owned: no structural change, no `WriteableEntities`, and therefore nothing
for it to serialise against. A binding that wrote a `Playing` Component would take
a structural lock every time a sound started, which on a frame that spawns a
hundred emitters is a hundred structural changes for bookkeeping the binding
could have kept to itself.

**It reads no `sound` resource other than the queue.** The live Voice view exists
and the binding could reconcile against it, but it does not need to: an entry
outliving its Voice is the whole policy, and a dead handle is indistinguishable
from a live one as far as every operation is concerned. **Settled here** — the
table alone is sufficient, and reading `*sound.Voices` would widen the lock set
for nothing.

---

## Authoring a 2D game

From [#470](https://github.com/dvoyni/cog/issues/470).

> **In a 2D game the `Listener` Tag does not go on the player sprite.**

A 2D sprite's rotation is a rotation about `Z`, which **fixes the Z axis** and so
leaves the Listener's forward at `(0,0,−1)` at every angle — it never turns.
Measured: at `rotZ(30°)` two sources both to the player's right land at +90° and
−90°, **opposite ears**; at `rotZ(90°)` a source 20 px to the right reads −90°,
hard left.

It goes on **an Entity that exists to be heard from**, carrying a `Transform`
whose `Position` follows the player and whose `Rotation` is the fixed
`m.QuatRotationX(-math.Pi/2)` that
[sound.md](../../../../slots/sound/docs/specs/sound.md) specifies for 2D.

```go
world.Spawn(
	ecsaudio.Listener{},
	m.Transform{
		Position: playerPos,
		Rotation: m.QuatRotationX(-math.Pi / 2),
	},
)
```

**Nothing 2D-specific is added to this package.** No mode, no field, no flag —
the rotation is an ordinary `m` value in an ordinary `Transform`, and the binding
copies it across as it copies any other.

**A directional 2D emitter takes the same quaternion, turned in-plane**:
`rotZ(θ).Mul(base)`. So a 2D author learns one rotation and spends it on the
Listener and on every `Emitter` with a `Cone`.

**Positions pass through unchanged**: `m.Vec3{X: x, Y: y}`, `Z` left at zero.

---

## Testing

**The binding is testable without a Device and without a Component write**:
spawn, reconcile, read the queue. Compose `nosound`, drive ticks, and assert
against `sound`'s live Voice view and `VoiceEndedEvent` exactly as a direct-face
test does — the assertions are the same because the machine is the same.

What is worth asserting *here* rather than in `sound`'s own tests is the
correspondence, which is this package's whole job:

- an `Emitter` added produces exactly one `Play`, and an unchanged `Emitter` on
  the next tick produces no second one;
- a **finished one-shot does not restart** on any subsequent tick — the single
  most important test in this package, because the failure is a sound that
  repeats forever and the bug is one line;
- a changed `Clip` produces `Stop` then `Play`, and an unchanged one does not;
- a despawn ends the Voice with `ReasonStopped`;
- a **stolen** Voice does not restart, which is the same rule reached by a
  different route;
- an `Emitter` one `Priority` band up keeps its Voice while effects a band
  below fill and overflow the cap — it stays in the live view and no
  `VoiceEndedEvent` names it, so a binding that dropped or overrode the field
  fails;
- two `Listener` Tags report once and take the lowest Entity, on every tick and
  not just the first;
- no `Listener` leaves the Listener where it was.

Test idiom is the repo's and is not this spec's: same-package `*_test.go`,
full-sentence test names, and **no race detector in this environment** — use
`-count=10` and say so.

---

## What is not foreclosed

- **A `Playing` Component**, if a game ever needs to read an Entity's Voice from
  its own System. Additive, and it would cost the structural lock the System
  avoids.
- **Re-triggering without remove-and-re-add**, which a counter field on `Emitter`
  would give and which nothing has asked for.
- **Exposing the `Entity → Voice` table to the agent capability.** An agent
  asking *what is this Entity playing* is answerable by joining the table against
  `sound`'s live Voice view, but the table is plugin-owned, so if that is ever
  wanted it needs a way in — and it should be decided in
  [mcp.md](../../../../slots/sound/docs/specs/mcp.md) rather than by exposing the
  table by default.
- **A second recording System**, if the transform copy and the emitter
  reconcile ever turn out to want different orderings. The Components are
  already split for it.

---

## Shapes that were rejected

- **A `DetachOnDespawn bool` on `Emitter`.** It puts a second mechanism beside
  the queue face for something the queue face already does perfectly, and it
  makes *what happens on despawn* a property a game has to read a Component to
  know.
- **An `ecs` Hook on Component removal.** The reconcile already sees every
  departure, and a Hook runs during structural change where the `Queue` lock is
  not held.
- **The `Entity → Voice` correspondence as a Component.** It would take a
  structural lock per sound started, for bookkeeping nobody outside the binding
  reads.
- **One `Emitter` Component carrying the transform.** It serialises the System
  copying positions every tick against the System changing a Clip once an hour.
- **Re-playing an `Emitter` whose Voice is not live.** Every one-shot restarts
  forever.
- **Resetting the Listener to the origin when no Entity carries the Tag.** It
  swings every positional sound at the worst possible moment.
- **Picking an arbitrary Listener when two Entities carry the Tag.** A sound bug
  nobody can reproduce.
- **`ecsaudio` owning a play command.** The engine is ECS-first and the direct
  face is `sound`'s queue; a command here would be a third way to start a sound.
- **The name `bundles/audio`.** It reads as a peer of `scene` and `canvas`, which
  are not bindings.

---

## Required work

Nothing below exists.

1. **`bundles/ecsaudio`** root: `doc.go`, `id.go`, `types.go` (`Emitter`,
   `Listener`, and the `RecordOnUpdate` ordering identity),
   `err.go` (`ErrManyListeners`). No `commands.go`, no `resources.go`, no
   `ports.go`, no `adapters.go` — a binding declares none of those.
2. **`bundles/ecsaudio/internal`** — the Component registrations, the
   plugin-owned `Entity → Voice` table, and the one recording System. The
   Components are plain data with no methods, so there is **no
   `internal/types`**, following `ecsscene`.
3. **`ecsaudioplugin`** exporting only `New()`, with zero type parameters and
   zero parameters.
4. **`ecsaudio` joins `archtest`'s whole-cog composition**
   (`kernel/archtest/composition_test.go:38-42`) beside `ecsscene`, which also
   requires `sound` and an Adapter in that composition.

**Blocked on `sound`**, and specifically on two things it owes:

- **`sound.ClipRef.Equal`**, which the reconcile needs because a `ClipRef` may
  hold a Blob and so is not comparable.
- **`sound.Params` staying a legal Component value** — `m.Maybe` fields, no
  pointers, comparable when its fields are.

**Already done, and not waiting on anything:** `libs/m/transform.go` exists with
`Transform`, `Forward()`, `Right()` and `Up()`. `m.Transform` is the one
placement type, registered by the ecs plugin
([#468](https://github.com/dvoyni/cog/issues/468)).

---

## Out of scope

- **Anything `sound` owns.** The arithmetic, the cap, stealing, Clips, the
  Device, the seam. This package computes nothing and stores nothing but the
  correspondence.
- **A Component for the Device or the live view.** Both are `sound` resources a
  game's own System reads directly; mirroring them into Components would be a
  second copy that can disagree.
- **Driving the Listener from a camera automatically.** `sound` never reads a
  camera, and neither does this: a game that wants the Listener on its camera
  puts the Tag on the camera's Entity, or copies the Transform itself.
- **More than one Listener**, which is `sound`'s own out-of-scope entry and would
  be an additive field there before it was anything here.
