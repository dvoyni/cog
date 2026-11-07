# Metronome

Metronome paces the work a kernel does over time: it publishes the beats of the tracks an application declares. It knows nothing of rendering, windows or the ECS. This glossary records its domain language.

## Language

**Track**:
A declared, independently paced source of beats, each published as one event type the application chooses.
_Avoid_: Loop, timer, ticker, clock

**Beat**:
One publication of a track's event, stamped with the track's timing.
_Avoid_: Frame, step, pulse

**Tick**:
A beat's number on its own track, counted from one.
_Avoid_: Frame number, index

**Fixed-step track**:
A track whose beats each stand for the same span of time, caught up after a delay.

**Variable track**:
A track whose beats each stand for the time since the previous beat.

**Clock**:
What metronome reads time from and waits on, deciding when each deadline is reached.
_Avoid_: Time provider, time source

**Stopwatch**:
A named measure of the time since it was started, by the clock that made it, through which a plugin other than metronome reads time.
_Avoid_: Timer, clock

**Epoch**:
The moment a clock reads as zero, from which every track measures its time.
_Avoid_: Start time, origin

**Debt**:
The steps a fixed-step track owes but has not yet published, capped so that the excess is dropped.
_Avoid_: Backlog, accumulator, pending

**Lost time**:
Time a track threw away just before a beat, by dropping owed steps or clamping a variable track's delta.
_Avoid_: Dropped steps, skipped time

**Reference track**:
The fixed-step track another track interpolates against.

**Interpolated tick**:
The last tick of the reference track published before a beat, whose state that beat's work reads.

**Settled**:
A track with no beat in flight and no debt.
_Avoid_: Idle, quiescent

**In flight**:
A beat whose publication's out dependency has not finished.
