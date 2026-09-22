package internal

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/sound"
	"github.com/dvoyni/cog/slots/sound/internal/types"
)

// tickAt publishes one fixed step numbered the way app numbers its own, which
// is what the endings ring stamps an ending with. The harness's own tick leaves
// the number at zero, and a ring whose entries all said zero would prove
// nothing about when anything ended.
func (h *harness) tickAt(tick int64) {
	h.t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: step, Last: true, Tick: tick}).Wait()
}

// listing is one sound_voices call, taken through the capability body an agent
// reaches rather than through the command it dispatches, so what these tests
// assert is what a client receives.
func listing(t *testing.T, h *harness) voicesResponse {
	t.Helper()
	response, err := voices(h.kernel, voicesRequest{})
	if err != nil {
		t.Fatalf("sound_voices refused: %v", err)
	}
	return response
}

// One capability, and it only looks. The three blocks are one answer to one
// question: split, a reader would need three calls to learn that the reason it
// heard nothing is that there is no Device.
//
// And there is no way here to make a sound. Input is synthesized because the
// player is an input; sound is an output, and a tool that injected audio the
// game never asked for would make every observation taken afterwards describe a
// world the game did not produce.
func TestCapabilities_OneCapabilityThatOnlyLooks(t *testing.T) {
	capabilities := (provider{}).Capabilities()
	if len(capabilities) != 1 {
		t.Fatalf("sound offers %d capabilities, want 1", len(capabilities))
	}
	offered := capabilities[0]
	if err := offered.Err(); err != nil {
		t.Fatalf("capability %q did not construct: %v", offered.Name(), err)
	}
	if offered.Name() != voicesName {
		t.Errorf("sound offers %q, want %q", offered.Name(), voicesName)
	}
	if !offered.ReadOnly() {
		t.Error("the listing reads retained state and costs no tick, so it is read-only")
	}
	if offered.RequestType() != reflect.TypeFor[voicesRequest]() ||
		offered.ResponseType() != reflect.TypeFor[voicesResponse]() {
		t.Errorf("voices is %v -> %v", offered.RequestType(), offered.ResponseType())
	}
}

// The description is prompt text, and what it says on purpose is what a reader
// handed these numbers without them would get wrong: a zero audibility read as
// a volume, an azimuth of 90 everywhere read as sounds that really are beside
// the player, and a device that is not ready read as a frozen game.
func TestTheDescriptionCarriesWhatAReaderGetsWrong(t *testing.T) {
	description := (provider{}).Capabilities()[0].Description()
	for _, wanted := range []string{
		"last 32 that ended",
		"`audibility: 0` is playing and cannot be heard",
		"`azimuth: 90` on every sound at once means the listener is oriented wrongly",
		"`voicesInUse` against `maxVoices`",
		"If `device.ready` is false",
		"playheads advance and sounds still end on time",
		"costs no tick and never changes the game",
		"take this one **last**",
	} {
		if !strings.Contains(description, wanted) {
			t.Errorf("the voices description never says %q", wanted)
		}
	}
}

// Nothing is playing and there is no device are different answers, and a reader
// that confuses them chases the wrong bug for a long time. So they are separate
// blocks that move independently: a silent game with a working device, a game
// playing into a device it has not got, and the playheads advancing through it
// all.
func TestNothingIsPlayingAndThereIsNoDeviceAreDifferentAnswers(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.tickAt(1)
	silent := listing(t, h)
	if len(silent.Voices) != 0 {
		t.Fatalf("a game that played nothing lists %d Voices", len(silent.Voices))
	}
	if !silent.Device.Ready || silent.Device.Name != "fake" {
		t.Fatalf("the device reads %+v, and the fixture opened one", silent.Device)
	}
	if silent.MaxVoices != sound.DefaultMaxVoices || silent.VoicesInUse != 0 {
		t.Fatalf("the cap reads %d of %d", silent.VoicesInUse, silent.MaxVoices)
	}

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	backend.setReady(false)
	h.tickAt(2)
	playing := listing(t, h)
	if len(playing.Voices) != 1 || playing.VoicesInUse != 1 {
		t.Fatalf("one Play lists %d Voices and %d in use", len(playing.Voices), playing.VoicesInUse)
	}
	if playing.Device.Ready {
		t.Fatal("the device went away and the listing still calls it ready")
	}

	// A Device that is not ready stops nothing: the playhead advances whether
	// or not anyone can hear it, which is exactly what the description
	// promises a reader who finds ready false.
	h.tickAt(3)
	later := listing(t, h)
	if later.Voices[0].Playhead <= playing.Voices[0].Playhead {
		t.Fatalf("the playhead sat at %v with no device, and it advances whether anyone hears it or not",
			later.Voices[0].Playhead)
	}
	if later.Tick != 3 {
		t.Fatalf("the listing names tick %d, want the last flush's 3", later.Tick)
	}
}

// audibility is the point of the whole capability, and it is the pre-pan scalar
// the cap steals by rather than anything read off the gain matrix. A source
// orbiting the Listener at a constant radius does not move it at all, while the
// bearing beside it moves the whole way.
//
// Reading it off the matrix as max(L, R) would score a centred Voice 0.1414
// against a hard-panned one's 0.2000 at the same distance: 3 dB invented out of
// nothing, and a reader told a sound beside the player is louder than the same
// sound in front of them.
func TestTheListingReportsTheDerivedGainAndNeverThePan(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	const radius = 5
	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{
		Volume:   m.Some[float32](1),
		Position: m.Some(m.Vec3{Z: -radius}),
	})
	h.tickAt(1)

	first := listing(t, h).Voices[0]
	if first.Audibility <= 0 || first.Audibility >= 1 {
		t.Fatalf("a source at radius %v is audible at %v, and the falloff is doing nothing",
			radius, first.Audibility)
	}

	var hardLeft, hardRight bool
	for degrees := 1; degrees < 360; degrees++ {
		radians := float64(degrees) * math.Pi / 180
		position := m.Vec3{
			X: float32(math.Sin(radians) * radius),
			Z: float32(-math.Cos(radians) * radius),
		}
		h.record(func(queue *sound.Queue) {
			queue.SetVoice(voice, sound.Params{Position: m.Some(position)})
		})
		h.tickAt(int64(degrees) + 1)

		view := listing(t, h).Voices[0]
		if !closeEnough(view.Audibility, first.Audibility) {
			t.Fatalf("%v degrees round the orbit the listing reports audibility %v, want %v everywhere",
				degrees, view.Audibility, first.Audibility)
		}
		if !closeEnough(view.Distance.Or(0), radius) {
			t.Fatalf("%v degrees round the orbit the distance reads %v, want %v",
				degrees, view.Distance.Or(0), float32(radius))
		}
		azimuth := view.Azimuth.Or(0)
		hardLeft = hardLeft || closeEnough(azimuth, -90)
		hardRight = hardRight || closeEnough(azimuth, 90)
	}

	// The assertion is only worth anything if the bearing did move. A quarter
	// of the way round the orbit the source is hard right, and three quarters
	// of the way round it is hard left: the whole of the pan, against an
	// audibility that did not move at all.
	if !hardLeft || !hardRight {
		t.Fatalf("the orbit reached hard left %v and hard right %v, want both: if the pan never "+
			"moved, a constant audibility proves nothing", hardLeft, hardRight)
	}
}

// The failure azimuth is reported to catch: a 2D game whose Listener is left
// unrotated hard-pans every sprite to one ear or the other, while the
// arithmetic stays correct, the sounds keep playing, and every other field
// looks entirely normal.
//
// Panning projects the up axis away before taking a bearing, and an unrotated
// Listener's Z=0 plane contains +Y, so every source with positive X is hard
// right whatever its Y. A reader seeing azimuth 90 on every Voice at once sees
// it immediately; a reader holding positions and a facing has to re-derive
// W3C's projection to find it.
func TestAnUnrotatedListenerShowsAsTheSameBearingOnEveryVoice(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	// Three sprites a 2D game would place all over its screen.
	for _, at := range []m.Vec3{{X: 1}, {X: 3, Y: -50}, {X: 0.5, Y: 200}} {
		h.play(sound.ClipWithResource(bell), 0, sound.Params{Position: m.Some(at)})
	}
	h.tickAt(1)

	response := listing(t, h)
	if len(response.Voices) != 3 {
		t.Fatalf("three Plays list %d Voices", len(response.Voices))
	}
	for i, view := range response.Voices {
		if !closeEnough(view.Azimuth.Or(0), 90) {
			t.Errorf("voice %d, spread across the screen, reads azimuth %v, want the 90 an "+
				"unrotated Listener gives every one of them", i, view.Azimuth.Or(0))
		}
	}
	if forward := response.Listener.Forward; !closeEnough(forward[2], -1) {
		t.Errorf("the Listener faces %v, and an unrotated one faces -Z", forward)
	}
	if up := response.Listener.Up; !closeEnough(up[1], 1) {
		t.Errorf("the Listener's up is %v, and an unrotated one is +Y", up)
	}
}

// A non-positional Voice has no bearing and no distance, as it has no position:
// it is heard centred, which is not a bearing of zero a reader should compare
// against anything. So the three fields are absent rather than zero, and a
// positional Voice that really is dead ahead still reports its zero.
func TestABearingIsAbsentOnANonPositionalVoiceAndPresentAtZero(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.play(sound.ClipWithResource(bell), 0, sound.Params{Volume: m.Some[float32](0.5)})
	h.play(sound.ClipWithResource(bell), 0, sound.Params{Position: m.Some(m.Vec3{Z: -1})})
	h.tickAt(1)

	response := listing(t, h)
	click, ahead := response.Voices[0], response.Voices[1]
	if click.Azimuth.Present() || click.Elevation.Present() || click.Distance.Present() ||
		click.Position != nil {
		t.Fatalf("a non-positional Voice reports a place: %+v", click)
	}
	if !ahead.Azimuth.Present() || !closeEnough(ahead.Azimuth.Or(1), 0) {
		t.Fatalf("a Voice dead ahead reports azimuth %v, want a present 0", ahead.Azimuth)
	}

	// The two must survive JSON as they read here, because omitzero on an
	// absent value and a present zero is exactly where a field quietly
	// vanishes.
	document, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal the listing: %v", err)
	}
	var wire struct {
		Voices []map[string]any `json:"voices"`
	}
	if err := json.Unmarshal(document, &wire); err != nil {
		t.Fatalf("read the listing back: %v", err)
	}
	for _, field := range []string{"azimuth", "elevation", "distance", "position"} {
		if _, carries := wire.Voices[0][field]; carries {
			t.Errorf("a non-positional Voice crosses the wire carrying %q", field)
		}
		if _, carries := wire.Voices[1][field]; !carries {
			t.Errorf("a Voice dead ahead crosses the wire without %q", field)
		}
	}
}

// A reader's real question is almost never "is it sounding at this instant". It
// is "did the alarm sound" - and a one-shot shorter than the gap between two
// calls is invisible to the live view, which reports nothing and means two
// different things by it.
func TestAOneShotThatEndedBetweenTwoLooksIsStillInTheRing(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4 * step, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for tick := int64(1); tick <= 4; tick++ {
		h.tickAt(tick)
	}
	h.waitEnded()

	response := listing(t, h)
	if len(response.Voices) != 0 {
		t.Fatalf("the one-shot is over and the live view still lists %d Voices", len(response.Voices))
	}
	if len(response.Endings) != 1 {
		t.Fatalf("the ring holds %d endings, want the one that just happened", len(response.Endings))
	}
	ended := response.Endings[0]
	if ended.Clip != bell || ended.Reason != "finished" || ended.Tick != 4 {
		t.Fatalf("the ring says %+v, want %s finished on tick 4", ended, bell)
	}
}

// The ring tells finished from stolen, which is two different bugs: a sound
// that played out, and a sound the cap took away. The cap itself is the other
// half of that answer - a game at its cap is a game where sounds are being
// stolen, and without the count it cannot be told from sounds never played.
func TestTheRingAndTheCapTellAStolenSoundFromAFinishedOne(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}.WithMaxVoices(1), clipBytes)

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tickAt(1)
	if at := listing(t, h); at.VoicesInUse != 1 || at.MaxVoices != 1 {
		t.Fatalf("one Voice on a cap of one reads %d of %d", at.VoicesInUse, at.MaxVoices)
	}

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tickAt(2)
	h.waitEnded()

	response := listing(t, h)
	if response.VoicesInUse != 1 || response.MaxVoices != 1 {
		t.Fatalf("a steal left %d of %d in use, and a steal nets to zero",
			response.VoicesInUse, response.MaxVoices)
	}
	if len(response.Endings) != 1 || response.Endings[0].Reason != "stolen" {
		t.Fatalf("the ring says %+v, want one stolen ending", response.Endings)
	}
	if response.Endings[0].Tick != 2 {
		t.Fatalf("the steal is stamped tick %d, want 2", response.Endings[0].Tick)
	}
}

// Thirty-two is a constant, not a knob: two ticks' worth of endings at the
// Voice cap. It holds the newest thirty-two and loses the oldest, in the order
// they happened, so a reader reads a sequence rather than a set.
func TestTheRingHoldsTheLastThirtyTwoEndingsAndLosesTheOldest(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	const played = 40
	var all []sound.Voice
	h.record(func(queue *sound.Queue) {
		for range played {
			all = append(all, queue.Play(sound.ClipWithResource(bell), 0, sound.Params{}))
		}
	})
	h.tickAt(1)

	// One ending per tick, so the tick a stop is stamped with is what
	// identifies it: ending i ends on tick i+1.
	for i, voice := range all {
		h.record(func(queue *sound.Queue) { queue.Stop(voice) })
		h.tickAt(int64(i) + 2)
		h.waitEnded()
	}

	response := listing(t, h)
	if len(response.Endings) != endingsHeld {
		t.Fatalf("the ring holds %d endings after %d, want %d", len(response.Endings), played, endingsHeld)
	}
	// The forty endings are stamped ticks 2 through 41, and the last
	// thirty-two of them are 10 through 41, oldest first.
	for i, ended := range response.Endings {
		want := int64(played + 2 - endingsHeld + i)
		if ended.Tick != want {
			t.Fatalf("ending %d is stamped tick %d, want %d: the ring is out of order or kept the "+
				"wrong end", i, ended.Tick, want)
		}
	}
}

// The ring is sized once and allocates nothing. It is a fixed array inside the
// resource, and an ending overwrites an entry in place rather than appending to
// anything - which is what lets the flush write one on a tick that otherwise
// allocates nothing at all.
func TestTheRingAllocatesNothing(t *testing.T) {
	record := &lastFlush{}
	ended := sound.ClipWithResource(bell)
	allocations := testing.AllocsPerRun(1000, func() {
		record.record(types.Ending{Reason: sound.ReasonFinished, Clip: ended}, 7)
	})
	if allocations != 0 && !raceEnabled {
		t.Fatalf("recording an ending allocated %v times, want 0", allocations)
	}
	if record.held != endingsHeld {
		t.Fatalf("after a thousand endings the ring holds %d, want %d", record.held, endingsHeld)
	}
	var visited int
	record.each(func(e ending) {
		visited++
		if e.tick != 7 || e.reason != sound.ReasonFinished {
			t.Fatalf("the ring holds %+v", e)
		}
	})
	if visited != endingsHeld {
		t.Fatalf("the ring visited %d entries, want %d", visited, endingsHeld)
	}
}

// The listing costs no tick. Unlike a Snapshot, which must step a paused engine
// because there is nothing to record otherwise, retained Voices are already
// there to read - so calling it repeatedly moves no playhead and hands the
// Adapter nothing.
func TestTheListingCostsNoTick(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tickAt(9)
	before := listing(t, h)
	batches := len(backend.emitted())

	for range 5 {
		if again := listing(t, h); !reflect.DeepEqual(again, before) {
			t.Fatalf("a second look answered %+v, want the same %+v: it describes a moment it did "+
				"not cause", again, before)
		}
	}
	if after := len(backend.emitted()); after != batches {
		t.Fatalf("five looks handed the Adapter %d batches, want none", after-batches)
	}
}
