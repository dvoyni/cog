package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

// gainTolerance is what "run-to-run determinism on one build, and explicitly
// not bit-equality across architectures" means to an assertion about a gain.
// math.Acos and math.Cos carry no bit-equality guarantee, so every gain these
// tests read is compared with a tolerance rather than with ==.
const gainTolerance = 1e-5

// sameGains compares two gain matrices within that tolerance. It exists
// because equalpower panning at dead centre puts a cos(Pi/2) in one entry, and
// cos(Pi/2) is 6e-17 rather than 0 in every language that has ever computed it.
func sameGains(got, want [2][2]float32) bool {
	for source := range got {
		for output := range got[source] {
			if math.Abs(float64(got[source][output]-want[source][output])) > gainTolerance {
				return false
			}
		}
	}
	return true
}

// closeEnough compares one gain, one audibility or one distance.
func closeEnough(got, want float32) bool {
	return math.Abs(float64(got-want)) <= gainTolerance
}

// A Voice with no position is non-positional: no falloff, no cone and no
// panning, heard centred. A UI click is one, and it is the overwhelming
// majority of what a game plays.
//
// "Heard centred" is a bearing of 0 run through the same equations rather than
// a second rule, which is why a mono Clip lands at 0.707 in each ear and not at
// unity in both - equal power, not a doubling.
func TestANonPositionalVoiceIsHeardCentredAtEqualPower(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{Volume: m.Some[float32](1)})
	h.tick()

	info := h.probe(voice).Info
	if !closeEnough(info.Audibility, 1) {
		t.Fatalf("a non-positional Voice is audible at %v, want its own volume and nothing else", info.Audibility)
	}
	if info.Distance != 0 {
		t.Fatalf("a non-positional Voice reports a distance of %v, and it has no position to be far from", info.Distance)
	}

	centred := float32(math.Sqrt2 / 2)
	if got := h.backend.emitted()[0].Starts[0].Params.Gains; !sameGains(got, [2][2]float32{{centred, centred}, {0, 0}}) {
		t.Fatalf("a centred mono Clip crosses the seam as %v, want %v in each ear", got, centred)
	}
}

// Positional is one-way. The first position a Voice receives, at Play or by
// SetVoice, makes it positional for the rest of its life: no operation is
// silently ignored, no clear verb is needed, and a Voice that should stop being
// positional is a new Play.
func TestTheFirstPositionMakesAVoicePositionalForTheRestOfItsLife(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	// A Falloff given to a Voice with no position is kept and takes effect
	// when it gets one - which is the whole of what "kept" has to mean.
	voice := h.play(ClipWithResource(bell), 0, Params{
		Volume:  m.Some[float32](1),
		Falloff: m.Some(DefaultFalloff()),
	})
	h.tick()
	if got := h.probe(voice).Info.Audibility; !closeEnough(got, 1) {
		t.Fatalf("a Voice carrying a Falloff and no position is audible at %v, want 1", got)
	}

	h.record(func(queue *Queue) {
		queue.SetVoice(voice, Params{Position: m.Some(m.Vec3{X: 4})})
	})
	h.tick()

	info := h.probe(voice).Info
	if !closeEnough(info.Distance, 4) {
		t.Fatalf("the Voice is %v from the Listener, want 4", info.Distance)
	}
	if !closeEnough(info.Audibility, 0.25) {
		t.Fatalf("a source at radius 4 is audible at %v, want W3C's 0.25 - that is -12 dB", info.Audibility)
	}

	// Hard right: a mono Clip goes entirely to the right ear.
	gains := h.backend.emitted()[1].Updates[0].Params.Gains
	if !closeEnough(gains[0][0], 0) || !closeEnough(gains[0][1], 0.25) {
		t.Fatalf("a source on the player's right crosses the seam as %v, want all of it in the right ear", gains)
	}
}

// Audibility is the scalar before panning - volume x bus x falloff x cone - and
// never max(L, R) taken off the matrix.
//
// The orbit is what proves it. A source going round the Listener at a constant
// radius is exactly as loud at every point of the circle, while every entry of
// its gain matrix travels the whole way from one ear to the other. Reading
// audibility off the matrix would make it swing by a factor of the square root
// of two twice a lap, and "quietest" and "furthest away" would be two policies
// that disagree with each other on a circle - which is the voice cap's problem,
// one ticket downstream.
func TestAudibilityDoesNotMoveOverAConstantRadiusOrbit(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	const radius = 5
	voice := h.play(ClipWithResource(bell), 0, Params{
		Volume:   m.Some[float32](1),
		Position: m.Some(m.Vec3{Z: -radius}),
	})
	h.tick()

	first := h.probe(voice).Info.Audibility
	if first <= 0 || first >= 1 {
		t.Fatalf("a source at radius %v is audible at %v, and the falloff is doing nothing", radius, first)
	}

	var leftmost, rightmost float32
	for degrees := 1; degrees < 360; degrees++ {
		radians := float64(degrees) * math.Pi / 180
		position := m.Vec3{
			X: float32(math.Sin(radians) * radius),
			Z: float32(-math.Cos(radians) * radius),
		}
		h.record(func(queue *Queue) {
			queue.SetVoice(voice, Params{Position: m.Some(position)})
		})
		h.tick()

		if got := h.probe(voice).Info.Audibility; !closeEnough(got, first) {
			t.Fatalf("%v degrees round the orbit the source is audible at %v, want %v at every point",
				degrees, got, first)
		}

		gains := h.backend.emitted()[degrees].Updates[0].Params.Gains
		leftmost = max(leftmost, gains[0][0])
		rightmost = max(rightmost, gains[0][1])
	}

	// The test is only worth anything if the matrix did move. Hard left and
	// hard right both put the whole of the source in one ear.
	if !closeEnough(leftmost, first) || !closeEnough(rightmost, first) {
		t.Fatalf("the orbit's loudest left was %v and its loudest right %v, want both to reach %v: "+
			"if the matrix never moved, a constant audibility proves nothing", leftmost, rightmost, first)
	}
}

// A cone needs a facing. A Positional Voice with no Orientation is equally loud
// in every direction whatever its Cone says, so a Voice can never be
// accidentally directional along an axis nobody chose - W3C's (1,0,0)
// PannerNode.orientation default is deliberately not inherited, and inheriting
// it here would make every coneless source directional.
func TestAPositionalVoiceWithNoOrientationIsEquallyLoudInEveryDirection(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	// A cone that is silent outside 90 degrees. Were W3C's (1,0,0) default
	// inherited, this Voice would be inaudible everywhere but on the +X axis.
	narrow := Cone{Inner: 60, Outer: 90, OuterGain: 0}
	voice := h.play(ClipWithResource(bell), 0, Params{
		Volume:   m.Some[float32](1),
		Falloff:  m.Some(Falloff{Model: DistanceInverse, Ref: 100, Max: 10000, Rolloff: 1}),
		Cone:     m.Some(narrow),
		Position: m.Some(m.Vec3{Z: -4}),
	})
	h.tick()

	ahead := h.probe(voice).Info.Audibility
	if !closeEnough(ahead, 1) {
		t.Fatalf("a coneless-facing source is audible at %v, want its own volume undimmed", ahead)
	}

	for _, at := range []m.Vec3{{X: 4}, {X: -4}, {Z: 4}, {Y: 4}} {
		h.record(func(queue *Queue) {
			queue.SetVoice(voice, Params{Position: m.Some(at)})
		})
		h.tick()
		if got := h.probe(voice).Info.Audibility; !closeEnough(got, ahead) {
			t.Fatalf("with no Orientation the source at %v is audible at %v, want %v everywhere", at, got, ahead)
		}
	}

	// Give it a facing and the same Cone bites: pointed along -Z with the
	// Listener at the origin behind it, the Listener is outside the outer cone.
	h.record(func(queue *Queue) {
		queue.SetVoice(voice, Params{
			Position:    m.Some(m.Vec3{Z: -4}),
			Orientation: m.Some(m.Quat{W: 1}),
		})
	})
	h.tick()
	if got := h.probe(voice).Info.Audibility; !closeEnough(got, 0) {
		t.Fatalf("a source facing away inside a 90 degree cone is audible at %v, want the outer gain 0", got)
	}
}

// One Listener, set by SetListener and readable back, sitting at the origin
// with no rotation before any call. An absent field is unchanged, which is what
// lets a 2D game set its rotation once at startup and only ever move afterwards.
func TestTheListenerIsReadableAndCoalescedFieldByField(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 10, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	// It reads back as the identity quaternion rather than the zero one,
	// because the zero Quat is (0,0,0,0) and is not a rotation at all.
	h.tick()
	if got := h.probe(NoVoice); got.ListenerAt != (m.Vec3{}) || got.ListenerFacing != (m.Quat{W: 1}) {
		t.Fatalf("before any call the Listener is at %v facing %v, want the origin, unrotated",
			got.ListenerAt, got.ListenerFacing)
	}

	// The 2D recipe, set once and never restated.
	flat := m.QuatRotationX(-math.Pi / 2)
	h.record(func(queue *Queue) {
		queue.SetListener(ListenerParams{Orientation: m.Some(flat)})
	})
	h.tick()

	// Two Systems in one tick, each saying half of it: the last word on each
	// field wins, and neither overwrites the other's.
	h.record(func(queue *Queue) {
		queue.SetListener(ListenerParams{Position: m.Some(m.Vec3{X: 1})})
		queue.SetListener(ListenerParams{Position: m.Some(m.Vec3{X: 90, Y: 12})})
	})
	h.tick()

	got := h.probe(NoVoice)
	if got.ListenerAt != (m.Vec3{X: 90, Y: 12}) {
		t.Fatalf("the Listener stands at %v, want the tick's last word on its position", got.ListenerAt)
	}
	if got.ListenerFacing != flat {
		t.Fatalf("the Listener faces %v, want the rotation set a tick ago and never restated", got.ListenerFacing)
	}
}

// Moving the Listener changes every Positional Voice at once, with no operation
// naming any of them - a player turning on the spot re-emits the sources around
// them. A Voice whose bearing and falloff did not move is not re-emitted, which
// is the Buses' "a fold that did not move is not a change" met a second time.
func TestMovingTheListenerReEmitsTheVoicesItMovedRelativeTo(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 600, channels: 1, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	positional := h.play(ClipWithResource(bell), 0, Params{Position: m.Some(m.Vec3{X: 4})})
	h.play(ClipWithResource(bell), 0, Params{})
	h.tick()

	// A tick in which nothing at all was said.
	h.tick()
	if updates := h.backend.emitted()[1].Updates; len(updates) != 0 {
		t.Fatalf("a silent tick produced %d updates, want none", len(updates))
	}

	h.record(func(queue *Queue) {
		queue.SetListener(ListenerParams{Position: m.Some(m.Vec3{X: 4})})
	})
	h.tick()

	updates := h.backend.emitted()[2].Updates
	if len(updates) != 1 {
		t.Fatalf("walking onto the source produced %d updates, want the one Voice that moved relative to it", len(updates))
	}
	if updates[0].Slot != 0 {
		t.Fatalf("the update is for slot %d, want the Positional Voice's slot 0", updates[0].Slot)
	}
	if got := h.probe(positional).Info.Distance; got != 0 {
		t.Fatalf("standing on the source, it is %v away", got)
	}

	// Standing on top of it, nothing normalizes a zero vector: it is heard
	// centred, at the reference distance, and no NaN reaches the seam.
	gains := updates[0].Params.Gains
	if !sameGains(gains, [2][2]float32{{float32(math.Sqrt2 / 2), float32(math.Sqrt2 / 2)}, {0, 0}}) {
		t.Fatalf("a source at the Listener's exact position crosses the seam as %v, want it centred", gains)
	}
}
