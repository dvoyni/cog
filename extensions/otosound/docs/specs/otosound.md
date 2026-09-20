# cog otosound — specification

`github.com/dvoyni/cog/extensions/otosound` is an Extension that fills
[`sound`](../../../../slots/sound/docs/specs/sound.md)'s `BackendPort` on
desktop: our own software Mixer over `ebitengine/oto/v3`, in pure Go, decoding
Ogg Vorbis with `jfreymuth/oggvorbis`.

It is the half of audio that touches a device thread, and that is what earns it a
document of its own: **everything below is invisible through the Port**. The
residency tiers, the ring that carries a tick's batch to the Mixer, the per-block
declick ramp, the offline resampler and the read-ahead goroutines are all
properties of *code* rather than of observable behaviour, and no test above the
seam can see any of them. `sound.md` writes the obligations as a numbered list an
Adapter author reads; this document is how **this** Adapter discharges them.

The design is bound by three requirements, in this order:

1. **The device thread never stalls.** It may not allocate, lock, free, call into
   `sound` or `kernel`, or decode. A callback that waited on the scheduler is
   audible as a click, and a buffer it failed to fill is audible as silence.
2. **cgo-free.** `oto` and `oggvorbis` are both pure Go, which is what keeps
   cross-compilation free and what lets the same decoder run under `js/wasm` in
   `jssound`'s fallback path.
3. **No tick is lost.** A batch is a delta, not a frame, so the handoff must be
   lossless and ordered even when the device thread has not run for several
   ticks.

Two properties fall out. **The Mixer never frees**, so a destroy travels in the
batch and the memory comes back on the tick side behind an atomic counter. And
**the expensive work runs in `Prepare`**, on a goroutine, so the device thread
only ever copies, multiplies and interpolates.

Assembled from
[Crossing the thread boundary: how a tick's writes reach the audio callback](https://github.com/dvoyni/cog/issues/302),
[Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298),
[The device's lifecycle](https://github.com/dvoyni/cog/issues/299),
[What a sound Backend speaks](https://github.com/dvoyni/cog/issues/376) and
[the prototype](https://github.com/dvoyni/cog/issues/304), plus the spike
recorded on [the audio map](https://github.com/dvoyni/cog/issues/294). Where a
claim rests on something unverified it is marked **Gap**; where assembling
decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**Nothing of this is implemented.** Neither is `sound`.

**It excludes `js` by build tag.** A web build that composes `otosound` must fail
to compile rather than quietly ship a Mixer that starves: Go's wasm target is
single-threaded, so the Mixer would compete with frame work, and `oto` requests
each chunk by `postMessage` to the main thread and fills **silence** when the
reply is late. The spike measured 1.880 s of audio produced for a 4.000 s window
at a 20 ms buffer, on an *idle* page. The browser gets `jssound`.

---

## Contents

- [Vocabulary](#vocabulary) · [What the spike measured](#what-the-spike-measured)
- [The device](#the-device) · [The handoff](#the-handoff) · [The Mixer](#the-mixer)
- [Preparing a clip](#preparing-a-clip) · [Streaming](#streaming) · [Resampling](#resampling)
- [Declicking](#declicking) · [How the obligations are proved](#how-the-obligations-are-proved)
- [What is not foreclosed](#what-is-not-foreclosed) · [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

Only one term here is a game's: **Mixer** — *what turns Voices into the samples a
Device consumes, running outside the Engine on the Device's own clock rather
than the tick, and reached by nothing that takes a lock.* It is in `CONTEXT.md`
under **Audio**, and §5 of the handoff is the rule that gives it a subject.

Everything else in this document — the batch ring, a voice slot, a read-ahead
ring, a block — is **machinery a game never meets**, and none of it enters the
glossary. `CONTEXT.md` is a glossary, not a description of how the engine is
built.

---

## What the spike measured

Run 2026-09-12 against `oto` v3.5.0 on Windows (WASAPI). These are the facts the
rest of this document is built on, and they are measurements rather than
assumptions.

| | |
|---|---|
| Builds at `CGO_ENABLED=0` | `windows/amd64`, `darwin/arm64`, `linux/amd64`, `js/wasm` |
| Desktop latency | **10 ms with zero underruns** over 12 s — 1200 reads, exactly 12.000 s produced |
| The pull path allocates per unit time, not per read | 41 reads and 400 reads in a fixed 4 s window both produced ~458 allocations (~114/s) |
| Context open | ~15 ms |
| Contexts per process | **one**, but a context mints several players |
| Consoles | `driver_console.go` carries `//go:build nintendosdk \|\| playstation5`; `internal/oboe` covers Android |

**The API is the pull shape we want:** `ctx.NewPlayer(io.Reader)` with
`FormatFloat32LE` — our Mixer *is* the `io.Reader`, and `oto` never learns what a
Voice is.

**~114 allocations per second is `oto`'s own**, not ours, and it is the figure
the no-allocation obligation is measured *against* rather than included in: what
this Adapter owes is that **its** contribution is zero.

---

## The device

### Opening, and never failing the game

`otosound` registers successfully whether or not a device exists, and `Startup`
neither blocks nor fails — `gfx`'s shape one Slot over.

A device that cannot be opened at all is reported **once**, through
`kernel.ReportErrorOnce`, as `otosound.ErrDeviceUnavailable`. After that
`otosound` behaves exactly as `nosound` does: it accepts everything and plays
nothing, and the game runs.

`Device()` is a **field read**, cheap enough for `sound` to poll every flush. The
device side writes it; the tick side reads it. It is the one value that genuinely
crosses in the other direction, and it changes under its reader with no
notification, which is correct because loss is silent.

### Loss and recovery

Loss is **ordinary and is not reported** — no `ReportError`, not even once.
`Ready` goes false, and that is the whole of the notification. The Adapter
**retries the open once a second, indefinitely**, and on recovery every live
Voice resumes where the world is *now*.

**Recovery is a burst, and it is the worst moment this Adapter has.** Every live
Voice restarts at an arbitrary playhead at the same instant: a resident Voice
costs an index, a streamed one costs a decoder open and a seek — **460 µs each**.
Sixty-four streamed Voices recovering together is ~29 ms of work that must not
land on the device thread. It is therefore done by the same read-ahead goroutines
that fill the rings, with the Mixer playing silence for the slots whose rings are
not yet primed.

**A lost Device frees releases at once again.** A released Clip is held until
the Mixer's applied counter passes the batch that destroyed it, and a lost
Device stops that counter for as long as the loss lasts. The device goroutine
therefore **detaches the ring** when it gives the player up — the same state a
ring that has never been pulled from is in, meaning the same thing, that nothing
is mid-copy out of anything — and the tick goes back to freeing immediately.
Without it every release after a loss is held: a leak rather than a crash,
bounded by what the game releases, and unbounded in time.

**The order is contract.** `Ready` goes false, then the player is stopped, then
the ring is detached. Stopping the player is `oto`'s `PauseAndStopReading` and
not `Pause`: `Pause` keeps reading the source so that playback can resume
without a gap, and a source still being read is a device thread that still
exists. `PauseAndStopReading` blocks until an ongoing read finishes and starts
no other, which is the barrier the detach needs.

**Recovery is a restart per live Voice, and `sound` emits it.** The Slot
restates every live Voice as a `VoiceStart` at its current playhead on the tick
it sees `Ready` go true, because a start is the only operation carrying a
position. For a resident Voice that is an index; for a streamed one it is the
decoder open and seek priced above, and [#482](https://github.com/dvoyni/cog/issues/482)
owns keeping that off the device thread.

**Loss is not reported however it presents itself.** Once a Device has been
ready in this Engine, nothing the device goroutine hits is reportable again —
not a poll that errors, and not the reattach after it refusing to take a player.
Both are the same Device being gone, and a game that has already heard something
must never be told the machine has no sound card.

> **Gap:** `oto` v3's behaviour on a device change was **not measured** in the
> spike. *The old device keeps being written to, and nothing errors* is a real
> possibility, in which case the retry loop never fires and `otosound` needs an
> explicit close-and-reopen instead. What would settle it: a run on a machine
> whose default device is switched mid-play, watching whether the player's writes
> begin to fail.
>
> Recorded in the implementation on `watch` in
> `extensions/otosound/internal/backend-device.go`, with the exact run that
> would settle it. **Still outstanding after
> [#485](https://github.com/dvoyni/cog/issues/485)**, which built the mechanism
> around it and could not measure it.

### Two Engines in one process

The context is per process, so a second `Engine` composing `otosound`
**shares it and takes its own player**. Both Engines are audible.

**The first composition's `Config` owns the context**, so a second Engine's
`SampleRate` and `BufferSize` are ignored. That is reported once as
`otosound.ErrDeviceConfigIgnored`, and `Device()` reports the **actual** values in
force rather than the ones it asked for.

### Config

```go
type Config struct {
	// BufferSize is how much audio the device buffers.
	//   0 means 10ms, the default, which the spike ran for 12s with no underrun
	BufferSize time.Duration
	// SampleRate is the rate the Mixer produces and the device consumes.
	//   0 means 48000
	SampleRate int
	// DecodedClipLimit is the largest decoded size, in bytes, a clip may have
	// and still be decoded once at load; larger clips stream.
	//   0 means 512 KiB   -1 means never stream   -2 means always stream
	DecodedClipLimit int
}
```

`extensions/gogpu/config.go`'s convention: zero value is the default, `With*`
builders return modified copies, fields exported for serialization.

**Channel count is not a knob.** The output is stereo because panning is.

---

## The handoff

From [#302](https://github.com/dvoyni/cog/issues/302). This is the sharpest part
of the Adapter: the device callback runs **outside the engine**, driven by the
sound card, holding no lock and permitted to take none, while the tick writes the
state it reads.

> **A ring of preallocated batches, published with one atomic store and drained
> whole at a block boundary.**

```
Emit        -> copy into the ring's write slot -> atomic store of the write index
block start -> atomic load -> drain every pending batch, in order, applying each whole
```

Wait-free on both sides. The tick never waits on the device thread, which is the
one thing it must never do, and the Mixer never waits on the tick.

**Depth 8** — about 130 ms at 60 Hz. Deep enough that an ordinary scheduling
hiccup on either side is invisible, shallow enough that the whole ring is
`8 × (3 × MaxVoices)` fixed-capacity entries, made once at `Voices(n)`.

### Why not `gfx`'s latest-wins triple buffer

`gfx` publishes frames latest-wins (`slots/gfx/internal/plugin.go:22-38,
212-233`), which is right for a frame and wrong here.

> **The failing sequence.** Two ticks land between two blocks. Tick *N*'s batch
> starts a Voice; tick *N+1*'s batch updates a different one. Latest-wins
> publishes *N+1* over *N*, and the Mixer never sees the start. **The Voice never
> sounds, and nothing reports it.**

A frame is a **complete description** and may be replaced. A batch is a
**delta**, and `sound`'s contract requires that a `Play` is never lost.

### A full ring merges; it never blocks and never drops

A full ring means the device thread has not run for eight ticks — a stalled or
dying device, where `Device.Ready` is very likely already false.

The tick side owns one **staging batch** that has not been published. When the
ring is full, `Emit` merges into staging instead of pushing: starts and stops
append, updates coalesce by slot. That is the same coalescing `sound` already
performs within a tick, so it introduces no new semantics — it just widens the
window it coalesces over. Because staging is unpublished, the merge touches
nothing the Mixer can be reading.

**Rejected:** dropping the oldest batch, which loses exactly the starts the
contract promised to keep. **Rejected:** blocking until the Mixer drains, which
makes a dead device stall the game. **Rejected:** a seqlock — the reader retries,
and a retry in a device callback is a spin whose bound is the writer's schedule.

### There is no tearing, only latency

A batch is applied **whole** at a block start, so a tick's operations become
audible together: a `Play` then a `SetVoice` carrying a position, in one tick, is
indistinguishable from a `Play` that carried the position. The list of tolerable
inconsistencies is **empty**.

What is real is bounded latency: a batch published mid-block is applied at the
next block start — **at most one block, 10 ms at the default**.

Routine consequences recorded rather than re-asked:

- **Several batches can land in one block.** They are applied in order, not
  coalesced. Two ticks' worth of operations taking effect at one instant is
  correct: they were ordered, and they stay ordered.
- **A play and a stop in one tick** arrive in one batch, applied in order, so the
  Voice is audible for zero samples. It still gets a handle and a
  `VoiceEndedEvent`.
- **The block boundary is not the tick boundary**, and nothing tries to align
  them. `Seek` being block-accurate is the same admission from the other side.

### The Mixer never frees

A release is stated on the tick, as a `Batch.Destroys` entry, and the Mixer may
be mid-copy out of that Clip's samples. There is no `Backend.Destroy` to state
it any other way ([#484](https://github.com/dvoyni/cog/issues/484)), because the
batch is the only route with an order.

So a destroy is **an operation inside the batch**, ordered after the stops that
precede it, and the memory is freed on the tick side only once the Mixer has
passed it:

```
Mixer, at the end of each drain: atomic store of "applied up to batch N"
tick,  each flush:               atomic load; free the clips destroyed in batches <= N
```

The Mixer never frees and never reads freed memory. The tick never waits — a Clip
whose batch has not been applied yet is simply freed a tick or two later.

**Rejected:** a latest-wins snapshot of complete Mixer state instead of deltas.
It would restore losslessness, but the Mixer owns each Voice's playhead — it
advances it per sample — so a published complete state would either clobber the
playhead or have to carry it, which means the tick reading state the Mixer is
writing. The delta is what keeps the playhead entirely on one side.

---

## The Mixer

### What it may touch

This is stated as a rule because it is the kind of invariant that erodes one
convenient call at a time.

> **The Mixer may touch:** its own voice table; the batch ring, through atomics
> only; the prepared Clip data it was given; its read-ahead rings.
>
> **The Mixer may not:** allocate, take any lock, free anything, call into
> `sound` or `kernel`, touch a `js.Value`, or decode.

The allocation constraint falls out of this rather than standing alone: the ring,
its batches, the staging batch, the voice table and every read-ahead ring are all
made once, at `Voices(n)` and at `Prepare`.

### What it does per block

For each live slot, in order:

1. Advance the playhead by `blockFrames × Rate`, wrapping at the
   [Loop Region](../../../../slots/sound/docs/specs/sound.md#the-loop-region)'s
   end if the Voice loops and at the Clip's end if it does not.
2. Read frames — an index into a shared buffer for a resident Clip, a copy out of
   the read-ahead ring for a streamed one.
3. Interpolate for `Rate`, if it is not 1.
4. Multiply by the 2×2 gain matrix, ramping from the previous block's matrix to
   this block's target.
5. Accumulate into the output block.

**It never decides anything.** It holds no notion of a Bus, a priority, a
position, the cap or a handle. A stolen Voice reaches it as an ordinary stop.

### The voice table

A fixed array of `MaxVoices` entries, indexed directly by `VoiceSlot`. `sound`
guarantees a slot is stopped before it is reused, so **no generation is needed
here** and the Adapter may rely on that rather than defend against it.

**An Adapter allocates nothing per play.**

---

## Preparing a clip

`Prepare(token, encoded)` returns `done=false` and **spawns a goroutine**, which
is what keeps the decode off both the tick and the device thread. The completion
lands in a slice that `TakePrepared` drains on the next flush.

The goroutine:

1. Parses the identification and comment headers, giving sample rate, channels
   and the [Loop Region](../../../../slots/sound/docs/specs/sound.md#the-loop-region)
   tags. `oggvorbis.GetCommentHeader` reads them from the header alone with no
   decode.
2. Reads the end granule position, giving the exact frame count.
3. Computes the decoded size as `frames × channels × 4` and compares it against
   `DecodedClipLimit`.
4. **Under the limit:** decodes the whole Clip, resamples it to the device rate
   once (see [Resampling](#resampling)), and hands back a `PreparedClip` holding
   `[]float32`.
5. **Over the limit, or a frame count of 0:** hands back a `PreparedClip` holding
   the encoded `assets.Blob` and what it needs to open decoders against it.

**The prepared value is garbage-collectable** — `[]float32` or a Blob, never
anything needing explicit release. That is the Port's rule, and the failing
sequence it exists for is a Clip released while its prepare is in flight: the
completion is dropped with nothing called, so anything native inside it would
never be freed.

**A frame count of 0 streams**, because there is no computable decoded size.

**Both tiers report the same facts** — duration, channels, rate and loop region —
so `sound` cannot tell which it got, and neither can a game.

**The Adapter drops a decoded Clip's encoded bytes** once decoded, and **retains
a streamed Clip's**. Retaining costs nothing: an `assets.Blob` is a pointer and a
length (`libs/assets/blob.go:30`), so it references the same run the cache holds.

### Why the tier lives here and not in `sound`

Choosing it needs duration, channels and rate, and those exist only after the
headers are parsed — which happens here. A `sound.Config` knob would force a
two-phase Port and would put *streamed or resident* above a seam that speaks only
Clip identity and bytes.

### The measurements that set the default

Run 2026-09-19 against `jfreymuth/oggvorbis` on Windows, `CGO_ENABLED=0`.

| | cost |
|---|---|
| Open a decoder (seekable source) | **460 µs**, 137 KB |
| Open a pooled / reused decoder | **74 µs** |
| Identification + comment headers only | **42 ns** |
| Setup header alone | **431 µs** (54% in `huffmanBuilder.put`'s linear `findEmpty`) |
| Decode one 10 ms block | allocates on every call |
| Resident PCM against encoded, mono test clip | **51.6×** |
| Resident PCM against encoded, 5-minute stereo track | **21×** (101 MiB) |

Break-even — where decoded samples are smaller than the decoder streaming them —
is **0.78 s mono / 0.39 s stereo**. The 512 KiB default sits deliberately *above*
the 137 KB memory break-even: between the two, memory is knowingly spent to make
`Play` free. At 512 KiB that is **2.97 s mono 44.1 kHz**, **1.49 s stereo
44.1 kHz**, **1.37 s stereo 48 kHz**.

> **Gap:** the 431 µs setup header is a **library** defect rather than a codec
> floor. libvorbis splits setup (`vorbis_info`, shareable across streams) from
> per-stream state (`vorbis_dsp_state`), and `jfreymuth/vorbis.Decoder`
> conflates them. **libvorbis and stb_vorbis were not benchmarked**, so the claim
> that the split recovers most of the 460 µs rests on reading their structure.
> Tracked as
> [otosound: the vorbis setup header costs 460 µs and 137 KB per decoder](https://github.com/dvoyni/cog/issues/466).
> It is not blocking: the tiers are the right shape whatever the library costs.
>
> **No pure-Go alternative exists.** Ebitengine wraps the same library, and every
> other option is cgo, which requirement 4 of the audio effort rules out.

---

## Streaming

> **No Clip's bytes are decoded on the thread that fills the device buffer.**

A streamed Voice gets a **read-ahead goroutine and a ring buffer**. The device
callback only ever copies from rings; the goroutine decodes ahead into them.

- **The ring is sized once**, at the Voice's start, and is never reallocated.
- **Read-ahead stops with its Voice**, including a stolen one, a released one and
  one cut by a despawn.
- **A wrap at a Loop Region seeks ahead of the wrap while filling**, not at it, so
  the page decode never lands on the wrap's own block. `oggvorbis.Reader.SetPosition`
  seeks the page before the target and skips the remainder, so it is
  **sample-exact**; it errors only on a non-seekable source, and the streamed tier
  reads from an in-memory Blob, which is seekable. *Implementation guidance, not
  a contract term — what is contract is that the loop is gapless.*
- **An underrun on a ring plays silence for that slot**, not for the block: one
  Voice that could not keep up must never take the rest of the mix with it.

**Sixty-four streamed Voices is about 8.7 MB and 64 goroutines**, which is the
worst case `sound.md` states rather than legislates against, because `sound`
cannot count what the seam hides from it.

**An underrun advances the playhead; the head of a stream does not.** A Voice
whose ring has not primed yet holds its playhead rather than advancing through
silence, because the first fill is a Clip load and a Clip load starts from its
offset with no catch-up. Once a Voice has sounded, an underrun advances: by then
it is the Device's rule - a playhead advances whether or not anyone can hear it
- and a stall that held would put the Voice permanently behind the world it is
scored to. The head rule is also what keeps a streamed one-shot away from the
[backstop](#the-voice-table): a playhead a few milliseconds behind `sound`'s
reaches the Clip's end *after* the Stop that declicks it, never before.

**A streamed Voice's recovery is an ordinary start.** The Slot restates every
live Voice as a `VoiceStart` at its current playhead
([#485](https://github.com/dvoyni/cog/issues/485)); for a streamed Voice the
Adapter halts the read-ahead that was filling the old ring and opens a new one
at that offset, and that is where the decoder open and the seek happen. The
Mixer plays silence for the slot until the ring primes, and the playhead holds
at the head while it does, so nothing of the Clip is lost to the restart. Sixty-
four of them is sixty-four goroutines rather than 29 ms on the thread that has
10 ms to fill a buffer, which is what "recovery is a burst" costs once it is
paid on the right side of the seam.

**A decoder that will not open, or fails mid-Clip, ends the Voice and reports
nothing.** A Clip failure is terminal and is reported once, at `Prepare`, where
a Clip that cannot be read is refused outright; a decoder that fails after that
belongs to a Clip which parsed, installed and has been playing, and the game has
already been told everything it is owed. What it gets is silence and an ending,
which is what any Voice that ran out gets.

---

## Resampling

> **The expensive resampler runs offline, in `Prepare`. The cheap one runs on the
> device thread.**

- **`Prepare` converts a Clip to the device rate once**, with a
  Blackman-windowed sinc, **lowpassed when decimating** — which is what keeps a
  96 kHz source from folding everything above 24 kHz into the audible band.
- **The device thread only ever interpolates for `Rate`**, where linear and
  Catmull-Rom were both acceptable in listening.

`Prepare` is not on the device thread and can afford the good one; the device
thread cannot, and does not need to.

**Pitch by resampling was judged acceptable by ear**
([#304](https://github.com/dvoyni/cog/issues/304)), which is what settled the
question at all — it was fog on the map until the Mixer's shape was fixed.

**A streamed Voice resamples in its read-ahead goroutine**, for the same reason
and with the same filter.

**`Prepare` must not bake the device rate into anything the Clip cache keys on**,
which is what keeps a device change from costing the cache anything. What it
returns is rate-converted; what the cache holds is the encoded bytes.

**The cutoff sits at 0.9 of the lower Nyquist rather than on it**, and the
kernel is stretched to that cutoff rather than truncated at 48 taps. A windowed
sinc has a transition band of real width - about 5.5/N of the input rate for a
Blackman window over N taps - and a cutoff placed exactly on the new Nyquist
puts half of that transition above it, where whatever survives folds. What the
margin costs is the top tenth of the new band, 21.6 kHz upwards against a 48 kHz
device, which is above what anyone hears. Upsampling keeps the cutoff at the
source's own Nyquist and the kernel at 48 taps, because there is nothing above
it to fold.

> **Measured** (2026-09-20,
> `TestTheOfflineResamplerDecimatesWithoutFoldingTheBandAboveNyquist`): a 30 kHz
> tone in a 96 kHz source, decimated to 48 kHz, arrives at 18 kHz **106 dB
> down**, against a passband flat to **0.00 dB**. The same conversion by the
> linear interpolation that stood here until
> [#482](https://github.com/dvoyni/cog/issues/482) folds it at **0 dB** - full
> amplitude - because at an exact 2:1 ratio every output frame lands on an input
> frame and the interpolation never runs at all.

**The two tiers are one filter, and it is asserted rather than assumed.** The
resampler is incremental: `Prepare` drives it in one call and a read-ahead
drives it a few thousand frames at a time, keeping the window's history across
the chunks and across a loop's wrap, so a streamed Clip comes out frame for
frame what the same Clip held resident would have been. A Clip must not sound
different for having been long enough to stream.

---

## Declicking

`sound` emits **target** values once per flush; the Adapter ramps to them across
its own blocks — a **per-block linear ramp**.

The reason it is the Adapter's and not `sound`'s is that declicking is a function
of the **output block rate**, which only the Adapter knows: `sound` runs at
16.7 ms at 60 Hz against a 10 ms buffer here, so anything it emitted would be a
step by the time it was heard.

> **Measured** ([#304](https://github.com/dvoyni/cog/issues/304)): without the
> ramp, the gain steps **0.078** of amplitude in a single sample at every 10 ms
> block boundary; with it, **0.00016** — smaller than the ramp's own per-sample
> motion. A **~480×** difference repeating at 100 Hz, which is what a zipper is.
>
> A per-block **linear** ramp between two tick-rate matrices was judged clean for
> a fast-moving source. Nothing heard argues for anything smoother.

**A `Stop` is the same mechanism**: ramp to zero over a few milliseconds, then
free the slot. This is why `VoiceEndedEvent` precedes the silence by a few
milliseconds, which `sound.md` states as contract.

---

## How the obligations are proved

There is **no conformance suite**, and that is forced rather than preferred:
`kernel/archtest/tiers_test.go:100` makes a shipped `slots/sound/soundtest`
impossible, and the `_test.go` loophole at `:30` was declined because a black-box
suite would certify what it can see while the things that matter went unchecked.

So each obligation is proved here, in this package. The prototype demonstrated
all of this is doable
([`proto/audio-spatialize`](https://github.com/dvoyni/cog/tree/proto/audio-spatialize)).

| obligation | how it is proved |
|---|---|
| Nothing decodes on the device thread | the decode path has no call site reachable from the callback; asserted structurally and by a test that runs a streamed Voice with a decoder that panics if called on the device goroutine |
| The device thread allocates nothing | `testing.AllocsPerRun` at **zero** over a full block, with the ring, table and read-ahead rings pre-made |
| No batch is lost | a full-ring test: push more batches than the depth, drain, and assert every `Play` arrived in order |
| The Mixer never frees, and never reads freed memory | destroy-in-batch plus the applied-counter, tested by destroying a Clip a Voice is mid-copy from |
| Declicking | assert the maximum per-sample gain step across a block boundary is below the ramp's own step |
| A loop is gapless | render a looping Voice to a buffer and assert no repeated or dropped frame at the wrap |
| The resampler does not alias | decimate the 96 kHz clip and assert the band above Nyquist is empty |
| No base-rate resampling on the device thread | the sinc path is reachable only from `Prepare` and the read-ahead goroutine |

**Render to a buffer, not to a device.** Every one of these is assertable without
a sound card, which is what keeps them runnable in CI.

**The repo already works this way.** The two real `storage` Extensions each
hand-write `TestValuesRoundTripAndSurviveARestart` from scratch with nothing
shared, and `permanentadapter_test.go` is byte-identical across seven packages.

Test idiom is the repo's: same-package `*_test.go`, full-sentence test names,
`<topic>bench_test.go` with no underscore, `b.ReportAllocs()` in code — and **no
race detector in this environment**, so concurrency tests run with `-count=10`
and say so.

---

## What is not foreclosed

- **Per-Clip decoder pooling**, which would close most of the gap `Preload`
  cannot close: a pooled decoder opens in **74 µs** rather than 460 µs.
- **Upstream library work** — [#466](https://github.com/dvoyni/cog/issues/466),
  splitting the shareable setup from per-stream state, which would move both the
  460 µs and the 137 KB.
- **A deeper or configurable batch ring.** Eight is a constant until something
  asks otherwise.
- **Aligning a block boundary to the tick**, which nothing currently needs and
  which sample-accurate music would be the first to want.
- **A smoother inter-block interpolator** than linear, if a case ever turns up
  that the ramp does not serve.
- **Buses in the Mixer**, the day filters or reverb land. Nothing above the seam
  moves when they do.
- **A configurable device retry cadence.** One second is a constant.

---

## Shapes that were rejected

- **A latest-wins triple buffer** between tick and Mixer — it silently drops a
  `Play` whenever two ticks land in one block gap.
- **A latest-wins snapshot of complete Mixer state** — the Mixer owns the
  playhead, so it would either be clobbered or have to cross back.
- **A seqlock** — a retry in a device callback is an unbounded spin.
- **Dropping the oldest batch on a full ring**, and **blocking until the Mixer
  drains**. One loses the starts the contract keeps; the other makes a dead
  device stall the game.
- **The Mixer freeing anything**, including a Clip it has just finished with.
- **Decoding on the device thread**, even for one block, even "just for the first
  block of a streamed Voice".
- **Resampling the base rate on the device thread.**
- **One residency tier.** Always-decode cannot hold music (21×); always-stream
  charges a footstep 460 µs and 137 KB for something smaller than its own
  decoder.
- **A second `NoDecodedClips bool` beside the limit** — `-2` does that job, and
  one field with sentinels beats two fields that can disagree.
- **A per-flush byte budget on the `storage` read.** It makes cold-play latency
  unbounded and untestable.
- **`otosound` on `js/wasm`.** Measured starving; the browser gets `jssound`.
- **`otosound` falling back to silence without reporting** when no device opens.
  That hides a real failure, which is why `nosound` exists as a deliberate
  composition instead.
- **The second `Engine` going silent** when it cannot own the context. Which
  composition ran first is a race.

---

## Required work

Nothing below exists.

1. **Root declarations**: `doc.go`, `id.go`, `config.go`, `adapters.go`,
   `err.go` (`ErrDeviceUnavailable`, `ErrDeviceConfigIgnored`). Build-tagged to
   exclude `js`.
2. **`internal/`**: the `oto` context and player, the `Backend` implementation,
   the batch ring and staging batch, the voice table, the Mixer's `io.Reader`,
   the header parser, the decode goroutines, the read-ahead rings, and the sinc
   resampler.
3. **`otosoundplugin`** exporting only `New()`, with zero type parameters and
   zero parameters.
4. **Dependencies**: `github.com/ebitengine/oto/v3` and
   `github.com/jfreymuth/oggvorbis` — both pure Go, both already verified to
   build at `CGO_ENABLED=0` on every current target.
5. **The prototype is worth re-reading before starting.**
   [`proto/audio-spatialize`](https://github.com/dvoyni/cog/tree/proto/audio-spatialize)
   under `docs/research/audio-spatialize/` already separates `sound`, `otosound`
   and the game at the same three seams, and its `mixer.go` is this Adapter in
   miniature: slots, a matrix, a rate and a paused flag across the ring, with the
   declick over its own blocks. It is out of `main` and disposable, but it is
   where the arithmetic was checked against W3C's landmarks — equal power to 1e-5
   across a sweep, a full orbit favouring neither ear by more than 0.002%.

---

## Out of scope

- **Anything `sound` owns**: the queue, Voices and handles, Buses, the cap,
  stealing, the Listener, the equations, the Clip cache, `VoiceEndedEvent`.
- **HRTF.** It would redraw the seam, because this Adapter receives a gain matrix
  and nothing a convolver could use.
- **Filters and reverb.** They would run here, and Buses would have to cross the
  seam for them. v1 ships without them.
- **More than two output channels**, which is a different Mixer rather than a
  different config.
- **Formats other than Ogg Vorbis.**
- **cgo**, in any form, in this Extension. A cgo Adapter is allowed in cog as an
  opt-in Extension a game composes itself; it is not this one.
- **The browser.** `jssound` is its own Extension with its own document, and the
  half of [#304](https://github.com/dvoyni/cog/issues/304) that checks the same
  arithmetic against a `PannerNode` waits on it existing.
