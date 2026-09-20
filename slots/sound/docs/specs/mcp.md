# sound as an mcp provider — specification

`sound` offers an agent one capability: **`sound_voices`** — what is playing,
what just ended, and whether anything could have been heard at all.

It answers one question no other capability can: *did the game make a noise?* A
game that signals with sound — an alarm, a low-health heartbeat, a voice line
gating a cutscene — is partly invisible to a driver that cannot hear, and
`gfx_capture` returns a frame in which nothing changed, so the agent concludes
the game is stuck when it is in fact waiting for a sound to finish.

The extension point is
[bundles/mcp/docs/specs/mcp.md](../../../../bundles/mcp/docs/specs/mcp.md), and
the machine it reports on is
[slots/sound/docs/specs/sound.md](sound.md). Assembled from
[The agent-facing capability: what an agent that cannot hear should ask](https://github.com/dvoyni/cog/issues/305),
as amended by [the prototype](https://github.com/dvoyni/cog/issues/304),
[how a game tests its sound](https://github.com/dvoyni/cog/issues/467) and
[where a 2D game's listener stands](https://github.com/dvoyni/cog/issues/470);
every section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it.

**This is implemented.** The provider is at
`slots/sound/internal/mcpprovider.go`, the endings ring at
`slots/sound/internal/lastflush.go`.

> **Implemented by [#487](https://github.com/dvoyni/cog/issues/487).** It is a
> rendering of a thing that already existed, so most of what follows is
> unchanged. Where the code and this document differ, the differences are here.
>
> - **The capability is an `mcp.Func` over a command `sound` keeps to itself.**
>   `voicesCmd`, its request and its response are declared in
>   `slots/sound/internal/mcpprovider.go` and not in the root. An `mcp.Command`
>   with zero glue — `input_state`'s shape, and the cheaper one — is rejected
>   because a command is named by the root that declares it, so its response
>   would be a game-facing type carrying the endings ring. The ring's whole
>   justification is that it has exactly one reader, and a type a game can name
>   is a second one.
> - **The ring and the flush's `tick` are one resource**, the unexported
>   `lastFlush`. Requirements 1 and 4 below are written by one pass and read by
>   one reader, so they are locked together rather than separately. The type is
>   unexported in a package nothing outside `slots/sound` may import, which is
>   "no `kernel.Read`-able counterpart and no game-facing type" spelled in the
>   type system rather than promised in prose.
> - **`Ending` gained a `Clip`.** The slot is cleared at the end of the flush and
>   the endings are read after that, so an ending that did not carry its Clip
>   could never be told which sound it was about. `VoiceEndedEvent` is unchanged:
>   a game holds the handle it played with and already knows.
> - **`azimuth` and `elevation` are reached through `VoiceDetail`**, a type in
>   `internal/types` that `slots/sound` aliases nowhere, yielded by the friend
>   function `VoicesDetails`. They were already retained on `voiceSlot`; what
>   #487 added is a way to read them that is not a field on `VoiceInfo`, which
>   leaves [#467](https://github.com/dvoyni/cog/issues/467) §3 intact rather
>   than reopening it — the pan is still off the view a game's test asserts
>   against. Rejected: a per-handle lookup beside `All()`, which is a second walk
>   of the table for a fact the first walk already had in hand.
> - **`Voices.Cap()` exists**, beside `Len()`. Threading the plugin's own
>   `maxVoices` into the provider is rejected: the count and the cap have to come
>   from one read lock, or a listing can report 65 of 64.
> - **`voicesInUse` and `maxVoices` are top-level fields**, not `device`'s. The
>   description prose writes `device.ready` with its block and those two without,
>   and the cap is `sound`'s rather than the device's.
> - **`azimuth`, `elevation` and `distance` are `m.Maybe` under `omitzero`**, so
>   a non-positional Voice omits them and a Voice dead ahead still reports `0`. A
>   bare `float32` under `omitempty` would have made *heard centred* and *dead
>   ahead* the same JSON.
> - **The wire spellings.** `clip` is the storage path, or `blob (N bytes)` for a
>   Clip named by bytes. `bus` is the integer a game numbers its own Buses with,
>   `0` being Master, because `sound` has no Bus names to report. `latencyMs` is
>   milliseconds, because a `time.Duration` marshals as nanoseconds. `endings` is
>   oldest first — a reader asking whether the alarm sounded before the door
>   opened reads them in the order the game played them.
> - **No Voice handle is reported.** The field table below does not name one, and
>   there is nothing an agent can do with a handle: it cannot stop, seek or
>   replay a Voice. Correlating one poll's listing with the next one's is left
>   for whatever asks for it.
> - **A dispatch that does not happen is refused rather than reported as
>   silence.** A stopped scheduler answers with the zero response, whose
>   `maxVoices` is `0` — impossible in a composed engine — and the body turns
>   that into *the game is shutting down* rather than into a game playing
>   nothing.

---

## Contents

- [Vocabulary](#vocabulary) · [The provider](#the-provider)
- [Why it is not a Snapshot](#why-it-is-not-a-snapshot) · [Which moment it describes](#which-moment-it-describes)
- [The response](#the-response) · [The endings ring](#the-endings-ring)
- [The description prose](#the-description-prose)
- [Required sound changes](#required-sound-changes) · [Out of scope](#out-of-scope)

---

## Vocabulary

Every term is `sound`'s and is in `CONTEXT.md` under **Audio** — **Voice**,
**Clip**, **Bus**, **Listener**, **Positional Voice**, **Stealing**, **Device**.

**This capability coins nothing.** A listing of what is playing is a sentence,
not a concept, and `CONTEXT.md` gains nothing from it — deliberately, and said
here so that the next reader does not reach for a word that is not there.

Two words it does *not* use: **Snapshot**, for the reason below, and **audio
thread**, because nothing this capability reports comes from one.

---

## The provider

`sound` hosts its own provider from its own `Register`, as every package does
(`bundles/canvas/internal/mcpprovider.go:22-40`), and the prompt text is
reproduced in [The description prose](#the-description-prose) so that it is
reviewed as prompt text rather than buried as a string literal.

```go
func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(voicesName, voicesDescription, voices, mcp.ReadOnly()),
	}
}
```

**One capability, not several.** The three blocks below are one answer to one
question, and splitting them would make an agent take three calls to learn that
the reason it heard nothing is that there is no Device.

**`ReadOnly`**, which in cog's reading means the capability does not change the
game. Here that is true without qualification: it reads retained state and costs
no tick.

**It is built rather than ruled out**, which
[#305](https://github.com/dvoyni/cog/issues/305)'s own scope note demanded be
argued rather than assumed. It earns its place on the case above, and it is
nearly free: [the Voice](https://github.com/dvoyni/cog/issues/301) already
granted the live Voice view for the game's own sake, and
[the Device's lifecycle](https://github.com/dvoyni/cog/issues/299) already ruled
it Adapter-independent. This capability is a **rendering of a thing that
exists**, not a mechanism.

**It answers identically under `nosound`, `otosound` and `jssound`**, which is
what makes it usable in the headless engine an agent actually drives. An agent on
`otosound` cannot hear the game any better than one on `nosound`, so putting the
view in the silent Adapter would have given the answer only to the configuration
that least needs it.

---

## Why it is not a Snapshot

A **Snapshot** is one tick's *recorded declarations*, rendered while they are
still alive, carrying a `Tick`. `canvas_draws` and `ui_layout` are Snapshots
because a frame is recorded and thrown away.

**Retained Voices are not recorded per tick.** They were recorded once, possibly
minutes ago, and have been alive since. Calling this listing a Snapshot would
make the word mean two incompatible things in one glossary, so it does not
stretch — **and nothing replaces it.** `sound_voices` is a listing of what is
playing.

The same reasoning is why the live view is a resource in `sound` rather than a
command: a Snapshot needs a command because there is nothing to read between
ticks; a Voice is already sitting still.

---

## Which moment it describes

**It costs no tick, and it reports one.**

`sound` holds the Voices and computes their playheads at its flush, so the
listing describes **the last flush**, and it reports that flush's `tick`. The
Device thread is not on the tick clock at all, and nothing here is read from it.

Unlike `canvas_draws`, which must step a paused engine because it has nothing to
record otherwise, retained Voices are already there to read. That is
`gfx_capture`'s position, and it inherits `gfx_capture`'s advice: **when pairing
observations under `app_time hold`, take this one last**, because it costs no
tick and so shows whatever the step produced.

**The playhead is tick-accurate, not sample-accurate.** An agent told a Voice is
1.2 s in must not conclude anything finer than a tick.

---

## The response

**Three blocks, because *nothing is playing* and *there is no device* are
different answers** and an agent that confuses them will chase the wrong bug for
a long time.

### Voices

One entry per live Voice:

| field | |
|---|---|
| `clip` | the path, or `blob` with its byte length |
| `bus` | the Bus it plays on |
| `playhead`, `duration` | seconds, tick-accurate |
| `looping`, `paused` | |
| **`audibility`** | the final computed gain: `volume × bus × falloff × cone` |
| `position`, `distance` | positional Voices only; the distance is from the Listener |
| `azimuth`, `elevation` | positional Voices only; degrees |

**`audibility` is the point of the whole capability.** An agent asking *can the
player hear this* wants the number the engine actually derived, not the raw
volume a game set: a Voice at full volume on a Bus at zero, or two hundred metres
behind a falloff, is playing and inaudible, and only one number says so. It is
the same figure stealing ranks by, so it costs nothing to report.

> **It is the scalar *before* panning.** Reading it off the gain matrix as
> `max(L, R)` scores a centred Voice **0.1414** against a hard-panned one's
> **0.2000** at the same distance — 3 dB invented out of nothing, because
> equal-power panning preserves power and cannot change how loud a Voice is,
> only where it is. An agent would be told a sound beside the player is louder
> than the same sound in front of them.

**`azimuth` and `elevation` are reported although the game-facing live view does
not carry them.** They already exist in the tick's arithmetic — the W3C equations
produce them on the way to the gain, exactly as audibility is produced on the way
to the matrix — so reporting them costs what audibility costs: nothing.

The case that earned them is
[#470](https://github.com/dvoyni/cog/issues/470). A 2D game whose Listener is
left unrotated hard-pans **every** sprite to one ear or the other, while the
arithmetic stays correct, the sounds keep playing, and every other field here
looks entirely normal. An agent reading `azimuth: 90.0` on every Voice at once
sees it immediately; an agent reading positions and a Listener facing has to
re-derive W3C's projection to find it.

This does not reopen [#467](https://github.com/dvoyni/cog/issues/467) §3, which
kept azimuth out of the **game-facing** view. That reasoning was about what a
test asserts on — engine arithmetic pinned once, not re-asserted by every game —
not about what an agent may read, and this capability already reports figures the
view does not.

**They are not a 2D figure.** Azimuth and elevation are reported for every
Positional Voice in every game. A non-positional Voice has neither, like position
and distance beside them.

**No detector in `sound`.** The alternative was a report fired when every
Positional Voice sits at |azimuth| = 90 for a while, and it is refused: it would
fire on a game where every sound genuinely *is* beside the player, which is a
guess about intent rather than an observation.

### The Listener

Its position and facing — because every distance above is relative to it, and an
agent that sees a Voice *12 m away* needs to know from where.

### The Device and the cap

`Ready`, `Name`, `SampleRate` and `Latency`, plus **Voices in use against
`MaxVoices`**.

An agent asking why it heard nothing should be able to find `Ready` false rather
than conclude the game is silent. And the cap matters on its own: an agent
looking at a game at its Voice limit is looking at a game where sounds are being
**stolen**, and without the count it cannot tell that from sounds that were never
played.

---

## The endings ring

An agent's real question is almost never *is it sounding at this exact instant*.
It is **did the alarm sound** — and a 400 ms one-shot is invisible between two
tool calls, however fast the agent is.

So the response carries a fixed ring of the **last 32 endings**: the Clip, the
`Reason` (`ReasonFinished`, `ReasonStopped`, `ReasonStolen`, `ReasonFailed`,
`ReasonReleased`), and the tick it ended on.

- It costs `sound` one fixed-size ring, sized once, allocating nothing.
- It makes `VoiceEndedEvent` readable by something that **cannot subscribe to
  events**, which an MCP client cannot. That is the ring's whole justification.
- It turns *the sound played and finished 200 ms ago* from unanswerable into a
  line of output, and it distinguishes **finished** from **stolen** from
  **failed**, which is three different bugs.

**Thirty-two is a constant, not a knob.** It is two ticks' worth of endings at
the Voice cap, which is more than an agent polling at any reasonable rate will
miss.

**The ring is the agent's alone.** It gets no `kernel.Read`-able counterpart and
no game-facing type: a test *can* subscribe, so a test reads `VoiceEndedEvent`,
which is unbounded and ordered. Handing a game a capped, lossy ring over a
lossless stream it can already consume would be strictly worse for the reader
that has a choice. The ring exists *because* one reader has none.

**Together the two blocks cover every Voice, with nothing falling between them.**
That is `sound`'s closure property read from here:

> Every Voice that ever existed is observable exactly once: it is in the Voices
> block now, or its ending is in the ring.

**There are no *starts* to add**, for either reader. There is no
`VoiceStartedEvent`, because an event exists to tell a game something it did not
do and a `Play` is the game's own instruction — the closure property is what
replaces it.

---

## The description prose

Prompt text, reproduced so that it is reviewed as prompt text.

> List every sound the game is playing right now, plus the last 32 that ended,
> the listener they are heard from, and the audio device. Use it when the game
> looks stuck or unchanged on screen but may be waiting on a sound, when you
> need to know whether an alarm, a voice line or a cue actually played, or when
> you want to check a sound you triggered was audible rather than merely started.
>
> Each playing sound reports its clip, its bus, how far through it is, and
> **audibility** — the gain the engine actually derived, after the bus volume,
> the distance falloff and the cone. A sound with `audibility: 0` is playing and
> cannot be heard, which is a different bug from one that never played. A
> positional sound also reports its position, its distance from the listener and
> its bearing as `azimuth` and `elevation` in degrees; `azimuth: 90` on every
> sound at once means the listener is oriented wrongly, not that everything is
> to the right.
>
> A one-shot is usually over before you can look, so read the endings list: it
> says which clip ended, on which tick, and why — `finished`, `stopped`,
> `stolen` by the voice cap, `failed` to load, or `released`. `voicesInUse`
> against `maxVoices` tells you the game is at its cap and sounds are being
> stolen.
>
> If `device.ready` is false nothing is audible at all — on the web that usually
> means the player has not clicked yet — and the game is still running normally:
> playheads advance and sounds still end on time whether or not anyone can hear
> them.
>
> This costs no tick and never changes the game. When you are pairing
> observations under `app_time hold`, take this one **last**.

Three things it says on purpose:

- **It names the failure `audibility` exists to catch**, because an agent that is
  handed a number without being told what a zero means will read it as a volume.
- **It names the 2D listener failure in the words an agent will see**, since the
  symptom is a value that looks ordinary in isolation.
- **It says playheads advance with no Device**, because otherwise an agent that
  finds `ready: false` will assume the game is frozen rather than merely silent.

---

## Required sound changes

`sound` exists now, and all five are in it. What this capability specifically
obliges the Slot to hold:

1. **The endings ring** — a fixed 32-entry ring of `{clip, reason, tick}`, sized
   once at startup, written on every ending beside the `VoiceEndedEvent`. No
   game-facing type. It is `lastFlush` in `slots/sound/internal`, written in the
   same pass that publishes the events.
2. **`azimuth` and `elevation` retained per Positional Voice** for the last
   flush. They are already computed on the way to the gain matrix; what is
   required is that they are not thrown away. `voiceSlot` already kept them, so
   what #487 added is `VoiceDetail` and the friend that yields it.
3. **`audibility` retained as the pre-pan scalar**, which stealing needs anyway.
   It is on `VoiceInfo` already, and the listing reports that number rather than
   deriving one of its own.
4. **The flush's `tick` retained**, so the listing can name the moment it
   describes. It is a field on `lastFlush`, beside the ring.
5. **The provider** at `slots/sound/internal/mcpprovider.go`, with
   `voicesDescription` as a package constant reproduced above.

---

## Out of scope

- **An agent making sound.** There is no synthetic-audio analogue of synthetic
  input, and this is ruled out with its reason rather than left unbuilt.

  Input is synthesized because an agent must stand in for the **player**, and the
  player is an input. Sound is an **output**. An agent that wants a sound played
  drives the game into playing it, exactly as a player would, and in doing so
  exercises the code path that was actually in question. A `sound_play` tool
  would let an agent inject audio the game never asked for — and then every
  observation it took afterwards would describe a world the game did not produce,
  which is the one thing an observation tool must never do.

  An agent that wants to *control* audio globally is not blocked: a Bus volume is
  an ordinary game verb, and a game that wants an agent to mute it exposes that
  its own way.

- **Filtering the Voices block**, which `canvas_draws` has and which a game at 64
  Voices might want. Additive, and left until something asks.

- **A longer ring.** 32 is sized for an agent, and a test — the other candidate
  reader — does not read it at all.

- **Reporting the gain matrix.** An agent reading four numbers per Voice would be
  re-deriving the pan; `azimuth` is the same fact in the form a reader can act
  on.
