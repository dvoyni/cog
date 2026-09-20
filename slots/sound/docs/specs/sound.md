# cog sound — specification

`github.com/dvoyni/cog/slots/sound` is a Slot that gives an app audio: Clips
played as Voices, grouped on Buses, positioned in the world and heard from one
Listener, with all of the arithmetic computed here and none of it below. Its
Adapters are `otosound` (a software mixer over `ebitengine/oto/v3`), `jssound`
(Web Audio) and `nosound` (plays nothing). This document specifies the whole of
the Slot and the seam beneath it.

**It is the machine, not a convenience layer.** `bundles/ecsaudio` drives the
same queue from ECS and reimplements nothing; a game without ECS records into
the queue directly, as a `canvas` game records into `gfx`.

The design is bound by four requirements, in this order, and every decision
below was taken against them:

1. **One machine, two faces.** A game records into `sound`'s queue directly
   (`Play`, `Seek`, `Stop`, a bus volume), and `ecsaudio`'s Components drive
   *that same queue*. Neither face is a reimplementation of the other.
2. **Nothing a game must load or release.** There is no `LoadSound` and no
   handle to a Clip. A caller names a path or a Blob; loading and caching live
   inside `sound`. Releasing is optional, for a game that wants the memory back
   sooner.
3. **2D and 3D.** Per-voice position and one Listener, with distance
   attenuation, panning and cones. The same model serves a `canvas` game and a
   `scene` game.
4. **Desktop first, then WebAssembly, then everything else.** Every Adapter cog
   ships in v1 is cgo-free, which keeps cross-compilation and the web build
   free. A cgo Adapter is allowed only as an opt-in Extension a game composes
   itself.

Three properties fall out of taking those in that order, and most of what
follows is a consequence of one of them. **The queue speaks in position and
Listener, never in gain and pan** — which is what keeps a later HRTF effort from
redrawing what a game says, and what makes the arithmetic one implementation
rather than one per Adapter. **A Voice is retained, not re-declared** — a music
track seeked partway through is not derivable from a frame's declarations, which
is why `sound` holds a table and `gfx` holds none. And **the Adapter is a dumb
sink**: it is handed a gain matrix and a rate, and knows nothing of handles,
buses, priorities, positions, the cap or the equations.

This document is the specification the implementation is judged against. It is
assembled from the eighteen resolved tickets of
[audio: a plugin whose sounds are commanded, positioned, and mixed in pure Go](https://github.com/dvoyni/cog/issues/294)
and from [sound on assetcache: the clip surface in K, D and T](https://github.com/dvoyni/cog/issues/412);
every section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it; where assembling
these decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**Nothing of this is implemented.** cog has no audio today — not a stub, nothing
in the tree; `slots/` holds `app`, `gfx` and `storage` alone. So
[Required work](#required-work) is the whole build, not a list of what is left.

**One thing here is younger than the map and reverses a premise several
resolutions were written on.** [#412](https://github.com/dvoyni/cog/issues/412)
expressed the clip surface in an `assetcache` whose `Get` was asynchronous and
whose entries carried generations. What shipped on 2026-09-18 is
[`libs/assets`](../../../../libs/assets/docs/specs/assets.md), a Library whose
`Load` is **synchronous**, which has four verbs and no probe, in which nothing
is ever pending — and whose own Out of scope says *"`sound`. The Slot does not
exist. This design is what it will use; nothing here is shaped by it."*
[Clips](#clips) is written against the Library that exists, and says exactly
which of [#412](https://github.com/dvoyni/cog/issues/412)'s words survive as
`sound`'s own and which were the cache's and are gone.

---

## Contents

- [Vocabulary](#vocabulary) · [The shape of the package](#the-shape-of-the-package)
- [The queue](#the-queue) · [What a game says](#what-a-game-says) · [What is never said](#what-is-never-said)
- [The Voice](#the-voice) · [Positional audio](#positional-audio) · [Clips](#clips)
- [The Device](#the-device) · [The seam](#the-seam) · [What an Adapter owes](#what-an-adapter-owes)
- [Testing](#testing)
- [What is not foreclosed](#what-is-not-foreclosed) · [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

Every term here is in `CONTEXT.md` under **Audio**, which is the glossary of
record; this list is a reading aid, not a second definition.

- **Clip** — a sound a Voice plays, named by a path or by a Blob of encoded Ogg
  and kept until it is released.
- **Prepared Clip** — a Clip turned into something a Voice can be started from.
  Which kind it is — shared samples, or bytes and a way to read them — is the
  Adapter's own business and a game cannot tell.
- **Loop Region** — the span of a Clip a looping Voice repeats between, declared
  by the Clip and never by the game.
- **Voice** — one playing instance of a Clip, begun by a play and addressed
  afterwards by what that play handed back.
- **Positional Voice** — a Voice that has been given a position, and so is
  quieter with distance and heard from its side.
- **Falloff** / **Cone** — how a Positional Voice quietens with distance, and
  the directions it is loud in. Each Voice carries its own.
- **Listener** — the place and facing a Positional Voice is heard from. One per
  Engine.
- **Bus** — a group of Voices the game declares, sharing one volume. Flat, all
  directly under **Master**.
- **Stealing** — what ends a Voice to make room when every slot is taken.
- **Device** — what makes sound audible. A game neither opens nor chooses one;
  it only reads whether there is one.
- **Mixer** — what turns Voices into the samples a Device consumes, outside the
  Engine on the Device's own clock, reached by nothing that takes a lock.
- **Fade** — a change of volume over time a game drives itself. `sound` has no
  verb that performs one, and the term is in the glossary so that nobody gives
  it one.

**Two words are deliberately unspent.** *Snapshot* does not stretch to a listing
of retained Voices — a Snapshot is one tick's recorded declarations, and a Voice
was recorded once, possibly minutes ago
([the agent-facing capability](https://github.com/dvoyni/cog/issues/305) §3).
And *VoiceSlot*, below, is machinery rather than a concept a game meets, which
is the point of it. **Backend**, **audio thread**, **callback**, **PCM**,
**sample**, **frame**, **channel** and **pan** are on the glossary's avoid
lists.

---

## The shape of the package

From [sound: a Slot beneath audio, the way gfx sits beneath canvas](https://github.com/dvoyni/cog/issues/373),
laid out as [Plugin structure: slots, extensions and bundles as declaration roots](https://github.com/dvoyni/cog/issues/351)
requires.

| Package | Kind | Holds |
|---|---|---|
| `slots/sound` | Slot | The queue, Voices and their handles, Buses, the cap, the Listener, **all of the arithmetic**, Clips and their cache, `VoiceEndedEvent`, `Device`, and the `Backend` interface with `BackendPort`. |
| `extensions/otosound` | Extension | Our own mixer over `oto`. Decodes, resamples, applies the gain matrix and the rate, and runs the device thread. Excludes `js`. |
| `extensions/jssound` | Extension | Web Audio nodes; the browser decodes and mixes on its own audio thread. `js` only. |
| `extensions/nosound` | Extension | Accepts everything and plays nothing, for headless engines, tests and CI. |
| `bundles/ecsaudio` | Bundle | Components and one recording System. Imports `ecs`; has no commands. Specified in [ecsaudio.md](../../../../bundles/ecsaudio/docs/specs/ecsaudio.md). |

**A game composes one Adapter per platform**, in build-tagged files, exactly as
feuds already does for `diskstorage` and `jsstorage`. A web build that composes
`otosound` **fails to compile** rather than quietly shipping a mixer that
starves. There is no knob selecting a backend: which one runs is the target
triple's answer, and a knob would offer a choice that does not exist
([the web backend](https://github.com/dvoyni/cog/issues/300) §3).

**There is no vocabulary package.** [#351](https://github.com/dvoyni/cog/issues/351)
folded `gpu` back into `gfx`'s root, and `sound` follows: `Backend` and
`BackendPort` live in the root's `ports.go`, beside `gfx`'s.

Four resources, and they are four rather than one so that a System asking a
question never contends with the Systems recording operations:

| Resource | Lock | What it is |
|---|---|---|
| `*sound.Queue` | `Write` | Where every operation is recorded. |
| `*sound.Clips` | `Read` | What `ClipInfo` asks. |
| `*sound.Voices` | `Read` | The live Voice view. |
| `*sound.Device` | `Read` | What the Device is now. |

`storage.FileSystem` is the closest precedent for all three read-only ones
(`slots/storage/resources.go:5-10`): *a read-only live view that exposes no
mutators at all.*

---

## The queue

One `Queue` resource, written under its lock by every recorder and **flushed
once per tick, atomically** ([#373](https://github.com/dvoyni/cog/issues/373)
item 5). It is deliberately **not** `gfx`'s latest-wins triple buffer: a frame
is a complete description and may be replaced, an operation is a delta and may
not.

- **Never lost, applied in order:** `Play`, `Seek`, `Stop`, `StopBus`,
  `Preload`, `Release`, `ReleaseAll`.
- **Coalesced within a tick, last value wins:** a Voice's `Params`, a Bus
  volume, the Listener.
- **A handle is minted when the play is *recorded***, so it is usable in the
  same tick that recorded it. There is no command to wait on.

### The flush, in order

**Settled here**, because [#412](https://github.com/dvoyni/cog/issues/412)'s
order was written for a cache that deferred its releases and the Library does
not defer anything.

1. **Drain `TakePrepared()`** into `sound`'s clip table: install each completion
   whose entry is still live at the epoch its token names, and drop the rest.
2. **Apply the tick's operations**, in the order they were recorded. A `Play` or
   `Preload` naming an unknown Clip reads its bytes and calls `Prepare` here; a
   `Release` stops the Voices on that Clip and queues its `Destroy`.
3. **Compute**: advance every playhead, run the equations, and fold each Bus
   volume into each Voice's gain matrix. Stealing is *applied* here and not
   resolved here — the victim was chosen when the play was recorded, because
   that is where the handle that names its slot was minted. See
   [Stealing](#stealing).
4. **`Emit(batch)`** once.

Draining first is what makes a release recorded against an in-flight `Prepare`
cost nothing: by the time the completion arrives its entry is gone, so no
`ClipID` is ever minted only to be destroyed. That is
[#412](https://github.com/dvoyni/cog/issues/412)'s own hazard, reached from the
other side now that releases are immediate.

---

## What a game says

From [The command surface: what gameplay says to make a sound](https://github.com/dvoyni/cog/issues/296),
with [Positional audio](https://github.com/dvoyni/cog/issues/297)'s fields,
[The voice](https://github.com/dvoyni/cog/issues/301)'s `Priority` and
[#412](https://github.com/dvoyni/cog/issues/412)'s `Preload`.

There are **three addressable things** — a Voice, a Bus, the Listener — and the
verbs fall out of them rather than being chosen.

```go
type Bus int
const Master Bus = 0 // the only Bus sound declares; a game declares the rest

// ClipRef names a Clip. It is not comparable: it may hold a Blob, which is a
// pointer and a length. Use Equal.
type ClipRef struct{ /* name, blob — opaque */ }

func ClipWithResource(path string) ClipRef     // read through storage
func ClipWithBytes(ogg assets.Blob) ClipRef    // encoded Ogg the caller holds
func (r ClipRef) Equal(other ClipRef) bool

// Params is "start like this" to Play and "become this" to SetVoice. An absent
// field is the default to Play and unchanged to SetVoice.
type Params struct {
	Bus         m.Maybe[Bus]
	Volume      m.Maybe[float32] // linear, 1 is unity
	Pitch       m.Maybe[float32] // playback rate, 1 is the clip's own
	Loop        m.Maybe[bool]
	Paused      m.Maybe[bool]
	Priority    m.Maybe[int]     // a hard band above audibility in stealing
	Position    m.Maybe[m.Vec3]
	Orientation m.Maybe[m.Quat]
	Falloff     m.Maybe[Falloff] // replaced whole
	Cone        m.Maybe[Cone]    // replaced whole
}

type ListenerParams struct {
	Position    m.Maybe[m.Vec3]
	Orientation m.Maybe[m.Quat]
}
```

**Ten operations**, recorded on the queue:

| addressed to | operation |
|---|---|
| a Voice | `Play(ClipRef, offset, Params) Voice` · `SetVoice(Voice, Params)` · `Seek(Voice, offset)` · `Stop(Voice)` |
| a Bus | `SetBus(Bus, volume)` · `StopBus(Bus)` — `Master` stops everything |
| the Listener | `SetListener(ListenerParams)` |
| a Clip | `Preload(ClipRef)` · `Release(ClipRef)` · `ReleaseAll()` |

and four questions, asked of the resources rather than the queue: a Bus's
volume, the Listener, `ClipInfo`, and the live Voice view.

**`Params` is one value of `m.Maybe` fields rather than a set of narrow
setters**, because the ECS face must reconcile a whole Component in one
operation and because `m.Maybe` is the engine's own spelling of a set-mask
(`libs/m/maybe.go:24-27`). It stays a legal Component value: comparable when its
fields are, pointer-free, copyable into a Store.

**`Clip` and `Offset` are not in `Params`.** Changing a Voice's Clip is a new
`Play`, a changed offset is a `Seek`, and a `ClipRef` is not comparable, which
it would otherwise drag into every `Params`.

**Offsets are seconds as `float32` on this face**, following
`anim.Params.Duration`. They cross [the seam](#the-seam) as a
`time.Duration`; the conversion is `sound`'s, and no Adapter ever sees a
`float32` of seconds. **Settled here**, reconciling
[#296](https://github.com/dvoyni/cog/issues/296)'s wording with
[#376](https://github.com/dvoyni/cog/issues/376)'s struct.

### Buses

`type Bus int`, declared by the game. `sound` predeclares **`Master = 0`
alone**, so an unset Bus lands on Master and a game that declares no Buses still
has working audio and a working master slider. Flat, no nesting — a tree is
where a DSP graph starts, and that is [out of scope](#out-of-scope).

**A fixed maximum of 32.** An out-of-range Bus plays on Master rather than
erroring, so a mis-declared constant does not silence a game. **A Bus volume is
readable**, so a settings screen draws its slider where the player left it.
**There is no `Mute`**: a game persists its settings anyway, and a shadow copy
inside `sound` invites *muted by whom*.

### Ordering and causality, and no figure anywhere

The contract promises **ordering and causality** and never a latency figure
([#300](https://github.com/dvoyni/cog/issues/300) §2):

- **A tick's operations apply atomically** — ordered by recording within the
  tick, audible together at the next block boundary, never partly applied. So a
  `Play` followed by a `SetVoice` carrying a position, in one tick, is
  indistinguishable from a `Play` that carried the position.
- A `Play` recorded in tick *N* is audible no later than one recorded in tick
  *N+1*.
- `Stop` silences without a tail.
- A position set in tick *N* governs everything mixed after it.

The numbers are **reported, never promised**: the Device's name, sample rate and
real output latency are readable, so a game that genuinely needs the figure — a
rhythm game — reads it and adapts or refuses.

### No fades, and no errors

**Every parameter change is declicked** by a short internal ramp with no knob: a
gain step on a running Voice clicks, and that is physics. The ramp is the
Adapter's, over its own blocks ([the seam](#the-seam) §4). A *musical* fade is
the game driving `SetVoice` or `SetBus` from an `anim` timeline in its own
handler, and a crossfade in v1 is two plays and a timeline the game writes —
stated as a deliberate non-feature so nobody later reads it as an omission.

**The surface is total.** Every operation is a **no-op on a Voice that no longer
exists**, with no error and no response field to check — a `Stop` racing a Clip
that finished a tick ago is the most common race in audio code. **No operation
returns an error at all.** The only failure `sound` reports is a Device that
could never be opened, and the only failure a *game* observes is a Clip that
ended its Voices with `ReasonFailed`.

### One event

**`VoiceEndedEvent{Voice, Reason}`, published for every ending**, so a
subscriber never has to tell *it ended* from *I ended it* to keep its own
bookkeeping straight. Dialogue sequencing and playlists land on it.

**There is no `VoiceStartedEvent`**
([how a game tests its sound](https://github.com/dvoyni/cog/issues/467) §2), and
the reason is what an event is for:

> An event exists to tell a game something it did not do.

Finishing, being stolen, failing, being released — none of those are the game's
doing. A start *is*: the game recorded the `Play` synchronously and already
knows.

---

## What is never said

No request type names, and no caller can state
([#296](https://github.com/dvoyni/cog/issues/296) item 12):

> sample rate · channel count · buffer or block size · device name or index ·
> backend name · file format or decoder · PCM, samples or frames · gain or pan
> (positions only) · thread, callback or mixer · a latency figure · a voice slot
> index · whether a Clip is streamed or resident.

**There is no `Update` or `Tick` verb**: audio runs on its own clock, and a game
that stops calling in keeps hearing sound.

**The rule governs a caller, and a file is not a caller.** A Clip's own Vorbis
comments carry its [Loop Region](#the-loop-region) in sample frames, read by the
Adapter from bytes it already parses and converted to seconds before they cross
the seam. No signature anywhere in `sound` names a sample. The rule delimits the
API, not the asset format
([where a music track's loop repeats from](https://github.com/dvoyni/cog/issues/471) §3).

---

## The Voice

From [The voice: its handle, its lifetime, and the cap](https://github.com/dvoyni/cog/issues/301).

### The handle

```go
// Voice is an opaque handle to one playing sound. It is comparable, copyable
// and usable as a map key, and the zero value means no Voice.
type Voice uint64

const NoVoice Voice = 0

func (v Voice) String() string // "Voice(7v2)" is index 7 at generation 2
```

`ecs.Entity`'s shape verbatim (`bundles/ecs/internal/types/entity.go:1-44`),
down to the reasoning: index in the low 32 bits, generation in the high 32,
**neither exported**, generations starting at 1 so `Voice(0)` unambiguously
means *no Voice* while index 0 stays an ordinary usable slot. A struct of two
`uint32`s is the same eight bytes and equally comparable, but it leaks its
layout into every call site and can never be re-cut.

**Staleness is one compare inside `sound` and is never surfaced.** A handle
whose generation does not match its slot's addresses nothing, which is exactly
the machinery *"every verb is a no-op on a Voice that is gone"* needs in order
to be implementable without an error path. A game holding a handle across a
steal, a stop and a recycle of that index never touches the Voice that took the
slot.

**Settled here:** because the table is fixed and generational, `sound` needs no
free list beyond the slots themselves and no map from handle to Voice. The
handle *is* the index.

### The cap

**`sound.Config.MaxVoices`, `0` meaning 64.** One number.

The tempting shape is two — a total, and a smaller one for streamed Voices,
since [Assets](https://github.com/dvoyni/cog/issues/298) measured a streamed
Voice at **137 KB plus a goroutine and a ring buffer** against an index for a
resident one. **That shape is foreclosed, not rejected:** the tier lives
entirely inside the Adapter and `sound` never learns it, so `sound` cannot count
what it cannot see, and reopening it would put *streamed or resident* back above
the seam.

So the spec states the worst case honestly instead of legislating against it:
**64 streamed Voices is about 8.7 MB and 64 goroutines.** The number a game
tunes is the one it can reason about — how many sounds it wants at once — and
the memory follows from what it plays.

### Stealing

When a play arrives at a full table, the loser is the Voice with the lowest
**(priority, audibility)**, **ties broken by age, oldest first**.

**Audibility is the final computed gain `volume × bus × falloff × cone`** — and
it is the scalar **before** panning, never anything read off the gain matrix.
That single number makes *quietest* and *furthest from the Listener* the same
policy rather than two competing ones, and it gives a non-positional Voice a
rank without a special rule.

> **Measured** ([the prototype](https://github.com/dvoyni/cog/issues/304)):
> taking audibility as `max(L, R)` off the matrix scores a centred Voice
> **0.1414** against a hard-panned one's **0.2000** at the same distance — 3 dB
> invented out of nothing, because equal-power panning preserves power by
> construction and so cannot make a Voice louder, only move it between the ears.
> Left as it was, stealing would discard the sound *in front of you* in favour
> of the one beside you.

**`Priority` is a hard band:** a lower-priority Voice always loses to a
higher-priority one whatever the gains say. Music at priority 1 is not stolen by
a crowd of footsteps at 0, however loud the crowd.

**The tie-break is not an implementation detail.** Two Voices tie on *both*
terms exactly whenever the same Clip is played twice at the same position in one
tick — a double footstep, two shell casings, a burst weapon. Left unstated, the
tie would fall to whatever order the table happened to iterate in, which makes
every stealing test flaky on a schedule nobody controls, rarely enough to be
blamed on something else for a long time
([#467](https://github.com/dvoyni/cog/issues/467) §6).

Two consequences stated rather than left implied:

- **A paused Voice ranks at the audibility it would have if it were not
  paused.** Pausing neither protects a Voice nor puts it first on the block;
  ranking it at zero would make *pause the music for a cutscene* a reliable way
  to lose the music.
- **A Voice waiting on its Clip, or on a Device that is not ready, occupies a
  slot**, because its clock is already running. A cold-started game hits the cap
  with nothing audible yet, and that is correct: those Voices are going to be
  audible.

**The incoming play can be the one that loses.** It still gets a real handle,
and its `VoiceEndedEvent` arrives in the same flush that recorded it. The
alternative — a play always steals something — means the hundredth footstep of a
bug silences the music, which is the failure the cap exists to prevent. The
game's code path is identical either way, because there is nowhere to put a
*your play was refused* case.

**Settled here:** three things this spec asserts cannot all hold at a full
table — the handle is minted when the play is **recorded**, the handle **is the
index**, and stealing resolves in the **compute** phase. The minter has no slot
to hand out at record time and the victim is not chosen until the flush.

**The third one bends.** The victim is chosen when the play is recorded, against
the table as it stood at the end of the last flush; compute only applies what
the handle already says. The other two are visible to a game — it holds the
handle, and every verb resolves through it — while *stealing resolves in
compute* is visible to nobody: what a game can observe is that the victim is in
the view until the flush that steals it, and that stays true either way.
Deferring the mint instead would bend what a game **can** see, because a play
that might lose would hand back a handle addressing nothing until the next tick,
`Play` would have two return shapes, and a `Stop` recorded beside it would
silently miss.

Two consequences, stated rather than left implied:

- **A play can only steal a Voice that was live at the end of the last flush.**
  It cannot steal a Voice started earlier in its own tick, so a hundred plays on
  one frame consume at most the slots that existed and the rest lose outright.
- **The ranking context is one moment.** Every candidate, the incoming play
  included, is ranked against the same Bus volumes and the same Listener — the
  ones every other question a recorder asks is already answered from. No two
  candidates are compared across a frame boundary.

**A recorder holds the queue's write lock and nothing else**, which is why the
Bus volumes and the Listener are *copied* to where the handle is minted at the
end of each flush rather than read there. A `Play` that had to lock the `Buses`
and the `Listener` to rank itself would widen every recorder's lock set, and
`sound`'s resources are several precisely so that they do not contend.

### Why a Voice ends

| `Reason` | |
|---|---|
| `ReasonFinished` | the Clip reached its end and was not looping |
| `ReasonStopped` | `Stop`, or `StopBus` covering it |
| `ReasonStolen` | the cap, whether it played for a second or never sounded |
| `ReasonFailed` | the Clip could not be read or prepared |
| `ReasonReleased` | the Clip it was playing was released |

- **`StopBus` does not get its own member.** It *is* `Stop` over a set; splitting
  it later is additive, and a reader switching on `ReasonStopped` keeps working.
- **`Release` does.** It is not a stop at all — stopping is its side effect, and
  it is typically called from a teardown System nowhere near the one that started
  the sound. Folding it into `ReasonStopped` would hide the one release-related
  bug behind the one cause that is always innocent
  ([what a released clip does](https://github.com/dvoyni/cog/issues/416) §2).
- **A looping Voice never ends by itself** and publishes nothing until one of the
  other four reaches it.
- **Shutdown publishes nothing.** An event nobody can receive is not an ending
  worth modelling, and N events at shutdown are N chances to touch a half-torn
  world.
- **The delays differ, and a game reading only the reason would assume they do
  not.** `ReasonStolen` from a full table arrives in the *same* flush as the
  play; `ReasonFailed` arrives **one or more ticks later**, never in the same
  tick, because preparing is asynchronous. A game cannot write *play it, and if
  it fails this frame, do X*.

### The live view, and the closure property

`sound` grants **one read-only view** of its live Voices — handle, Clip, Bus,
playhead, duration, current `Params`, paused, and for a Positional Voice its
audibility and its distance from the Listener — read the way `gfx.Viewport` is,
as `kernel.Read[*sound.Voices]`.

**It is a resource and not a command.** `gfx.FrameSnapshot` is a command because
a frame is **not retained**: there is nothing to read between ticks, which is
also why `canvas_draws` must step a paused engine to have anything to show.
Voices *are* retained. A command would be a second door onto a fact already
sitting still.

The reader can rely on this:

> **Every Voice that ever existed is observable exactly once: it is in the live
> view now, or its `VoiceEndedEvent` has fired.**

It holds in the hard cases. A play stolen in the flush that recorded it never
appears in the view, but fires `ReasonStolen` in that same flush. A Clip that
cannot be prepared fires `ReasonFailed` a tick or more later. **The cost, stated
rather than hidden:** a Voice stolen instantly leaves behind only Clip, reason
and tick — the parameters it was going to play with are gone, because the view
is where parameters live and it never appeared there. A test that must assert on
the parameters of a Voice that may be stolen asserts on them before the flush
that steals it.

**The playhead is tick-accurate, not sample-accurate.** `sound` computes it from
`Duration()` and the Voice's rate, the same way it computes endings, and the
Device is somewhere inside the current block when anyone reads it. `Seek` being
block-accurate is the same admission from the other side, which is why the two
sit in one paragraph: a reader meets the granularity once.

**The view carries audibility and distance; it does not carry the pan.** A
game's test asserts **what it commanded and what the engine derived from it** —
that the alarm is playing, on the right Bus, audible at 0.4 rather than 0. The
pan is engine arithmetic, pinned once by `sound`'s own package tests against
W3C's published numbers, not re-asserted by every game that makes a noise. A
game checking its sound is on the right side of the player checks *where it put
the source*, which is its own data.

---

## Positional audio

From [Positional audio: the listener, the units, and what 2D means beside 3D](https://github.com/dvoyni/cog/issues/297),
as amended by [where a 2D game's listener stands](https://github.com/dvoyni/cog/issues/470).

**One model serves both renderers.** A position is always an `m.Vec3`, a facing
always an `m.Quat`, and a 2D sound is a 3D sound lying on the `Z=0` plane.
`sound` has no `Vec2`, no 2D mode, no pan, and no notion of what a unit is.

### The axes, stated outright

**Right-handed, forward −Z, up +Y, right +X**, for the Listener and for every
Voice. This is what `scene.LookAt` and the ECS spotlight already do, and it is
the W3C Listener's default forward `(0,0,−1)` and up `(0,1,0)` — so an unrotated
Listener *is* the W3C default Listener, and a speaker and a spotlight on one
`Transform` point the same way. `scene` follows this only by habit, so **this
spec states it rather than inheriting it.**

### The arithmetic is W3C's, transcribed

The distance models (`linear`, `inverse`, `exponential`), the cone gain, and
`equalpower` constant-power panning from azimuth are the **W3C Web Audio API
specification's published equations, adopted verbatim and normatively**. The
reason is structural rather than aesthetic: a future `PannerNode` backend agrees
with our mixer **by construction** instead of by careful matching. The cost is
inheriting someone else's parameter names and two quirks — `maxDistance` only
clamps the linear model and never silences a Voice, and the linear formula is
odd.

**The implementation transcribes them from the W3C document itself**, not from a
ticket and not from this spec.

**Precision.** The spec promises **run-to-run determinism on one build** and
explicitly **not bit-equality across architectures**: `math.Acos` and `math.Cos`
carry no such guarantee, so a gain computed on `windows/amd64` may differ in the
last bits from the same gain on `darwin/arm64`. **Tests compare with a
tolerance**, and this sentence sits here beside the arithmetic rather than in a
testing appendix, because it is contract.

### Falloff and Cone are per Voice

```go
type Falloff struct {
	Model   DistanceModel // Inverse (default), Linear, Exponential
	Ref     float32       // W3C refDistance,   default 1
	Max     float32       // W3C maxDistance,   default 10000
	Rolloff float32       // W3C rolloffFactor, default 1
}

type Cone struct {
	Inner     float32 // degrees, W3C coneInnerAngle, default 360
	Outer     float32 // degrees, W3C coneOuterAngle, default 360
	OuterGain float32 // W3C coneOuterGain,           default 0
}
```

Each field's doc comment names the W3C parameter it corresponds to. **The
prefixes are dropped** — `Ref`, not `RefDistance` — because the struct already
says which distance; [#297](https://github.com/dvoyni/cog/issues/297) left that
naming to this spec. Both groups are comparable and pointer-free, so a Component
can carry one, and each is **replaced whole** by `SetVoice`: seven separate
`Maybe` fields would buy finer updates and cost a fourteen-field `Params`, for a
group nobody changes piecemeal.

**Nothing about spatialization is per Bus.** A game that wants every footstep to
fall off alike passes the same `Falloff` on each play. A node-graph backend would
otherwise have to copy Bus state onto every node, and a Bus would stop being *a
volume*.

**Units are the game's.** `sound` never learns a scale: no world-scale config
and no default-falloff verb. A game measured in pixels passes its own `Falloff`,
typically a constant it declares once.

> **The spec must tell authors to set `Ref` to their world's scale**, because
> W3C's default of 1 is a trap off metre scale: measured in
> [the prototype](https://github.com/dvoyni/cog/issues/304), a source at radius
> 4 is **−12 dB** and at radius 10 is **−20 dB**. Correct, and far quieter than
> an author expects.

### Positional is one-way

A Voice with no position is **non-positional**: no falloff, no cone, no panning,
heard centred. A UI click is one. **The first position a Voice receives, at
`Play` or by `SetVoice`, makes it positional for the rest of its life.** No
operation is silently ignored and no *clear* verb is needed; a Voice that should
stop being positional is a new `Play`. A `Falloff` or `Cone` given to a Voice
with no position is kept and takes effect when it gets one.

**A cone needs a facing.** A Positional Voice with no `Orientation` is equally
loud in every direction whatever its `Cone` says, so a Voice can never be
accidentally directional along an axis nobody chose. W3C's `(1,0,0)`
`PannerNode.orientation` default is **not** inherited.

### One Listener

Set by `SetListener` with a `ListenerParams` of `m.Maybe` fields where absent
means *unchanged*, readable back, and sitting at the origin with no rotation
before any call. **`sound` never reads a camera** — a game, or `ecsaudio`,
copies a camera's `Transform` across; it is one value copy, since `m.Transform`
carries `Position` and `Rotation` and `Scale` means nothing here.

### Stereo pans as W3C pans it

A stereo Clip keeps both channels and shifts weight between them; it is **not**
mixed down to mono first, because mixing down is a departure a `PannerNode`
backend would have to imitate by hand.

**Whether a positional Clip should be mono is the game's judgement, not the
engine's.** Both were judged fine by ear
([#304](https://github.com/dvoyni/cog/issues/304) item 6), so the spec carries a
**note** describing what stereo does when panned — W3C's stereo arm passes one
channel at unity and bleeds the other — and not a recommendation. `sound` pans
what it is given.

### 2D, precisely

A `canvas` game passes `m.Vec3{X: x, Y: y}` and **rotates its Listener**:

```go
sound.SetListener(sound.ListenerParams{
	Orientation: m.Some(m.QuatRotationX(-math.Pi / 2)),
})
```

That is forward `(0,−1,0)`, up `(0,0,−1)`, right `(1,0,0)`: with canvas's Y-down,
ahead is up the screen, up is out of the screen, right is screen-right.

**This is not optional, and leaving it out is not a degraded pan but a broken
one.** Panning projects the Listener's up axis away before taking a bearing, and
item 6 fixes up at `+Y`. The `Z=0` plane *contains* `+Y`, so projecting it away
leaves `X` and nothing else — every sprite with positive X is hard right and
every sprite with negative X is hard left, whatever its Y. Measured on an
800×600 canvas with the player at centre:

| source | unrotated Listener | rotated Listener |
|---|---|---|
| 20 px right, level | **+90.0°** | +90.0° |
| 200 px right, 200 px up-screen | **+90.0°** | +45.0° |
| 20 px right, 400 px up-screen | **+90.0°** | +2.86° |
| straight up-screen | 0.0° | 0.0° |

Distances are correct in every row either way, which is what makes the failure
so quiet: three pans exist, the arithmetic is right, and nothing reports
anything.

**Nothing 2D-specific is added.** No constant in `sound`, no field in
`ecsaudio`, no Listener in `canvas`, no mode — the value is an ordinary `m`
rotation handed to the `SetListener` that already exists.

**Screen Y is front-and-back, not elevation.** Under the rotation, elevation is
identically zero in a 2D game, and front and back fold to the same pan under
equal-power panning.

**The 2D cone is the same quaternion, turned in-plane.** `rotZ(θ).Mul(base)`
puts an emitter's forward at `(0,−1,0)`, `(1,0,0)`, `(0,1,0)` for θ = 0°, 90°,
180°, so a 2D author learns **one** rotation and spends it on the Listener and
on every directional emitter. This is the one-line recipe
[#297](https://github.com/dvoyni/cog/issues/297) item 7 promised, because nobody
writes it by hand correctly the first time.

**The degenerate case** is a source at the Listener's exact position: the
projection is zero-length, azimuth is **0**, and it is heard centred. The
implementation **must never normalize a zero vector**.

### What is accepted rather than fixed

Judged by ear and accepted as known properties of equal-power stereo rather than
defects ([#304](https://github.com/dvoyni/cog/issues/304)):

- **The fold behind.** A source passing behind pans identically to its mirror
  image in front, because two speakers cannot say *behind* and W3C folds rather
  than inventing a cue. This is why keeping [HRTF out of scope](#out-of-scope)
  is honest rather than a deferral.
- **The zenith snap.** Azimuth is 0 by definition where the horizontal
  projection vanishes.

---

## Clips

From [Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298)
and [#412](https://github.com/dvoyni/cog/issues/412), re-expressed against the
Library that shipped.

### Where each half lives

**Settled here.** [#412](https://github.com/dvoyni/cog/issues/412) expressed the
whole clip surface as one `assetcache` entry carrying a decoded value, with
tokens, generations, `Complete`, `ApplyReleases`, a `Peek` and an `ErrTryAgain`.
[`libs/assets`](../../../../libs/assets/docs/specs/assets.md) has none of those:
`Load` is synchronous, `T` is immutable, nothing is ever pending, `Get` is the
only read and it loads on a miss, and *"a plugin that wants asynchrony puts it
**in front of** `Get` and keeps its own not-yet-requested state."*

So the surface splits at exactly that line, and each half goes where it belongs:

| | what holds it | what it does |
|---|---|---|
| **encoded bytes** | `assets.Cache[clipParams, U, assets.Blob]` | the `storage` read, report-once, terminal failure, `Free` |
| **prepared clip** | `sound`'s own table, in front of `Get` | pending state, epochs, `Prepare` / `TakePrepared` / `Install`, `ClipID` |

**`sound`'s loader is the identity.** `Load` returns the bytes the Library
already read; `Default` returns the zero `Blob`, which `sound` reads as *the
read failed*; `Free` does nothing, because bytes are ordinary Go memory. Three
lines, and in exchange `sound` inherits the Library's read, its
`kernel.ReportErrorOnce` under the descriptor key, and its rule that **the entry
is the once** — a load runs one time per key until a `Free`, which is where
terminal failure, report-once and the absence of a retry all come from together.

**Nothing composes into the key.** `gfx` composes a colour space and `canvas`
composes a pixel size; a Clip has no variant dimension, because rate is a Voice
parameter applied at playback, stereo is never downmixed, and there is no bake.
`sound` is the one caller whose key *is* its source, so `clipParams` is the
empty struct. That holds only because **an Adapter may not bake the Device rate
into what it prepares** — resampling is at playback, so a Device change costs the
cache nothing.

**A Blob is a pointer and a length** (`libs/assets/blob.go:30`), so an Adapter
streaming from a Clip's encoded bytes references the same run the cache holds
and copies nothing. A `Release` drops the cache entry while the Mixer may still
be reading — and that is safe without any rule, because the bytes are Go memory
the Adapter still references and the garbage collector is what frees them.

**What [#412](https://github.com/dvoyni/cog/issues/412) says that is gone:**
`Peek`, `BlobKey`, `NewAccess`, `Complete`, `ApplyReleases`, `ErrTryAgain`, and
*a `Get` beats a queued `Release`* — the last of which existed only because that
cache deferred, and a Library that frees immediately cannot have the problem.
**What survives verbatim, as `sound`'s own:** the token carrying key and epoch,
the `done` flag, the rule that a prepared value must be garbage-collectable, the
poll rather than a callback, and `ClipInfo` as a question rather than a handle.

### Two tiers, and which is invisible

A path becomes samples in **two tiers, and which tier is the Adapter's
business.** A Clip whose decoded size fits under a limit the Adapter's config
names is decoded once at load and its Voices index one shared buffer; a longer
Clip **streams**, a decoder per Voice.

Neither tier alone is defensible, and the arithmetic is why:

- *Always decode* — a 5-minute stereo 44.1 kHz track is **101 MiB** of float32
  against ~4.8 MB encoded, **21×**; the measured mono test clip came out at
  **51.6×**. Music cannot be resident.
- *Always stream* — **460 µs** to open a decoder and **137 KB** resident per
  live decoder. A footstep would pay half a millisecond and 137 KB for a Clip
  whose entire decoded form is smaller than its own decoder. Break-even is
  **0.78 s mono / 0.39 s stereo**.

```go
// DecodedClipLimit is the largest decoded size, in bytes, a clip may have and
// still be decoded once at load; larger clips stream.
//   0  means 512 KiB, the default
//  -1  means no limit: always decode and cache
//  -2  means always stream: never decode at load
//   >0 is the limit in bytes
DecodedClipLimit int
```

The limit is on **decoded** size rather than encoded size, because decoded size
is what costs memory — and it is computable *before* decoding, as
`Length() × Channels × 4`, from the identification header and the end granule
position. At 512 KiB that is **2.97 s mono 44.1 kHz**, **1.49 s stereo
44.1 kHz**, **1.37 s stereo 48 kHz**. It sits deliberately *above* the 137 KB
memory break-even: between the two, memory is knowingly spent to make `Play`
free. **A Clip whose length reads 0** — truncated, or a non-seekable source — has
no computable decoded size and therefore **streams**.

**The knob is the Adapter's config and not `sound`'s**, because choosing the tier
needs duration, channels and rate, and those exist only after the Adapter has
parsed the headers. A `sound.Config` knob would force a two-phase Port and would
put *streamed or resident* above a seam that speaks only Clip identity and
bytes.

**The tier is invisible to the game**, which is item 12 holding, and it is why
`Preload` promises what it does.

### The read is on the tick, and the spec says so

A Clip's bytes are read from `storage` **synchronously inside `sound`'s flush**,
on the tick the `Play` or `Preload` that needs them was recorded. This is not an
implementation accident left implicit: it is the one place `sound` can stall a
frame, and a reader deserves to know where.

`storage` gives no other option — its resource must not be retained past the
handler that declared it, so there is no filesystem to hand a goroutine. The
**prepare** is what moves off the tick, not the read.

**Rejected:** a per-flush byte budget reading N bytes a tick. It makes cold-play
latency unbounded and untestable, and is strictly worse against the causality
promise than one honest stall. **Rejected:** changing `storage` to offer a
detachable reader — a redesign this effort has no business starting.

### `Preload` promises residency, not a free `Play`

> **`Preload(ClipRef)` promises that the Clip is resident and will not fail. It
> does not promise that the next `Play` is free.**

It cannot. For a short Clip the next `Play` *is* free. For a long one the first
`Play` still opens a Voice decoder, 460 µs. And the game cannot tell which it
has. A guarantee the caller cannot verify and the engine cannot keep is worse
than the weaker one that is always true.

`Preload` is also the lever for choosing *which* frame eats the read — a loading
screen rather than the first shot fired.

### What a play before its Clip is ready does

**The Voice exists from the tick that recorded it.** It is addressable,
`Stop`-able and `Seek`-able, silent until the Clip is resident, and then starts
**from its offset — no catch-up.** Catch-up would serve music sync and ruin
one-shots, which are the overwhelming majority: a 300 ms load would play a
footstep's tail.

**This is deliberately unlike the Device rule** in [The Device](#the-device),
where a playhead advances whether or not anyone can hear it. A Clip load is a
bounded hiccup the engine is actively fixing, measured in milliseconds, so
starting at the head is honest; Device readiness may never come at all. The two
rules sit here together rather than letting a reader discover the difference.

### Failure

A Clip that cannot be read or prepared is **reported once** — the Library's own
report for a read failure, `kernel.ReportErrorOnce` under the key for a prepare
failure — and is **terminal**: the entry holds the failure, and only a release
clears it. Voices already recorded against it end with `ReasonFailed`.

**There is no retry and no `ErrTryAgain`.** Every Clip failure is terminal: a bad
path, a file that is not Ogg Vorbis, a corrupt stream. The candidates for a
transient refusal did not survive — a suspended `AudioContext` still serves
`decodeAudioData` and still constructs a buffer, and `otosound`'s table is
ordinary Go memory whether or not `oto`'s ready channel has closed.
**Readiness gates audibility, not residency.**

**There is no fallback Clip.** A silence-shaped stand-in would be a lie with a
duration; a pending Voice is not playing anything at all.

### Asking about a Clip

```go
func ClipInfo(handle kernel.Read[*Clips], ref ClipRef) (ClipInfo, State)
```

`ClipInfo` carries `{Duration, Channels, SampleRate}` and **no id**. Requirement
2 is *nothing a game must load or release*, so there is no handle to a Clip, and
exporting one invites a game to hold it. What gameplay legitimately wants is a
question, not a handle.

**Asking never starts a load.** It reads `sound`'s own table, never the Library,
because the Library's `Get` is the only read it has and it loads on a miss.

**The `State` beside it is what stops this repeating `gfx`'s lie.** A path-named
`TextureDescr` reports `Size() == 0, 0` **forever**
(`slots/gfx/internal/types/texture.go:28-30`), so its caller cannot tell *not
yet* from *never*. A duration of zero and a state of `Loading` are different
answers to different questions.

### Release

**A release stops every Voice playing the Clip.** The deferral
[#373](https://github.com/dvoyni/cog/issues/373) promised is **retired, not
moved up a layer**, and the failing sequence is what decides it:

> A game releases a level's Clips, one of which is a **looping ambience**. Under
> deferral the last Voice on that Clip never ends, so the release is never
> forwarded, the memory never comes back, and nothing is reported — `Release`,
> called for the sole purpose of getting memory back, did nothing at all,
> silently, for the rest of the process.

A release that quietly does not release is worse than one that stops a sound,
and *releasing something still bound is the caller's mistake* is already the
rule `gfx`, `scene` and `canvas` live by.

- Those Voices end with **`ReasonReleased`**.
- **`ReleaseAll()` cuts everything**, which is what a teardown call means. A game
  that wants one track to bridge a transition calls `Release` per Clip and keeps
  the bridging one, or lets `ReleaseAll` cut it and starts the bridge afterwards
  — asking again reloads.
- **Requirement 2 is the backstop.** Release is optional, so a game that never
  calls it never meets any of this. That is what makes the cheap, honest answer
  good enough.
- **It is a guarantee, not a reported property.** Stopping a Voice needs no
  cooperation from a buffer's lifetime on either Adapter —
  `AudioBufferSourceNode.stop()` on one, dropping a table row on the other — so:
  **after the flush in which a release is recorded, no Voice is playing that
  Clip, on any Adapter.**
- **The stops precede the `Destroy` in the same batch**, so a release is atomic
  within its tick and the Mixer never applies a destroy for a Clip it is still
  mixing.
- **A pending Voice is cut the same way**, and ends with `ReasonReleased` rather
  than `ReasonFailed`. Nothing failed; the game changed its mind.
- **A streamed Voice's read-ahead stops with it.**

### The Loop Region

From [where a music track's loop repeats from](https://github.com/dvoyni/cog/issues/471).

> **A loop point is a fact about a Clip, not a parameter of a Voice.**

```go
// LoopRegion is the span a looping Voice repeats between, in seconds.
type LoopRegion struct{ Start, End float32 }
```

It rides in the Ogg file's **Vorbis comment header**, is parsed by the Adapter in
the same pass that already reads duration, channels and rate, and comes back up
on `PreparedClip.LoopRegion() m.Maybe[LoopRegion]`. **Absent means the whole
Clip**, following `Params`'s own rule that an absent `m.Maybe` is the default.

`Params.Loop` stays `m.Maybe[bool]` and `VoiceStart.Loop` stays `bool`: `Loop`
simply stops meaning *repeat the whole Clip* and starts meaning **repeat the way
this Clip says to**. A Clip with no tag says *whole*, which is what every Clip
says today, so no existing behaviour moves.

Read case-insensitively:

| tag | meaning |
|---|---|
| `LOOPSTART` | loop start, **in sample frames** |
| `LOOPLENGTH` | loop length in sample frames; `End = Start + Length` |
| `LOOPEND` | loop end in sample frames, used when `LOOPLENGTH` is absent |

This is the de-facto convention game-music tooling already writes, which is the
whole point of choosing it: a composer's export is the authoring surface, and no
Go code has to agree with a file it cannot see.

**A malformed region is dropped whole, never clamped.** If `Start < 0`, or
`End <= Start`, or `End > Duration`, or a value does not parse, the Adapter drops
the region entirely and loops the whole Clip, reporting once per Clip. Clamping
was rejected: a clamped loop *sounds like a working loop with the wrong loop
point*, which is the single hardest audio bug to attribute, and it would make a
tag's meaning depend on the Clip it sits in.

**`sound` needs the region for two reasons of its own**, which is why it comes
up rather than staying below:

- **The playhead.** A looping Voice wraps at the loop end, not at `Duration`.
- **`Seek`.** A seek past the end wraps **to the loop start**, not to zero.

Endings are unaffected: a looping Voice never ends by itself.

**The granule correction and the musical loop point are the same parse in the
same place.** The true final sample of a stream, against a decoded buffer that
may carry codec padding, and the `LOOPSTART` tag both come out of the Adapter's
one pass over the headers. They do not compose in the spec; they compose in one
function, and `sound` never sees either input. An absent region still means
loop-to-the-granule-end, never loop-to-the-padded-buffer-end.

**The contract promises a loop is gapless:**

> **A looping Voice produces no gap, and no repeated or dropped sample frame, at
> its loop point.** On every Adapter that makes sound.

This is a third kind of promise beside ordering and causality, and it is
admitted for one reason: **both Adapters already hold the sample-exact
mechanism**, so the promise costs nothing to keep and something real to lose.
Written down, it stops a later optimisation from spending it quietly. It is
deliberately narrow — it says nothing about underruns, about the web's ~100 ms
floor, or about anything the Device's lifecycle governs — and it is tested **at
the Adapter**, which can render to a buffer and assert on frames, not through
the game-facing surface, which by design cannot see a sample.

**Chaining two distinct tracks stays on `VoiceEndedEvent`**, and the spec states
its cost in words because it is not zero and a later reader would otherwise file
it as an oversight:

> A track chained on `VoiceEndedEvent` starts on the tick after the ending — up
> to one tick of silence, 16.7 ms at 60 Hz, and more on a slow frame. **That is
> the tolerance a playlist gets. It is not the tolerance a loop gets.**

A game that genuinely needs two segments gapless has the answer above: they were
one file with a loop region all along.

---

## The Device

From [The device's lifecycle: not ready, absent, and lost](https://github.com/dvoyni/cog/issues/299).

> **Audio never stops a game from starting**, and a Device that is missing, late
> or lost is a fact the game can read rather than an error it must handle.

### Registration never fails on a Device

An Adapter registers successfully whether or not a Device exists; `Startup`
neither blocks nor fails. This is `gfx`'s shape one Slot over, and the two read
the same deliberately: `slots/gfx/ports.go` has the driver provide its `Backend`
at registration, *before* the device exists, and report `Ready() false` until it
arrives.

A Device that cannot be opened at all — no sound card, a locked device, CI — is
reported **once**, by the Adapter, through `kernel.ReportErrorOnce`
(`otosound.ErrDeviceUnavailable`), after which that Adapter behaves exactly as
`nosound` does. The game runs.

**The error belongs to the Adapter, never to `sound`.** `gfx` reports
`ErrBackendNotReady` from the Slot the first time a frame is rendered before the
backend is ready (`slots/gfx/err.go:237-245`). **Audio must not copy that.** On
web, not-ready is the normal case for as long as the player has not clicked, and
`sound` cannot tell *I tried to open a device and failed* from *I am waiting for
a gesture that may never come* — only the Adapter knows which. A frame that did
not render is a bug; silence nobody asked about is not.

### Three states, and one rule about time

The Device is **absent** (never opened), **not ready yet** (web, before the
gesture; desktop, for the ~15 ms the spike measured) or **lost** (unplugged,
default device changed). A game sees one thing in all three: `Ready` is false.

> **A Voice's playhead advances whether or not anyone can hear it.**

A Voice recorded while the Device is not ready exists from the tick that recorded
it, advances silently, and is simply mid-Clip when the Device arrives — or has
already ended, with its `VoiceEndedEvent` delivered on schedule. A footstep
played thirty seconds before the player clicks is over before the click; music
started then is heard thirty seconds in.

**Rejected:** suspending time until the Device is ready. It makes the queue and
the first audible moment both unbounded — thirty seconds of accumulated footsteps
arriving as one wall — and it breaks the causality promise in the direction
nobody checks: a `Play` from tick *N* becoming audible after real *minutes* of
world time.

### Loss is ordinary, and is not reported

Headphones come out; the default device changes. This is common, not a fault.

- **Nothing is reported.** No `ReportError`, not even once. `Ready` goes false,
  which is the whole of the notification.
- **Playback keeps being simulated.** Clocks run, Voices end on time, parameters
  keep coalescing.
- **The Adapter retries the open on a slow cadence — one second — indefinitely.**
- **On recovery, Voices resume where the world is *now***, not where it was when
  the Device went away.

**How "where the world is *now*" is said:** the Slot notices `Ready` going true
on the tick it polls it, and restates every live Voice as a `VoiceStart` at the
playhead it is at now. A start is the only operation that carries a position, so
a restart *is* the resync, and it carries that tick's `Params` with it, so no
update is owed beside it. The cost is bounded by `MaxVoices` — the same bound a
Bus volume change already pays — and it is paid once per arrival, never per tick.

**This is the Slot's job, not the Adapter's.** An Adapter has no playheads at
all: `sound` computes them, which is what makes a test's timeline world time
rather than the Device's. An Adapter left to resync itself could only resume its
own mixer table where that table stopped, which is where the world *was*.

**It covers all three states and not just loss.** A Device that was never ready
becoming ready is the same transition and gets the same restart, which is what a
web game's first click is: every Voice played before it is re-pinned to the
world, and the ones whose Clips ran out in the meantime have already ended.

This **narrows** what [#300](https://github.com/dvoyni/cog/issues/300) handed
down: the Device is still the only thing audio reports as a failure, but *losing*
one is not. Only a machine where a Device never opened at all is.

**Consequence worth stating:** resuming a streamed Voice at an arbitrary playhead
costs a decoder open and a seek (460 µs) where a resident Voice costs an index. A
recovery that restarts many streamed Voices at once pays that many times.

> **Gap:** oto v3's behaviour on a device change was **not measured** in the
> 2026-09-12 spike. *The old device keeps being written to, and nothing errors*
> is a real possibility, in which case `otosound` needs an explicit
> close-and-reopen rather than a retry loop that never fires. The shape above
> does not change either way; what would settle it is a measurement on a machine
> whose default device is switched mid-run.
>
> Recorded in the implementation on `watch` in
> `extensions/otosound/internal/backend-device.go`, with the exact run that
> would settle it. **Still outstanding after
> [#485](https://github.com/dvoyni/cog/issues/485)**, which built the mechanism
> around it and could not measure it.

### `sound.Device`

```go
// Device reports the sound device as it is now. It is read-only: a game cannot
// choose a backend, only see which one it got.
type Device struct {
	// Ready reports whether anything is audible. False means absent, not opened
	// yet, or lost; a game cannot tell which, and does not need to.
	Ready bool
	// Name is the Adapter's own name: "otosound", "jssound", "nosound".
	Name string
	// SampleRate and Channels are the device's, not a clip's. Zero until Ready.
	SampleRate int
	Channels   int
	// Latency is the real output latency, not a figure from a Config. Zero
	// until Ready. The contract promises ordering and causality and never a
	// latency figure, so this is the only place one is honest.
	Latency time.Duration
}
```

**What a game does with it is the justification, not a nicety:** on web it draws
the *click to enable sound* prompt, because `Ready` is the only way to know the
gesture is still owed, and nothing else in the contract can say so.

Routine calls recorded rather than re-asked:

- **`nosound` reports `Ready` true.** It is a working Device that plays nothing,
  not a missing one. A game gating a prompt on `Ready` must not hang forever
  under `nosound`, and a test must not special-case it.
- **`Latency` is zero under `nosound`**, which is true rather than a placeholder:
  nothing is buffered.
- **A paused engine does not change `Device`.** Pause is not a Device state.

### Two Engines in one process

One `oto` context per process was measured, but a context mints several players,
so a second `Engine` registering `otosound` **shares the context and takes its
own player**. Both Engines are audible, which is what a second simulation in one
process presumably wants.

The compromise is stated rather than hidden: **the first composition's `Config`
owns the context**, so the second Engine's sample rate and buffer size are
ignored. That is reported once as `otosound.ErrDeviceConfigIgnored`, and its
`Device` reports the **actual** values in force, never the ones it asked for.
`jssound` has no such limit that we know of, and `nosound` has none by
construction.

**Rejected:** the second Engine goes silent. Which composition ran first is a
race, and silence decided by a race is worse than an audible Engine with a stated
compromise.

### Configuration

Each Adapter's `Config` follows `extensions/gogpu/config.go`: zero value is the
default, a zero field takes the default its comment names, `With*` builders
return modified copies, fields exported for serialization.

```go
// otosound
type Config struct {
	BufferSize time.Duration // 0 means 10ms
	SampleRate int           // 0 means 48000
	DecodedClipLimit int     // 0 means 512 KiB; see Clips
}

// jssound
type Config struct {
	LatencyHint      time.Duration // 0 means interactive
	DecodedClipLimit int
}

// nosound
type Config struct {
	SampleRate int // 0 means 48000, so a test can assert a rate without a device
}

// sound itself
type Config struct {
	MaxVoices int // 0 means 64
}
```

**Channel count is not a knob.** The output is stereo because panning is; a mono
output would make constant-power panning meaningless, and a surround output is a
different Mixer. `jssound` gets no rate — the browser owns it, and its
wasm-decoder fallback is **detected, never configured**.

### Pause suspends; Hold is not audio's

An engine Pause **silences every Voice**, and audio subscribes so that games do
nothing: a paused game that keeps playing footsteps is a bug in every game that
hits it.

It **suspends**: the playhead stops and resumes on the same sample. A paused game
that comes back to its music thirty seconds in is a bug in every game that has
ever paused.

- **The Device stays open** and keeps being fed silence. Closing it risks a
  reopen that fails and costs the open again (~15 ms desktop, a fresh gesture on
  web).
- **Pause beats not-ready.** A pause while the Device is not ready suspends
  everything, the playhead rule included: nothing advances.
- **`Hold` is not audio's concern.** `app`'s hold keeps the step window open
  under pause (`slots/app/commands.go:76-80`), and pause has already silenced.

---

## The seam

From [What a sound Backend speaks: the seam beneath the sound Slot](https://github.com/dvoyni/cog/issues/376).

> **The Adapter is a dumb, allocation-free sink with a fixed table of slots.**

It is told how many slots exist once, receives one batch per tick, multiplies a
gain matrix it did not compute, and is asked two questions. It knows nothing of
handles, Buses, priorities, positions, the cap, or the equations.

### A slot, not a handle

```go
// VoiceSlot identifies a voice to an Adapter. It is always in [0, MaxVoices),
// minted and recycled by sound, which is what lets an Adapter's voice table be
// a fixed array indexed directly by it.
type VoiceSlot int
```

`Voice`'s index/generation split is deliberately not public contract, so it
cannot cross into an Adapter without exporting the accessors
[#301](https://github.com/dvoyni/cog/issues/301) refused — so it does not cross
at all. **`sound` guarantees a slot is stopped before it is reused**, so the
Adapter never sees an ambiguous id and needs no generation of its own — and a
steal is where the two land in one batch, which is why `Stops` are applied
before `Starts`. The map
between the two is `sound`'s, and it is indexable because the handle's index half
*is* the slot.

**Settled here:** an Adapter allocates nothing per play. Its table is
`MaxVoices` long, made once at `Voices(n)`.

### A 2×2 gain matrix, computed above the seam

```go
// VoiceParams is everything about how a voice sounds, as the Adapter sees it.
type VoiceParams struct {
	// Gains maps source channels to output channels: Gains[src][out]. Row 0 is
	// a mono clip's only row. sound computes it from the voice's volume, its
	// bus, its falloff and cone and the W3C panning equations; the Adapter
	// multiplies and nothing else.
	Gains [2][2]float32
	// Rate is the playback rate, 1 being the clip's own.
	Rate float32
	// Paused stops the voice advancing without ending it.
	Paused bool
}
```

**Sixteen bytes and no equation below the seam.** A scalar pan cannot express
W3C's stereo rule — `outL = inL + inR·cos x`, `outR = inR·sin x` on one side and
its mirror on the other — and mixing a stereo Clip down to dodge it was refused.
Since a stereo positional Voice is a supported choice rather than a discouraged
one, **all four entries are ordinary traffic** rather than a defensive
generalisation.

**Rejected:** a scalar pan with a separate stereo rule, which puts half of a W3C
equation in every Adapter and lets two Adapters disagree about the same Clip.

### An Adapter does not know Buses exist

`sound` folds a Bus's volume into each Voice's matrix. There is no Bus at the
seam.

The cost is stated: **a Bus volume change re-emits every Voice on that Bus**,
bounded by `MaxVoices` and therefore at most 64 entries in one batch. That is
nothing against the alternative, which is a second place volume is decided.
Filters and reverb would genuinely want Buses in the Adapter, and they are out of
scope — adding the concept now to serve a feature that was ruled out is how a
seam ossifies around machinery nobody asked for.

### The Adapter declicks

`sound` emits **target** values once per flush; the Adapter ramps to them across
its own blocks. A `Stop` is the same mechanism: ramp to zero over a few
milliseconds, then free the slot.

The reason is that declicking is a function of the **output block rate**, which
only the Adapter knows. `sound` runs at tick rate — 16.7 ms at 60 Hz, against
`otosound`'s 10 ms buffer and a browser's 128-sample quantum — so anything it
emitted would be a step by the time it was heard.

> **Measured** ([#304](https://github.com/dvoyni/cog/issues/304)): without
> declicking the gain steps **0.078** of amplitude in a single sample at every
> 10 ms block boundary; with it, **0.00016** — smaller than the ramp's own
> per-sample motion. A **~480×** difference repeating at 100 Hz, which is what a
> zipper is.

**Consequence worth stating:** `VoiceEndedEvent` is published on the tick the
stop was recorded, not when the ramp finishes, so the event precedes the silence
by a few milliseconds. A game cannot use the event to mean *the speaker is now
quiet*, and nothing ever promised it could.

### One call per tick

`gfx.Backend.Execute`'s shape verbatim, down to the ownership note at
`slots/gfx/ports.go` — *queue is owned by the caller and valid only for the
duration of the call*.

```go
type Batch struct {
	Starts   []VoiceStart
	Updates  []VoiceUpdate
	Stops    []VoiceSlot // applied first, before the starts that reuse their slots
	Destroys []ClipID    // ordered after the stops that precede them
}

type VoiceStart struct {
	Slot   VoiceSlot
	Clip   ClipID
	Offset time.Duration // where to begin; 0 is the head
	Loop   bool
	Params VoiceParams
}

type VoiceUpdate struct {
	Slot   VoiceSlot
	Params VoiceParams
}
```

**A `Seek` is a `VoiceStart` with an `Offset`**, which is also how a web
Adapter's sample-accurate `start(when, offset)` is reached without the seam
naming a sample.

`jssound` makes its calls on the main thread, where one crossing that iterates
beats N crossings outright; `otosound` has to hand a whole tick over atomically
anyway, and exactly one handoff point per tick is what makes that possible.

**Rejected:** one call per operation. It makes the atomic-per-tick guarantee
something each Adapter must reconstruct, and on web it pays a main-thread
crossing per footstep.

### Nothing is pushed; two things are polled

```go
type Backend interface {
	// Voices tells the Adapter how many slots exist. Called once at startup,
	// before any Emit. Slots are numbered [0, n) and sound stops a slot before
	// it reuses one.
	Voices(n int)

	// Emit applies one tick's operations. batch is owned by sound and is valid
	// only for the duration of the call.
	Emit(batch *Batch)

	// Device reports the device as it is now, including Ready. Polled once per
	// flush; it is a field read, not a query.
	Device() Device

	// Prepare turns encoded Ogg bytes into something voices can be started
	// from: samples for a short clip, the bytes and a way to stream them for a
	// long one. Which, is the Adapter's business. done=true means prepared is
	// valid now; done=false means it will appear in TakePrepared under the same
	// token. The prepared value must be garbage-collectable.
	Prepare(token any, encoded assets.Blob) (prepared PreparedClip, done bool, err error)
	TakePrepared() []Prepared
	Install(PreparedClip) (ClipID, error)
	Destroy(ClipID)
}

type PreparedClip interface {
	Duration() float32
	Channels() int
	SampleRate() int
	LoopRegion() m.Maybe[LoopRegion] // absent: loop the whole clip
}

type Prepared struct {
	Token any
	Clip  PreparedClip
	Err   error
}

// ClipID is an opaque handle minted by the Adapter. Zero means none.
type ClipID uint32

// BackendPort is the Port sound requires exactly one Adapter for. A composition
// without one fails with kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[Backend]
```

**Every return path is a poll at flush**, which is `gfx.TakeCapture`'s shape. A
callback would arrive on the device thread or in a JS callback, where there is no
handler and no lock.

**And there is nothing to poll about Voices at all.** `sound` computes endings
from `Duration()` and the Voice's rate, and all five `Reason` members are things
`sound` decides, so **an Adapter never reports a Voice**. `Device()` returns the
whole struct rather than sitting beside a separate `Ready()`: one call, polled
where every other answer is polled, and no thread question to answer.

**`PreparedClip` stays opaque about samples and honest about facts.** It carries
those four because `VoiceEndedEvent` fires for *every* ending: if it were a bare
`any`, only the Adapter would know when a Clip runs out, `nosound` would never
end a Voice, and a game tested under `nosound` would behave differently from the
same game under `otosound`.

**`Install` mints the id rather than `Prepare`** so that the handle comes into
existence on `sound`'s tick, where `Destroy` is guaranteed to pair with it. That
is the same reason for the garbage-collectability rule, and the failing sequence
is:

> A Clip is released while its prepare is in flight. The entry goes. The
> completion arrives, `sound` sees a stale epoch and **drops the prepared value
> without calling anything** — there is no `Destroy` for something never
> installed. Anything native inside it is never freed.

All three Adapters satisfy the rule: `[]float32`, a `js.Value`, and nothing.

**One interface covers all three Adapters with no branch in `sound`:**
`otosound` spawns a goroutine and returns `done=false`; `jssound` returns
`done=false` and its `decodeAudioData` callback appends to a slice
`TakePrepared` drains; `nosound` parses the header inline and returns
`done=true`.

### Pause is a field, not a verb

Zeroing a gain does not suspend: a resident buffer started with `start()` keeps
advancing whatever its gain is, so a resume would land thirty seconds in. The
Adapter has to stop the source, not just silence it.

So it crosses as **`VoiceParams.Paused`**, and an engine Pause is `sound`
emitting an update for every live Voice — at most 64 entries, no special path. A
`SuspendAll()` verb would be a second way to say something the batch already
says, and two ways to say one thing can disagree.

---

## What an Adapter owes

There is **no conformance suite**, and that is forced rather than preferred
([#467](https://github.com/dvoyni/cog/issues/467) §4):

- `kernel/archtest/tiers_test.go:100` classifies any package under a plugin that
  is neither `<name>plugin` nor `internal/` as **no tier**, and `:244` files that
  as a `ruleNoTier` violation. A shipped `slots/sound/soundtest` **cannot
  exist**.
- `ruleReach` (`tiers_test.go:30`) carves one loophole — *a `_test.go` file may
  import another plugin's `internal/`* — so a shared suite **could** live at
  `slots/sound/internal/soundconformance`. **It is declined.** The obligations
  below are properties of *code*, not of observable behaviour, and samples never
  cross the Port, so a black-box suite would certify the things it can see while
  the things that matter went unchecked. That is worse than no suite, because it
  would read like one.

So the obligations are a numbered list an Adapter author reads, and **each
Adapter proves its own, in its own package.** The repo already works this way:
the two real `storage` Extensions each hand-write
`TestValuesRoundTripAndSurviveARestart` from scratch with nothing shared.

1. **Nothing decodes on the thread that fills the device buffer.** This is the
   difference between a glitch-free Mixer and an unexplained crackle.
2. **The device thread may touch** its own voice table, whatever queue carries
   batches to it *through atomics only*, the prepared Clip data it was given, and
   its read-ahead rings. **It may not** allocate, take any lock, free anything,
   call into `sound` or `kernel`, touch a `js.Value`, or decode. An Adapter with
   no device thread of its own satisfies this vacuously, which is the correct
   outcome rather than an exemption.
3. **Do not resample a Clip's base rate on the device thread.** The expensive
   resampler runs in `Prepare`; the cheap interpolation for `Rate` runs on the
   device thread.
4. **Do not bake the Device rate into what `Prepare` returns**, so that a Device
   change costs the Clip cache nothing.
5. **Declick every parameter change and every stop**, over the Adapter's own
   blocks.
6. **A looping Voice is gapless** at its loop point: no gap, no repeated or
   dropped frame.
7. **A prepared value is garbage-collectable.** Anything needing explicit release
   goes behind a `ClipID`, never into the value handed to `Install`.
8. **A slot is stopped before `sound` reuses it**, so no generation is needed
   below the seam — this is `sound`'s guarantee, and an Adapter may rely on it
   rather than defend against it.
9. **`Device()` is a field read**, cheap enough to poll every flush.
10. **A Device that could never be opened is reported once, by the Adapter**, and
    the Adapter then behaves as `nosound` does. **A lost Device is reported
    never.**

---

## Testing

From [How a game tests its sound](https://github.com/dvoyni/cog/issues/467).

> **A test composes `nosound`, drives ticks, and asserts against `sound`'s own
> tick-computed state and its events — never against samples, and never below
> the seam.**

**The determinism line is the seam.** Above it everything is deterministic under
a fixed `TimeStep`: which Voices exist, their playheads, audibility, the order
stealing takes, which endings fire and on which tick. It stays deterministic
because a playhead advances whether or not anyone can hear it, so a test's
timeline is world time and never the Device's. **Below it nothing is**, and a
test asserts nothing there — not `Device.Latency`, and not `Device.Ready` on a
real Adapter. Under `nosound` both are constants by construction, so a test may
assert them *there* and nowhere else.

That is not a caveat on the testing story; it *is* the testing story.

**History is `VoiceEndedEvent`, not the endings ring.** The ring
[the agent-facing capability](https://github.com/dvoyni/cog/issues/305) buys is
justified by one reader having no choice: an MCP client cannot subscribe to
events. A test can, so a test reads the event stream, which is unbounded and
ordered. The ring gets no `kernel.Read`-able counterpart and no game-facing type.
Handing a game a capped, lossy ring over a lossless stream it can already consume
would be strictly worse for the reader that has a choice.

**What a test asserts on**: that a Clip is playing, on the right Bus, at an
audibility of 0.4 rather than 0; that a one-shot ended, and why; that the right
Voice was stolen. **Not** the gain matrix, and not azimuth — engine arithmetic is
pinned once by `sound`'s own package tests against W3C's published numbers.

**Reading the view costs a fixture plugin.** A test driving the Engine from
outside, with no Systems of its own, writes a probe plugin declaring the read
lock — `bundles/input/internal/plugin_test.go:33` is the canonical ~15 lines, and
`slots/storage`'s and `slots/gfx`'s are the same shape. For a *game's* test this
is not ceremony at all: a game already composes its own plugin with its own
Systems, so reading `kernel.Read[*sound.Voices]` inside one is the game reading
its own engine. The seven identical copies of `permanentadapter_test.go` say the
duplication is the house pattern rather than a smell.

### `nosound` prepares the header and nothing else

`nosound` **records nothing** — it is a pure sink, and every call is accepted and
kept nowhere. The live Voice view is `sound`'s and is Adapter-independent, so an
agent or a test gets the same answer whatever was composed; putting the view in
the silent Adapter would give it only to the configuration that least needs it.

But it cannot install *nothing*, because a Voice waiting on its Clip starts at
the head when the Clip installs — so if `nosound` installed nothing, no Voice
under it would ever play.

> **`nosound.Prepare` reads the Ogg headers and the stream length, and decodes no
> samples.** It keeps sample rate, channels, duration and the Loop Region; it
> keeps nothing else.

Without a real duration `ReasonFinished` never fires, the closure property has a
hole, and a test cannot assert that a one-shot ended — which is most of what a
test of sound wants to say. It also keeps `ReasonFailed` reachable, a path no
game can otherwise exercise deliberately. And a header is not a recording of what
played, so *records nothing* survives intact.

**Measured** against `jfreymuth/oggvorbis` v1.0.5 on the
[#304](https://github.com/dvoyni/cog/issues/304) clip kit:

| clip | header only | full decode | ratio |
|---|---|---|---|
| `bellsmall.ogg` (mono 44.1k, 6.5 s) | 526 µs | 5.83 ms | 11× |
| `engine5cyl.ogg` (mono 96k, 32.6 s) | 523 µs | 60.4 ms | 115× |
| `pianoroll.ogg` (stereo 44.1k, 176 s) | 525 µs | 312 ms | **595×** |

**The header cost is flat — ~525 µs whatever the Clip's length** — which is the
property that matters: a CI suite's cost stops scaling with the audio it names.
`Reader.Length()` matched the decoded frame count **exactly** on all three, so
the duration is exact and not an estimate.

**Two implementation obligations the measurement surfaced:**

- **`Length()` requires an `io.Seeker`**, because it reads the last page's
  granule position; on a non-seekable source it returns **0**.
  `storage.FileSystem.Open` returns `fs.File`
  (`slots/storage/internal/types/filesystem.go:65`), which promises no `Seek`, so
  `nosound` header-parses a `bytes.Reader` over the bytes it was handed — O(file
  size) in I/O, still O(1) in CPU. **A duration of zero must never be silently
  accepted as a Clip's length.**
- **`NewReader` decodes the first audio packet**, not only the three header
  packets, because Vorbis data need not start at position zero. *Header only* is
  therefore **constant work**, not *zero samples*, and the spec says so rather
  than promising more than the library does.

**Rejected:** instant success with a zero duration — it makes the playhead and
duration fiction and silently removes `ReasonFinished` from every test.
**Rejected:** decode fully and discard — it buys real durations that header
parsing already gives exactly, and charges CI 595× for them on a music-length
Clip.

**`nosound` is cog's first shipped no-op Adapter**, and therefore a precedent as
well as an Adapter: *fill the Port, keep what the contract must be able to
report, keep nothing else.* There is no `nogfx` or `nostorage`; every package
that needs one today hand-writes a fixture.

---

## What is not foreclosed

- **Buses at the seam**, the day filters or reverb land. Nothing above the seam
  moves when they do.
- **More than two output channels**, which widens `Gains`. It is a different
  Mixer, not a different config.
- **An Adapter-reported Voice ending**, if a streamed Clip ever turns out to end
  at a length `Duration()` did not predict.
- **A loop region in `Params`**, for dynamic music — two loops in one file,
  switched at runtime. It is additive: a new `m.Maybe` field and a wider
  `VoiceStart`, with the absent case being exactly today's behaviour.
- **A `ReleaseUnused()`**, releasing only Clips no Voice is playing, if a game
  ever wants the memory without the silence. `sound` already knows which Clips
  have Voices.
- **Splitting `ReasonStopped`** into voice-scoped and bus-scoped members.
  Additive.
- **A per-Bus cap**, if a game ever wants a budget rather than a volume group.
- **Exposing the index/generation split of `Voice`**, which the opaque `uint64`
  keeps re-cuttable.
- **Per-Clip decoder pooling** in an Adapter — a pooled decoder opens in **74 µs**
  rather than 460 µs.
- **`nosound`'s cheap header path**: the identification header gives rate and
  channels in **42 ns** and an end-scan gives duration in ~28 µs without touching
  the setup header, at the cost of `nosound` owning an Ogg page parser. 50 Clips
  in a test is 23 ms today, which is not worth the code yet.
- **A device-changed event**, if a game turns out to want to act on recovery
  rather than poll `Device`.
- **A configurable retry cadence.** One second is a constant until something asks
  otherwise.
- **A shared conformance suite** at `slots/sound/internal/soundconformance`, if a
  third-party Adapter ever appears and something turns out to be checkable
  through the Port.
- **Exposing azimuth or the gain matrix on the live view**, if a game turns out
  to want to assert on the pan itself. Additive — the agent capability already
  reports both.
- **A Web Audio `PannerNode` backend.** The arithmetic is W3C's verbatim
  precisely so that one would agree by construction.

---

## Shapes that were rejected

- **One software mixer on every platform**, which is what
  [#300](https://github.com/dvoyni/cog/issues/300) settled and
  [#373](https://github.com/dvoyni/cog/issues/373) reversed. It ships web
  delayed and starved when the browser's own audio thread is available. **The
  browser main thread cannot be escaped**: Go wasm threads are blocked on an
  unowned 2018 issue, a Worker-hosted instance forks the runtime and puts Go's
  GC on a real-time thread, and SharedArrayBuffer needs cross-origin isolation
  and still leaves the producer on the main thread.
- **A Port named `sfx`.** In game audio *SFX* means effects as opposed to music,
  and it is the Bus name most games declare.
- **Narrow setters** (`SetVolume`, `SetPitch`, `SetPosition`, `SetPaused`). Five
  more operation types, and the ECS binding would dispatch four times to
  reconcile one Component.
- **Wholesale replace plus a mandatory Voice query.** It makes reading the state
  of a Voice mandatory machinery.
- **Buses by hash, or a fixed `SFX`/`Music`/`UI` enum.** A fixed enum forces the
  cog taxonomy on every game; `Master` is the only name `sound` owns.
- **`Stop` accepting either a Voice or a Bus** — a union in disguise.
- **A clip hash with a Name table.** Retired by
  [the name hash leaving the ecs](https://github.com/dvoyni/cog/commit/8bafb30),
  because a Component can hold a string. Re-introducing one would bring back
  exactly what that commit removed.
- **A baked Clip handle** (`BakeClip`/`ReleaseClip`). That is
  `LoadSound`/`UnloadSound`, which requirement 2 forbids outright. Additive
  later if anything ever asks.
- **A listener that follows a camera.** `sound` would import a renderer and pick
  one of two.
- **`Position2D m.Maybe[m.Vec2]`** beside the 3D field: two position fields that
  can disagree, and the second model requirement 3 forbids.
- **A world-scale setting, and a `SetDefaultFalloff` verb.** The first would be
  `sound`'s only config; the second hides state from each play.
- **Mixing stereo down to mono before panning.**
- **Two caps, one counting streamed Voices.** Foreclosed rather than rejected:
  `sound` cannot count what the seam hides from it.
- **Oldest-first stealing on its own**, which steals the music for a footstep;
  and **priority as the only key**, which makes a game enumerate every sound it
  owns before it can play two.
- **A `sound`-level `ErrBackendNotReady`.** On web, not-ready is normal.
- **Failing the composition when no Device opens.** Audio is the one subsystem
  whose total absence costs a game nothing.
- **`sound` deferring a release until the last Voice ends** — the looping
  ambience never ends, so the memory never comes back and nothing is reported.
  **And the Voice keeping its samples**, which needs a refcount in the layer
  that was kept free of lifetime rules and makes `Release` mean two different
  things on two Adapters.
- **Clamping a malformed Loop Region**, which sounds like a working loop with the
  wrong loop point.
- **A queue-next verb on a Voice**, for gapless chaining. A second scheduling
  mechanism beside `Play` for a case nothing has raised, and it reopens
  *one-shot and sustained are not distinguished*.
- **A `SuspendAll()` verb**, a second way to say what `VoiceParams.Paused`
  already says.
- **A latest-wins publish** between tick and Mixer — a batch is a delta, so
  latest-wins silently drops a `Play` whenever two ticks land in one block gap.
- **A tier enum (`Tight`/`Relaxed`) in place of a reported latency.** It forces
  the spec to defend category boundaries it will get wrong.

---

## Required work

Nothing below exists. This is the build.

**`slots/sound`** — the Slot, laid out per
[#351](https://github.com/dvoyni/cog/issues/351):

1. Root declarations: `doc.go`, `id.go`, `config.go`, `types.go` (`Voice`,
   `Params`, `Falloff`, `Cone`, `ListenerParams`, `ClipRef`, `ClipInfo`,
   `State`, `LoopRegion`, `Device`, `Batch`, `VoiceStart`, `VoiceUpdate`,
   `VoiceParams`, `VoiceSlot`, `ClipID`, `PreparedClip`, `Prepared`),
   `resources.go` (`Queue`, `Clips`, `Voices`, `Device`), `ports.go` (`Backend`,
   `BackendPort`), `events.go` (`VoiceEndedEvent`, `Reason`), `err.go`,
   `utils.go` (`ClipInfo`, the `ClipWith*` constructors, `ClipRef.Equal`).
2. `internal/` — the queue and its flush, the Voice table and stealing, the
   clip table in front of `assets.Cache`, the W3C equations transcribed from the
   W3C document, the endings ring, the MCP provider.
3. `internal/types/` — `Queue`, aliased by the root.
4. `soundplugin` — exporting only `New()`, with **zero type parameters and zero
   parameters** (`kernel/archtest/roots_test.go:547`).

**`extensions/otosound`**, **`extensions/jssound`**, **`extensions/nosound`** —
each with `doc.go`, `id.go`, `config.go`, `adapters.go`, `err.go`, an
`internal/` and a constructor package. `otosound` excludes `js`; `jssound` is
`js` only. `otosound` is specified separately in
[otosound.md](../../../../extensions/otosound/docs/specs/otosound.md).

**`bundles/ecsaudio`** — specified separately in
[ecsaudio.md](../../../../bundles/ecsaudio/docs/specs/ecsaudio.md).

**Outside `sound`:**

- **`CONTEXT.md` gains `Loop Region`** under **Audio** — the span of a Clip a
  looping Voice repeats between, declared by the Clip and never by the game.
  Every other Audio term is already written and reads true.
- **`nosound` joins `archtest`'s whole-cog composition**
  (`kernel/archtest/composition_test.go:38-42`) beside the existing fixture
  Adapters, and `sound` must satisfy `ruleSlotPort`, `ruleRootFiles` and the
  Adapter-declaration rules like every other Slot.
- **`m.Transform` is done** — `libs/m/transform.go` exists, with
  `Forward()`/`Right()`/`Up()` on both `Mat4` and `Transform`, and
  `scene.Transform` is an alias for it. Nothing here is waiting on it.

**Already true and worth not re-deriving:** `libs/assets` is built and is what
holds the encoded bytes; `m.Maybe` is `libs/m/maybe.go`; `m.QuatRotationX` is
`libs/m/quaternion.go:120`; `kernel.ReportErrorOnce` is `kernel/kernel.go:52`.

---

## Out of scope

Ruled beyond this effort. These do not return except as a fresh effort.

- **HRTF and binaural spatialization.** Nothing permissive in the Go ecosystem
  provides it, and it would mean an FFT convolver plus an HRIR dataset. The queue
  speaks in position and Listener rather than gain and pan, so a later effort
  would not redraw what a game *says* — but it **would** redraw the Adapter seam,
  which now receives only a gain matrix. The front/back fold v1 ships with is the
  honest price of that.
- **Filters and reverb.** Low-pass occlusion and Freeverb are the named
  candidates, and Buses are where they would attach. The filtering would run in
  the Adapter, with `sound` computing only its parameters.
- **An arbitrary DSP or effects node graph.** A general graph ossifies a contract
  around machinery nobody has asked for.
- **Doppler, and a velocity in the model.** v1 forecloses nothing: `Params`
  carries `Pitch` and `VoiceParams` carries `Rate`, so a later `Velocity` is an
  additive `m.Maybe` feeding a path that already exists and no Adapter changes. A
  speed of sound needs a unit, and that question belongs to Doppler's own effort.
- **More than one Listener.** Split-screen is the case, and nothing has asked for
  it. `SetListener` can later take an optional listener whose absence means the
  only one, without breaking a caller.
- **MP3, FLAC, Opus and WAV decoding.** Ogg Vorbis only. MP3's one Go decoder is
  archived, Opus has no mature pure-Go decoder, and the rest earn nothing v1
  needs.
- **cgo Adapters shipped by cog**, such as miniaudio, OpenAL Soft, FMOD or Steam
  Audio. One is *allowed* as an opt-in Extension a game composes itself: it
  breaks no web build and costs nothing to a game that does not import it. cog
  ships none.
- **A live, growing audio stream** — voice chat, a synthesizer, a video's audio
  track. A func cannot be a Component, it is not declarative, and a node-graph
  backend could serve it only through a worklet fed from the main thread, which
  is what starves.
- **Caller-supplied PCM Clips.** Deferred rather than abandoned: the source of a
  `ClipRef` is opaque, so a `ClipWithSamples` is an additive constructor. Kept
  out because nothing asks for synthesis and it would put a sample rate back into
  the request types.
- **An agent making sound.** Ruled out in
  [mcp.md](mcp.md): input is synthesized because the player *is* an input,
  whereas sound is an output, and a tool that injected it would make every later
  observation describe a world the game did not produce.
