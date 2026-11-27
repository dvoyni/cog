package sfx_test

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// gainTolerance is what run-to-run determinism on one build, and not
// bit-equality across architectures, means to an assertion about a gain.
const gainTolerance = 1e-4

// centredMonoGain is a mono clip's gain in each ear when it is heard centred.
var centredMonoGain = float32(math.Sqrt2 / 2)

// isClose compares one gain, one audibility or one distance.
func isClose(got, want float32) bool {
	return math.Abs(float64(got-want)) <= gainTolerance
}

// isSameMatrix compares two gain matrices within gainTolerance: equal-power
// panning puts a cos(π/2) in one entry, 6e-17 rather than 0.
func isSameMatrix(got, want [2][2]float32) bool {
	for source := range got {
		for output := range got[source] {
			if !isClose(got[source][output], want[source][output]) {
				return false
			}
		}
	}
	return true
}

// playWith records a play of the fixture clip with options, steps, and
// answers its handle.
func playWith(h *sfxtest.Harness, options sfx.VoiceOptions) sfx.Voice {
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, 0, options) })
	h.Step()
	return voice
}

// readListener answers the listener as of the last flush.
func readListener(h *sfxtest.Harness) sfx.Listener {
	var heardFrom sfx.Listener
	h.Read(func(_ sfx.LiveVoices, _ sfx.ClipTable, listener sfx.Listener) { heardFrom = listener })
	return heardFrom
}

// monoPlugin wraps sfx's BackendSocket so every clip it prepares says it is
// mono, since the fixture is stereo and a mono clip's one row is the plainest
// reading of a pan.
type monoPlugin struct{}

func (monoPlugin) Name() kernel.PluginName { return "mono" }

func (monoPlugin) Dependencies() []kernel.PluginName { return nil }

func (monoPlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.WrapSocket[sfx.BackendSocket](func(bound sfx.BackendSocket) sfx.BackendSocket {
		return monoBackend{BackendSocket: bound}
	})
	return nil
}

func (monoPlugin) Start(*kernel.Kernel) error { return nil }

func (monoPlugin) Stop(*kernel.Kernel) error { return nil }

func (monoPlugin) Destroy() error { return nil }

// monoBackend is the bound backend, its prepared clips said to be mono.
type monoBackend struct{ sfx.BackendSocket }

func (b monoBackend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	prepared, done, err := b.BackendSocket.Prepare(token, encoded)
	if prepared != nil {
		prepared = monoClip{PreparedClip: prepared}
	}
	return prepared, done, err
}

// monoClip is a prepared clip said to have one channel.
type monoClip struct{ sfx.PreparedClip }

func (monoClip) Channels() int { return 1 }

// A voice with no position is non-positional: no falloff, no cone and no
// panning, heard centred, which is what a UI click is. "Heard centred" is
// azimuth 0 run through the same equations, so a mono clip is cos(π/4) in
// each ear rather than unity in both.
func TestANonPositionalVoiceIsHeardCentredAtEqualPower(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, monoPlugin{}, seamPlugin{seam: seam})

	voice := playWith(h, sfx.VoiceOptions{})

	entry, _ := readLiveVoice(h, voice)
	if !isClose(entry.Audibility, 1) {
		t.Fatalf("a non-positional voice is audible at %v, want 1", entry.Audibility)
	}
	if entry.Distance != 0 {
		t.Fatalf("a non-positional voice is %v from the listener, and it has no position to be far from", entry.Distance)
	}
	if got := seam.emitted()[0].Starts[0].Params.Gains; !isSameMatrix(got, [2][2]float32{{centredMonoGain, centredMonoGain}}) {
		t.Fatalf("a centred mono clip crosses the seam as %v, want %v in each ear", got, centredMonoGain)
	}
}

// A voice to the listener's right is heard right, quieter by its distance:
// at 4 on W3C's defaults, 0.25, which is -12 dB. The stereo fixture is panned
// rather than mixed down, its left channel folded into the right ear.
func TestAVoiceToTheListenersRightIsHeardRight(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := playWith(h, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: 4})})

	entry, _ := readLiveVoice(h, voice)
	if !isClose(entry.Distance, 4) || !isClose(entry.Audibility, 0.25) {
		t.Fatalf("a voice at X 4 is %v away and audible at %v, want 4 and W3C's 0.25", entry.Distance, entry.Audibility)
	}
	if got := seam.emitted()[0].Starts[0].Params.Gains; !isSameMatrix(got, [2][2]float32{{0, 0.25}, {0, 0.25}}) {
		t.Fatalf("a stereo voice on the listener's right crosses the seam as %v, want both channels in the right ear", got)
	}
}

// A 2D game rotates its listener once, and only then does a sprite's Y
// matter: under an unrotated listener a sprite 20 px right and one 20 px
// right and 400 px up the screen are both hard right; under the rotation the
// second is nearly centred. The falloff's Ref is the screen's scale, so
// neither is attenuated.
func TestTheTwoDimensionalRotationMakesASpritesYMatter(t *testing.T) {
	pixelFalloff := m.Some(sfx.Falloff{Model: sfx.DistanceInverse, Ref: 1000, Max: 10000, Rolloff: 1})
	for _, listener := range []struct {
		name        string
		orientation m.Quat
		farLeft     float32
	}{
		// The far sprite's left ear: hard right, nothing; at 2.86 degrees,
		// cos of x times π/2 with x = (90 + 2.86) / 180.
		{"unrotated", m.Quat{W: 1}, 0},
		{"rotated", m.QuatRotationX(-math.Pi / 2), float32(math.Cos((90 + 2.8624) / 180 * math.Pi / 2))},
	} {
		seam := &seam{}
		h := newHarness(t, nil, monoPlugin{}, seamPlugin{seam: seam})
		h.Record(func(queue sfx.Queue) {
			queue.SetListener(sfx.ListenerOptions{Orientation: m.Some(listener.orientation)})
			queue.Play(clipPath, 0, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: 20}), Falloff: pixelFalloff})
			queue.Play(clipPath, 0, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: 20, Y: -400}), Falloff: pixelFalloff})
		})
		h.Step()

		starts := seam.emitted()[0].Starts
		if levelGains := starts[0].Params.Gains[0]; !isClose(levelGains[0], 0) || !isClose(levelGains[1], 1) {
			t.Errorf("under the %s listener the level sprite is %v, want hard right", listener.name, levelGains)
		}
		if farGains := starts[1].Params.Gains[0]; !isClose(farGains[0], listener.farLeft) {
			t.Errorf("under the %s listener the sprite up the screen is %v, want %v in the left ear", listener.name, farGains, listener.farLeft)
		}
	}
}

// Audibility is the scalar before panning, never anything read off the
// matrix: over an orbit at one distance it does not move, while every entry
// of the matrix travels from one ear to the other. The listener orbits the
// voice, which is the same orbit seen from the voice.
func TestAudibilityDoesNotMoveOverAConstantRadiusOrbit(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, monoPlugin{}, seamPlugin{seam: seam})

	const radius = 5
	voice := playWith(h, sfx.VoiceOptions{Position: m.Some(m.Vec3{Z: -radius})})

	firstEntry, _ := readLiveVoice(h, voice)
	if firstEntry.Audibility <= 0 || firstEntry.Audibility >= 1 {
		t.Fatalf("a voice %v away is audible at %v, and the falloff is doing nothing", radius, firstEntry.Audibility)
	}

	// Six degrees a flush, so the orbit ends before the fixture does, and
	// passes hard left and hard right on the way.
	var leftmost, rightmost float32
	for step := 1; step < 60; step++ {
		degrees := step * 6
		radians := float64(degrees) * math.Pi / 180
		position := m.Vec3{X: float32(-math.Sin(radians) * radius), Z: float32(math.Cos(radians)*radius) - radius}
		h.Record(func(queue sfx.Queue) { queue.SetListener(sfx.ListenerOptions{Position: m.Some(position)}) })
		h.Step()

		if entry, _ := readLiveVoice(h, voice); !isClose(entry.Audibility, firstEntry.Audibility) {
			t.Fatalf("%v degrees round the orbit the voice is audible at %v, want %v at every point", degrees, entry.Audibility, firstEntry.Audibility)
		}
		for _, update := range seam.emitted()[step].Updates {
			leftmost = max(leftmost, update.Params.Gains[0][0])
			rightmost = max(rightmost, update.Params.Gains[0][1])
		}
	}

	// A constant audibility proves nothing unless the matrix moved: hard left
	// and hard right each put the whole voice in one ear.
	if !isClose(leftmost, firstEntry.Audibility) || !isClose(rightmost, firstEntry.Audibility) {
		t.Fatalf("the orbit's loudest left was %v and its loudest right %v, want both to reach %v", leftmost, rightmost, firstEntry.Audibility)
	}
}

// A cone needs a facing: a positional voice with no orientation is equally
// loud in every direction whatever its cone says, since W3C's (1,0,0) default
// orientation is not inherited. Given a facing, the same cone bites.
func TestAPositionalVoiceWithNoOrientationIsEquallyLoudInEveryDirection(t *testing.T) {
	h := newHarness(t, nil)

	// A cone silent outside 45 degrees either side, and a falloff whose Ref
	// keeps every voice here unattenuated.
	narrowCone := m.Some(sfx.Cone{Inner: 60, Outer: 90, OuterGain: 0})
	unattenuatedFalloff := m.Some(sfx.Falloff{Model: sfx.DistanceInverse, Ref: 100, Max: 10000, Rolloff: 1})
	positions := []m.Vec3{{Z: -4}, {X: 4}, {X: -4}, {Z: 4}, {Y: 4}}
	voices := make([]sfx.Voice, len(positions))
	var facingAway sfx.Voice
	h.Record(func(queue sfx.Queue) {
		for index, at := range positions {
			voices[index] = queue.Play(clipPath, 0, sfx.VoiceOptions{Position: m.Some(at), Falloff: unattenuatedFalloff, Cone: narrowCone})
		}
		// Facing -Z from Z -4, with the listener at the origin behind it.
		facingAway = queue.Play(clipPath, 0, sfx.VoiceOptions{Position: m.Some(m.Vec3{Z: -4}), Orientation: m.Some(m.Quat{W: 1}), Falloff: unattenuatedFalloff, Cone: narrowCone})
	})
	h.Step()

	for index, voice := range voices {
		if entry, _ := readLiveVoice(h, voice); !isClose(entry.Audibility, 1) {
			t.Errorf("with no orientation the voice at %v is audible at %v, want 1 everywhere", positions[index], entry.Audibility)
		}
	}
	if entry, _ := readLiveVoice(h, facingAway); !isClose(entry.Audibility, 0) {
		t.Errorf("a voice facing away inside a 90 degree cone is audible at %v, want the outer gain 0", entry.Audibility)
	}
}

// One listener, set by SetListener and readable back, at the origin with no
// rotation before any call. An absent field is unchanged, so a 2D game sets
// its rotation once and only moves after, and two jobs each saying half of it
// in one flush do not overwrite each other.
func TestTheListenerIsReadableAndCoalescedFieldByField(t *testing.T) {
	h := newHarness(t, nil)

	h.Step()
	if got := readListener(h); got.Position() != (m.Vec3{}) || got.Orientation() != (m.Quat{W: 1}) {
		t.Fatalf("before any call the listener is at %v facing %v, want the origin, unrotated", got.Position(), got.Orientation())
	}

	flat := m.QuatRotationX(-math.Pi / 2)
	h.Record(func(queue sfx.Queue) { queue.SetListener(sfx.ListenerOptions{Orientation: m.Some(flat)}) })
	h.Step()

	h.Record(func(queue sfx.Queue) {
		queue.SetListener(sfx.ListenerOptions{Position: m.Some(m.Vec3{X: 1})})
		queue.SetListener(sfx.ListenerOptions{Position: m.Some(m.Vec3{X: 90, Y: 12})})
	})
	h.Step()

	got := readListener(h)
	if got.Position() != (m.Vec3{X: 90, Y: 12}) {
		t.Fatalf("the listener stands at %v, want the flush's last position", got.Position())
	}
	if got.Orientation() != flat {
		t.Fatalf("the listener faces %v, want the rotation set a flush ago and never restated", got.Orientation())
	}
}

// Moving the listener restates every positional voice it moved relative to,
// with no operation naming any of them, and nothing else: a non-positional
// voice, and a flush in which nothing moved, restate nothing. Standing on a
// voice, nothing normalizes a zero vector: it is heard centred.
func TestMovingTheListenerReEmitsTheVoicesItMovedRelativeTo(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, monoPlugin{}, seamPlugin{seam: seam})

	var positional sfx.Voice
	h.Record(func(queue sfx.Queue) {
		positional = queue.Play(clipPath, 0, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: 4})})
		queue.Play(clipPath, 0, sfx.VoiceOptions{})
	})
	h.Step()

	h.Step()
	if updates := seam.emitted()[1].Updates; len(updates) != 0 {
		t.Fatalf("a flush in which nothing moved restated %d voices, want none", len(updates))
	}

	h.Record(func(queue sfx.Queue) { queue.SetListener(sfx.ListenerOptions{Position: m.Some(m.Vec3{X: 4})}) })
	h.Step()

	updates := seam.emitted()[2].Updates
	if len(updates) != 1 || updates[0].Slot != 0 {
		t.Fatalf("walking onto the voice restated %+v, want the positional voice's slot 0 alone", updates)
	}
	if entry, _ := readLiveVoice(h, positional); entry.Distance != 0 {
		t.Fatalf("standing on the voice, it is %v away", entry.Distance)
	}
	if got := updates[0].Params.Gains; !isSameMatrix(got, [2][2]float32{{centredMonoGain, centredMonoGain}}) {
		t.Fatalf("a voice at the listener's exact position crosses the seam as %v, want it centred", got)
	}
}
