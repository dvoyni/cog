package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/bundles/ecsaudio"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// An Emitter added produces exactly one Play, and an unchanged Emitter on the
// next tick produces no second one. Everything else in this file is a variation
// on that sentence, and the live view is how it is read: a second Play would be
// a second live Voice.
func TestAnEmitterAddedPlaysOnceAndAnUnchangedOneNeverPlaysAgain(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()

	got := h.probe(e)
	if !got.Held {
		t.Fatal("the binding holds no entry for an Entity carrying an Emitter")
	}
	if !got.Found || got.Live != 1 {
		t.Fatalf("after one tick the view holds %d Voices and found=%v, want one Voice", got.Live, got.Found)
	}
	voice := got.Entry.voice

	h.ticks(8)
	again := h.probe(e)
	if again.Live != 1 {
		t.Fatalf("an unchanged Emitter reached %d live Voices over nine ticks, want one", again.Live)
	}
	if again.Entry.voice != voice {
		t.Fatalf("an unchanged Emitter was re-played: %v became %v", voice, again.Entry.voice)
	}
	h.noEnding()
	h.noErrors()
}

// The single most important test in this package. A one-shot that finished
// leaves an entry holding a dead handle, and the binding does not re-play it -
// were the rule instead "an Emitter with no live Voice gets a Play", every
// one-shot in the game would restart forever, because a finished Voice is
// exactly a Voice that is no longer live.
//
// It is read two ways, because the failure is one line and both readings must
// hold: sound holds no Voice on any subsequent tick, and nothing ends a second
// time. The entry is still there, which is what makes the first reading a
// statement about policy rather than about an Entity that quietly vanished.
func TestAFinishedOneShotNeverRestarts(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.ticks(endsOnTick)

	if ended := h.waitEnded(); ended.Reason != sound.ReasonFinished {
		t.Fatalf("the one-shot ended as %v, want finished", ended.Reason)
	}
	got := h.probe(e)
	if got.Live != 0 || got.Found {
		t.Fatalf("after tick %d the view still holds %d Voices", endsOnTick, got.Live)
	}
	if !got.Held {
		t.Fatal("the entry did not outlive its Voice, which is what stops the re-play")
	}
	finished := got.Entry.voice

	// Long enough that a re-play would itself have finished and been heard to
	// end, so a restart cannot hide inside the window.
	h.ticks(endsOnTick + 4)

	after := h.probe(e)
	if after.Live != 0 {
		t.Fatalf("a finished one-shot restarted: %d live Voices %d ticks later", after.Live, endsOnTick+4)
	}
	if after.Entry.voice != finished {
		t.Fatalf("a finished one-shot restarted: the entry moved from %v to %v", finished, after.Entry.voice)
	}
	h.noEnding()
	h.noErrors()
}

// A stolen Voice is the same rule reached by a different route: sound can end a
// Voice under the cap with nothing having despawned, so a handle goes stale on
// its own. The binding treats that exactly as it treats a finished one-shot -
// the entry stays, the handle is dead, nothing restarts - and a game that wants
// a stolen ambience back re-adds the Component.
//
// The cap is one Voice and both Emitters play the same Clip with the same
// Params, so they tie on priority and on audibility exactly. Issue 480 breaks
// that tie by age, oldest first, so the victim is the *first* Emitter's Voice
// and the incoming play takes the slot. Which one loses is the cap's business;
// what this test pins is that the loser's entry stays, its handle is dead, and
// the binding never re-plays it.
func TestAStolenVoiceNeverRestarts(t *testing.T) {
	h := newHarnessCapped(t, 1)

	first := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()
	if got := h.probe(first); got.Live != 1 {
		t.Fatalf("the first Emitter left %d live Voices, want one", got.Live)
	}

	h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()

	if ended := h.waitEnded(); ended.Reason != sound.ReasonStolen {
		t.Fatalf("the Voice that lost the cap ended as %v, want stolen", ended.Reason)
	}
	got := h.probe(first)
	if got.Found {
		t.Fatalf("the stolen Voice is still in the live view as %v", got.Entry.voice)
	}
	if !got.Held {
		t.Fatal("the entry of a stolen Voice was dropped, so the next tick would re-play it")
	}
	stolen := got.Entry.voice

	h.ticks(8)

	after := h.probe(first)
	if after.Entry.voice != stolen {
		t.Fatalf("a stolen Voice restarted: the entry moved from %v to %v", stolen, after.Entry.voice)
	}
	if after.Live != 1 {
		t.Fatalf("a stolen Voice restarted: %d live Voices, want only the one that took the slot", after.Live)
	}
	h.noEnding()
	h.noErrors()
}

// An Emitter's Params.Priority is the band sound's stealing ranks on, and it
// reaches the Voice unchanged - which is the whole of "keep my music playing":
// music one band up is never stolen by effects at the default 0, however many
// of them arrive.
//
// The cap is two. The music takes one slot; then a crowd of effects fills the
// other and overflows it, on two ticks, so the effects steal one another and
// the plays that find only the music left lose outright. Were the binding ever
// to drop the field or override it, the music would sit in the effects' band as
// the oldest Voice there, and the second effect of the first crowd would steal
// it.
func TestAnEmittersPriorityKeepsItsVoiceFromEffectsABandBelow(t *testing.T) {
	h := newHarnessCapped(t, 2)

	music := h.emit(sound.ClipWithResource(clip), sound.Params{Loop: m.Some(true), Priority: m.Some(1)})
	h.tick()
	voice := h.voiceOf(music)
	if got, found := h.info(voice); !found {
		t.Fatal("the music did not start")
	} else if got.Params.Priority.Or(0) != 1 {
		t.Fatalf("the music's Voice plays at priority %d, want the Emitter's 1", got.Params.Priority.Or(0))
	}

	const crowd = 4
	for wave := range 2 {
		for range crowd {
			h.emit(sound.ClipWithResource(clip), sound.Params{})
		}
		h.tick()
		// The first wave finds a free slot and loses the other three plays;
		// the second steals the one effect holding it and loses the rest.
		lost := crowd - 1
		if wave == 1 {
			lost = crowd
		}
		for range lost {
			if ended := h.waitEnded(); ended.Voice == voice {
				t.Fatalf("the music ended as %v under effects a band below it", ended.Reason)
			} else if ended.Reason != sound.ReasonStolen {
				t.Fatalf("an effect ended as %v, want stolen", ended.Reason)
			}
		}
		h.noEnding()
	}

	got := h.probe(music)
	if !got.Found {
		t.Fatal("the music is not in the live view after the effects overflowed the cap")
	}
	if got.Live != 2 {
		t.Fatalf("%d live Voices, want the cap of two", got.Live)
	}
	h.noErrors()
}

// A changed Clip is a Stop and a fresh Play, because there is no field of Params
// that says which Clip. It is detected by ClipRef.Equal rather than ==, since a
// ClipRef may hold a Blob and so is not comparable.
func TestAChangedClipStopsTheVoiceAndPlaysTheNewOne(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()
	first := h.voiceOf(e)

	h.change(changeRequest{Entity: e, Emitter: &ecsaudio.Emitter{Clip: sound.ClipWithResource(otherClip)}})
	h.tick()

	if ended := h.waitEnded(); ended.Voice != first || ended.Reason != sound.ReasonStopped {
		t.Fatalf("the old Voice ended as %v/%v, want %v/stopped", ended.Voice, ended.Reason, first)
	}
	got := h.probe(e)
	if got.Entry.voice == first {
		t.Fatal("a changed Clip left the Entity on its old Voice")
	}
	if !got.Found || got.Live != 1 {
		t.Fatalf("after the change the view holds %d Voices and found=%v, want one Voice", got.Live, got.Found)
	}
	if !got.Entry.clip.Equal(sound.ClipWithResource(otherClip)) {
		t.Fatal("the entry still names the Clip it was playing before the change")
	}
	h.noErrors()
}

// Bytes are the other kind of ClipRef, and the reason the comparison is Equal
// rather than ==: a ClipRef holding a Blob is not comparable, so an Emitter
// naming one must still be recognised as unchanged from tick to tick.
func TestAnEmitterNamedByBytesIsUnchangedFromTickToTick(t *testing.T) {
	h := newHarness(t)

	ref := sound.ClipWithBytes(h.oggBytes())
	e := h.emit(ref, sound.Params{})
	h.tick()
	voice := h.voiceOf(e)

	h.ticks(8)

	got := h.probe(e)
	if got.Entry.voice != voice || got.Live != 1 {
		t.Fatalf("a Clip named by bytes was re-played: %v became %v with %d live Voices",
			voice, got.Entry.voice, got.Live)
	}
	h.noEnding()
	h.noErrors()
}

// A despawn ends the Voice with ReasonStopped. The Entity stops matching the
// query, so it is the same row of the table as an Emitter that was removed -
// no ecs Hook, no Reference to resolve, and no reach-back from the Voice.
func TestADespawnEndsTheVoiceAsStopped(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()
	voice := h.voiceOf(e)

	h.despawn(e)
	h.tick()

	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonStopped {
		t.Fatalf("a despawn ended %v as %v, want %v/stopped", ended.Voice, ended.Reason, voice)
	}
	got := h.probe(e)
	if got.Live != 0 || got.Entries != 0 {
		t.Fatalf("after the despawn the view holds %d Voices and the table %d entries", got.Live, got.Entries)
	}
	h.noErrors()
}

// Removing the Emitter is the same row again, and it is the half of it a game
// reaches without retiring an Entity: the sound stops and the Entity lives on.
// Re-adding the Component is also the sanctioned way to re-trigger a one-shot,
// so the second half of this test is that recipe working.
func TestRemovingAnEmitterStopsItsVoiceAndReAddingOneStartsAgain(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()
	voice := h.voiceOf(e)

	h.change(changeRequest{Entity: e, RemoveEmitter: true})
	h.tick()

	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonStopped {
		t.Fatalf("removing the Emitter ended %v as %v, want %v/stopped", ended.Voice, ended.Reason, voice)
	}
	if got := h.probe(e); got.Held || got.Live != 0 {
		t.Fatalf("the entry survived the Emitter: held=%v, %d live Voices", got.Held, got.Live)
	}

	h.change(changeRequest{Entity: e, Emitter: &ecsaudio.Emitter{Clip: sound.ClipWithResource(clip)}})
	h.tick()

	got := h.probe(e)
	if !got.Found || got.Live != 1 {
		t.Fatalf("remove-then-re-add left %d live Voices and found=%v, want one Voice", got.Live, got.Found)
	}
	if got.Entry.voice == voice {
		t.Fatal("remove-then-re-add handed back the handle of the Voice it stopped")
	}
	h.noErrors()
}

// A Transform is where an Entity is heard from, and the binding copies its
// Position across on every tick a positional Voice is running - which is what
// makes a moving Entity a moving sound without the game saying anything.
func TestATransformMakesTheVoicePositionalAndKeepsMovingIt(t *testing.T) {
	h := newHarness(t)

	place := m.Transform{Position: m.Vec3{X: 3}}
	e := h.spawn(spawnRequest{
		Emitter: &ecsaudio.Emitter{Clip: sound.ClipWithResource(clip)},
		Place:   &place,
	})
	h.tick()

	got := h.probe(e)
	if !got.Found {
		t.Fatal("a placed Emitter started no Voice")
	}
	if position, ok := got.Info.Params.Position.Get(); !ok || position.X != 3 {
		t.Fatalf("the Voice's position is %v (set=%v), want the Transform's (3,0,0)", position, ok)
	}
	if !got.Entry.positional {
		t.Fatal("the entry does not record that its Play carried a Position")
	}

	h.change(changeRequest{Entity: e, Place: &m.Transform{Position: m.Vec3{X: 9, Y: 4}}})
	h.tick()

	moved := h.probe(e)
	if position, _ := moved.Info.Params.Position.Get(); position != (m.Vec3{X: 9, Y: 4}) {
		t.Fatalf("the Voice stayed at %v after its Transform moved", position)
	}
	if moved.Entry.voice != got.Entry.voice {
		t.Fatal("moving a Transform re-played the Voice")
	}
	h.noErrors()
}

// An Emitter with no Transform is a non-positional Voice - background music, a
// UI click - which is what "heard from nowhere in particular" already describes.
func TestAnEmitterWithNoTransformIsNonPositional(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{})
	h.tick()

	got := h.probe(e)
	if !got.Found {
		t.Fatal("an unplaced Emitter started no Voice")
	}
	if _, ok := got.Info.Params.Position.Get(); ok {
		t.Fatal("an Emitter with no Transform was played with a position")
	}
	if got.Entry.positional {
		t.Fatal("the entry calls an unplaced Voice positional")
	}
	h.noErrors()
}

// Two consequences of sound's one-way positional rule, neither guessable, and
// they are one test because they are one sentence read from both ends: the
// shape of a Voice is fixed at the Play, and a Transform afterwards can move a
// positional Voice but cannot make one.
func TestATransformAddedLaterDoesNotSpatializeAPlayingVoiceAndOneRemovedDoesNotUnspatializeIt(t *testing.T) {
	h := newHarness(t)

	unplaced := h.emit(sound.ClipWithResource(clip), sound.Params{})
	placed := h.spawn(spawnRequest{
		Emitter: &ecsaudio.Emitter{Clip: sound.ClipWithResource(clip)},
		Place:   &m.Transform{Position: m.Vec3{X: 5}},
	})
	h.tick()

	h.change(changeRequest{Entity: unplaced, Place: &m.Transform{Position: m.Vec3{X: 7}}})
	h.change(changeRequest{Entity: placed, RemovePlace: true})
	h.tick()

	if got := h.probe(unplaced); got.Info.Params.Position.Present() {
		position, _ := got.Info.Params.Position.Get()
		t.Fatalf("a Transform added to a playing Voice spatialized it at %v", position)
	}
	stillThere := h.probe(placed)
	position, ok := stillThere.Info.Params.Position.Get()
	if !ok || position != (m.Vec3{X: 5}) {
		t.Fatalf("removing a Transform left the Voice at %v (set=%v), want it stopped at (5,0,0)", position, ok)
	}
	h.noErrors()
}

// A Positional Voice with no Orientation is equally loud in every direction
// whatever its Cone says, so absent is not the identity and the binding sends
// the Transform's Rotation only when the author asked for a cone. A binding
// that sent every Transform's Rotation would make every source in the game
// directional along an axis nobody chose.
func TestTheTransformsRotationIsSentOnlyForAnEmitterWithACone(t *testing.T) {
	h := newHarness(t)

	turned := m.Transform{Position: m.Vec3{X: 2}, Rotation: m.QuatRotationZ(1)}
	plain := h.spawn(spawnRequest{
		Emitter: &ecsaudio.Emitter{Clip: sound.ClipWithResource(clip)},
		Place:   &turned,
	})
	coned := h.spawn(spawnRequest{
		Emitter: &ecsaudio.Emitter{
			Clip:   sound.ClipWithResource(clip),
			Params: sound.Params{Cone: m.Some(sound.Cone{Inner: 30, Outer: 90})},
		},
		Place: &turned,
	})
	h.tick()

	if got := h.probe(plain); got.Info.Params.Orientation.Present() {
		t.Fatal("an Emitter with no Cone was made directional by its Transform's Rotation")
	}
	got := h.probe(coned)
	rotation, ok := got.Info.Params.Orientation.Get()
	if !ok || rotation != turned.Rotation {
		t.Fatalf("an Emitter with a Cone faces %v (set=%v), want its Transform's %v",
			rotation, ok, turned.Rotation)
	}
	h.noErrors()
}

// The Listener is copied across and never read: sound has no camera dependency,
// and neither does this. Both halves of an m.Transform go, because Scale is the
// only field of one that means nothing to audio.
func TestTheListenerIsTheTransformOfTheEntityCarryingTheTag(t *testing.T) {
	h := newHarness(t)

	rotation := m.QuatRotationX(-1.5707963)
	h.spawn(spawnRequest{
		Listener: true,
		Place:    &m.Transform{Position: m.Vec3{X: 1, Y: 2, Z: 3}, Rotation: rotation},
	})
	h.tick()

	got := h.listener()
	if position, _ := got.Position.Get(); position != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("the Listener stands at %v, want the Tag's Transform", position)
	}
	if orientation, _ := got.Orientation.Get(); orientation != rotation {
		t.Fatalf("the Listener faces %v, want the Tag's %v", orientation, rotation)
	}
	h.noErrors()
}

// No Entity carrying the Tag writes nothing, leaving the Listener where it was.
// Resetting to the origin would swing every positional sound in the world the
// instant a listener Entity is despawned mid-level, which is the one moment a
// game can least afford it - so the despawn is the case this test is really
// about, and the ticks before it are what makes "where it was" mean something.
func TestNoListenerLeavesTheListenerWhereItWas(t *testing.T) {
	h := newHarness(t)

	standing := m.Vec3{X: 40, Y: 8}
	e := h.spawn(spawnRequest{Listener: true, Place: &m.Transform{Position: standing}})
	h.tick()

	h.despawn(e)
	h.ticks(4)

	if position, _ := h.listener().Position.Get(); position != standing {
		t.Fatalf("the Listener moved to %v when its Entity despawned, want it left at %v", position, standing)
	}
	h.noErrors()
}

// A Listener Tag on an Entity with no Transform is ignored, and counts as none:
// there is nothing to copy, so there is nothing to report either.
func TestAListenerWithNoTransformIsIgnoredAndCountsAsNone(t *testing.T) {
	h := newHarness(t)

	placed := m.Vec3{X: 12}
	h.spawn(spawnRequest{Listener: true, Place: &m.Transform{Position: placed}})
	h.spawn(spawnRequest{Listener: true})
	h.ticks(2)

	if position, _ := h.listener().Position.Get(); position != placed {
		t.Fatalf("the Listener stands at %v, want the one Entity that has a Transform at %v", position, placed)
	}
	h.noErrors()
}

// Two or more report once and the lowest Entity is heard from, on every tick
// and not just the first. An arbitrary pick that changed with iteration order
// would be a sound bug nobody could reproduce, and a report at the frame rate
// would be noise.
func TestTwoListenersReportOnceAndTakeTheLowestEntityEveryTick(t *testing.T) {
	h := newHarness(t)

	lowest := m.Vec3{X: 1}
	first := h.spawn(spawnRequest{Listener: true, Place: &m.Transform{Position: lowest}})
	second := h.spawn(spawnRequest{Listener: true, Place: &m.Transform{Position: m.Vec3{X: 100}}})
	if second < first {
		t.Fatalf("the fixture assumes the second spawn is the higher handle: %v then %v", first, second)
	}

	for tick := range 5 {
		h.tick()
		if position, _ := h.listener().Position.Get(); position != lowest {
			t.Fatalf("on tick %d the Listener stands at %v, want the lowest Entity's %v", tick+1, position, lowest)
		}
	}

	errs := h.errs.snapshot()
	if len(errs) != 1 {
		t.Fatalf("five ticks with two Listeners reported %d errors, want exactly one", len(errs))
	}
	var many ecsaudio.ErrManyListeners
	if !errors.As(errs[0], &many) || many.Count != 2 {
		t.Fatalf("reported %v, want ErrManyListeners of 2", errs[0])
	}
}

// A looping sound needs no separate concept: it is an Emitter whose Params
// loop, and the same table entry tracks it. The proof is that it is still
// playing long after the one-shot of the same Clip would have finished, and
// that nothing re-played it in the meantime.
func TestALoopingEmitterIsAnEmitterWhoseParamsLoop(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{Loop: m.Some(true)})
	h.tick()
	voice := h.voiceOf(e)

	h.ticks(endsOnTick + 8)

	got := h.probe(e)
	if !got.Found || got.Live != 1 {
		t.Fatalf("a looping Emitter left %d live Voices and found=%v, want one", got.Live, got.Found)
	}
	if got.Entry.voice != voice {
		t.Fatalf("a looping Emitter was re-played: %v became %v", voice, got.Entry.voice)
	}
	h.noEnding()
	h.noErrors()
}

// An unchanged Emitter costs a SetVoice that says nothing, which is what makes
// restating the whole Component every tick free: an absent Maybe field means
// unchanged, so what a game did say - here a Volume - survives every tick that
// did not mention it, and what it changes takes effect without a re-play.
func TestChangingParamsRestatesTheVoiceRatherThanReplayingIt(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(clip), sound.Params{Volume: m.Some[float32](0.25)})
	h.tick()
	voice := h.voiceOf(e)

	h.ticks(4)
	if volume, ok := h.probe(e).Info.Params.Volume.Get(); !ok || volume != 0.25 {
		t.Fatalf("the Voice's volume is %v (set=%v) four ticks on, want the 0.25 it was played with", volume, ok)
	}

	h.change(changeRequest{Entity: e, Emitter: &ecsaudio.Emitter{
		Clip:   sound.ClipWithResource(clip),
		Params: sound.Params{Volume: m.Some[float32](0.5)},
	}})
	h.tick()

	got := h.probe(e)
	if got.Entry.voice != voice {
		t.Fatalf("changing a parameter re-played the Voice: %v became %v", voice, got.Entry.voice)
	}
	if volume, _ := got.Info.Params.Volume.Get(); volume != 0.5 {
		t.Fatalf("the Voice's volume is %v after the change, want 0.5", volume)
	}
	h.noEnding()
	h.noErrors()
}

// A Clip that fails is sound's own path, and what it costs the binding is that
// the entry outlives that ending too - so a Clip that is not there is played
// once and reported once, rather than retried at the frame rate for the life of
// the game.
func TestAFailedClipIsNotRetriedEveryTick(t *testing.T) {
	h := newHarness(t)

	e := h.emit(sound.ClipWithResource(missingClip), sound.Params{})
	h.ticks(2)

	if ended := h.waitEnded(); ended.Reason != sound.ReasonFailed {
		t.Fatalf("a missing Clip ended as %v, want failed", ended.Reason)
	}
	voice := h.probe(e).Entry.voice

	h.ticks(8)

	got := h.probe(e)
	if got.Live != 0 {
		t.Fatalf("a failed Clip was retried: %d live Voices", got.Live)
	}
	if got.Entry.voice != voice {
		t.Fatalf("a failed Clip was retried: the entry moved from %v to %v", voice, got.Entry.voice)
	}
	h.noEnding()
	if errs := h.errs.snapshot(); len(errs) != 1 {
		t.Fatalf("a missing Clip reported %d errors over ten ticks, want exactly one", len(errs))
	}
}
