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

**Nothing of this is implemented**, because `sound` is not.

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

Everything, since `sound` does not exist. What this capability specifically
obliges the Slot to hold:

1. **The endings ring** — a fixed 32-entry ring of `{clip, reason, tick}`, sized
   once at startup, written on every ending beside the `VoiceEndedEvent`. No
   game-facing type.
2. **`azimuth` and `elevation` retained per Positional Voice** for the last
   flush. They are already computed on the way to the gain matrix; what is
   required is that they are not thrown away.
3. **`audibility` retained as the pre-pan scalar**, which stealing needs anyway.
4. **The flush's `tick` retained**, so the listing can name the moment it
   describes.
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
