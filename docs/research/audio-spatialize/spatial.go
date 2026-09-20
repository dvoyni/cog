package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// This file is sound's half: the arithmetic #297 adopted verbatim from the W3C
// Web Audio API, computed once per tick and handed down as the 2x2
// source->output gain matrix #376 says crosses the seam. Nothing in here knows
// what a device is, and nothing below the seam sees a position.

import (
	"fmt"
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// DistanceModel is W3C's three, in W3C's order of usefulness rather than its
// order of definition. Inverse is the default #297 adopted and item 2 of the
// ticket asks whether that default is right.
type DistanceModel int

const (
	DistanceInverse DistanceModel = iota
	DistanceLinear
	DistanceExponential
)

func (d DistanceModel) String() string {
	switch d {
	case DistanceLinear:
		return "linear"
	case DistanceExponential:
		return "exponential"
	default:
		return "inverse"
	}
}

// Falloff is W3C's distance model with W3C's defaults.
type Falloff struct {
	Model         DistanceModel
	RefDistance   float32
	MaxDistance   float32
	RolloffFactor float32
}

// DefaultFalloff is exactly W3C's: inverse, ref 1, max 10000, rolloff 1.
func DefaultFalloff() Falloff {
	return Falloff{Model: DistanceInverse, RefDistance: 1, MaxDistance: 10000, RolloffFactor: 1}
}

// Cone is W3C's cone with W3C's defaults, which are the no-op: 360/360 means
// every direction is inside the inner cone, so the outer gain never applies.
type Cone struct {
	InnerAngle float32 // degrees, full width
	OuterAngle float32 // degrees, full width
	OuterGain  float32
}

// DefaultCone is W3C's: 360 / 360 / 0, which is silent about direction.
func DefaultCone() Cone { return Cone{InnerAngle: 360, OuterAngle: 360, OuterGain: 0} }

// Listener is one listener, on #297's axes: right-handed, facing -Z, +Y up.
type Listener struct {
	Position m.Vec3
	Forward  m.Vec3
	Up       m.Vec3
}

// DefaultListener faces -Z from the origin with +Y up - scene.LookAt's
// convention and the W3C listener default, which #297 noted are the same.
func DefaultListener() Listener {
	return Listener{Forward: m.Vec3{X: 0, Y: 0, Z: -1}, Up: m.Vec3{X: 0, Y: 1, Z: 0}}
}

// Matrix is source channel -> output channel gain, rows indexed by source
// channel. A mono source spends row 0 only.
//
// This is #376's claim made concrete, and the stereo arm of panMatrix is the
// evidence for it: a scalar pan cannot express W3C stereo panning, because at
// azimuth <= 0 the left source channel passes through at unity while the right
// one is both attenuated and bled into the left output. Four numbers, not one.
type Matrix [2][2]float32

// Emitter is everything sound knows about one voice's placement. A voice with
// Positional false is music or UI: no position, no falloff, no cone.
type Emitter struct {
	Positional  bool
	Position    m.Vec3
	Orientation m.Vec3 // the cone's axis; zero means no cone
	Falloff     Falloff
	Cone        Cone
	Volume      float32 // the game's own per-voice gain, before any of this
	// BusVolume is the voice's bus, which #296 predeclares as Master = 0.
	// #376 keeps buses off the seam entirely: sound folds the bus's volume into
	// the matrix, at the cost of re-emitting a bus's voices when it changes. So
	// it is one more scalar here and nothing at all below.
	//
	// Zero reads as unity, which is canvas's habit with a zero scale and ui's
	// with a zero tint: without it, every caller that does not care about buses
	// would silently get silence.
	BusVolume float32
}

// Spatialized is what one tick of the arithmetic produces for one voice: the
// matrix that crosses the seam, plus the intermediate terms, which cross
// nothing but are what #305's capability reports and what the on-screen
// instrumentation shows.
type Spatialized struct {
	Matrix       Matrix
	Azimuth      float32 // degrees, 0 ahead, +90 right
	Elevation    float32 // degrees, +90 overhead
	Distance     float32
	DistanceGain float32
	ConeGain     float32
	// Audibility is the final scalar gain, which #301 makes the stealing rank
	// and #305 reports.
	//
	// It is deliberately the gain *before* panning: volume x distance x cone.
	// Equal-power panning preserves power by construction, so it cannot make a
	// voice louder or quieter, only move it between the ears - and a measure
	// taken from the matrix says otherwise. Reading it as max(L, R) makes a
	// centred voice score 0.707 against a hard-panned one's 1.0 at the same
	// distance, a 3 dB difference out of nothing, which would have #301's
	// stealing throw away the sound in front of you in favour of the one beside
	// you. Prototyped, measured, and corrected here.
	Audibility float32
}

// Spatialize is the whole of sound's arithmetic for one voice.
func Spatialize(e Emitter, l Listener, channels int) Spatialized {
	bus := e.BusVolume
	if bus == 0 {
		bus = 1
	}
	volume := e.Volume * bus
	if !e.Positional {
		// A non-positional voice is centred and unattenuated. It still gets a
		// matrix, because the seam has no other shape.
		s := Spatialized{DistanceGain: 1, ConeGain: 1, Audibility: volume}
		if channels == 1 {
			// Mono spread to both outputs at equal power, which is what a
			// centred equal-power pan comes to anyway.
			g := volume * float32(math.Sqrt2/2)
			s.Matrix[0][0], s.Matrix[0][1] = g, g
		} else {
			s.Matrix[0][0], s.Matrix[1][1] = volume, volume
		}
		return s
	}

	azimuth, elevation := azimuthElevation(e.Position, l)
	distance := e.Position.Sub(l.Position).Length()
	dg := e.Falloff.gain(distance)
	cg := e.Cone.gain(e.Position, e.Orientation, l.Position)
	scalar := volume * dg * cg

	mat := panMatrix(azimuth, channels)
	for r := range mat {
		for c := range mat[r] {
			mat[r][c] *= scalar
		}
	}

	return Spatialized{
		Matrix:       mat,
		Azimuth:      azimuth,
		Elevation:    elevation,
		Distance:     distance,
		DistanceGain: dg,
		ConeGain:     cg,
		Audibility:   scalar,
	}
}

// azimuthElevation is the W3C spec's own algorithm, transcribed rather than
// re-derived, so that a PannerNode backend would agree by construction - which
// is the promise #300 made and the last check on the ticket.
//
// azimuth is degrees in [-180, 180]: 0 straight ahead, +90 to the right, +-180
// straight behind.
func azimuthElevation(source m.Vec3, l Listener) (azimuth, elevation float32) {
	sl := source.Sub(l.Position)
	if sl.LengthSquared() == 0 {
		return 0, 0
	}
	sl = sl.Normalize()

	forward := l.Forward.Normalize()
	right := forward.Cross(l.Up.Normalize()).Normalize()
	// W3C recomputes up from right x forward rather than trusting the listener's
	// own up, which re-orthogonalizes a listener whose up is not perpendicular
	// to its forward.
	up := right.Cross(forward).Normalize()

	projected := sl.Sub(up.MulS(sl.Dot(up)))
	if projected.LengthSquared() == 0 {
		// Straight overhead or straight underneath. The projection onto the
		// horizontal plane vanishes and the spec's acos has nothing to take, so
		// azimuth is 0 by definition - the source snaps to dead ahead. This is
		// item 1's second listening case, and the question is whether the snap
		// is audible on the way through.
		azimuth = 0
	} else {
		projected = projected.Normalize()
		azimuth = degrees(math.Acos(float64(clamp32(projected.Dot(right), -1, 1))))
		if projected.Dot(forward) < 0 {
			azimuth = 360 - azimuth
		}
		// Make azimuth relative to front rather than to the right vector.
		if azimuth >= 0 && azimuth <= 270 {
			azimuth = 90 - azimuth
		} else {
			azimuth = 450 - azimuth
		}
	}

	elevation = 90 - degrees(math.Acos(float64(clamp32(sl.Dot(up), -1, 1))))
	if elevation > 90 {
		elevation = 180 - elevation
	} else if elevation < -90 {
		elevation = -180 - elevation
	}
	return azimuth, elevation
}

// gain is W3C's three distance models, verbatim.
func (f Falloff) gain(distance float32) float32 {
	ref, maxd, rolloff := f.RefDistance, f.MaxDistance, f.RolloffFactor
	switch f.Model {
	case DistanceLinear:
		if maxd == ref {
			return 1
		}
		d := clamp32(distance, ref, maxd)
		return 1 - rolloff*(d-ref)/(maxd-ref)
	case DistanceExponential:
		if ref <= 0 {
			return 1
		}
		return float32(math.Pow(float64(max32(distance, ref)/ref), float64(-rolloff)))
	default: // inverse
		denom := ref + rolloff*(max32(distance, ref)-ref)
		if denom == 0 {
			return 1
		}
		return ref / denom
	}
}

// gain is W3C's cone, verbatim. A zero orientation or a 360/360 cone is the
// no-op, which is how the default disappears.
func (c Cone) gain(source, orientation, listener m.Vec3) float32 {
	if orientation.LengthSquared() == 0 || (c.InnerAngle == 360 && c.OuterAngle == 360) {
		return 1
	}
	toListener := listener.Sub(source)
	if toListener.LengthSquared() == 0 {
		return 1
	}
	angle := abs32(degrees(math.Acos(float64(clamp32(
		toListener.Normalize().Dot(orientation.Normalize()), -1, 1)))))
	inner := abs32(c.InnerAngle) / 2
	outer := abs32(c.OuterAngle) / 2
	switch {
	case angle <= inner:
		return 1
	case angle >= outer:
		return c.OuterGain
	default:
		x := (angle - inner) / (outer - inner)
		return (1 - x) + c.OuterGain*x
	}
}

// panMatrix is W3C's equalpower panning, which is the only algorithm #297
// adopted: HRTF is out of scope, and there is no third choice.
//
// Note what the fold does. A source behind the listener maps to the same pan as
// its mirror image in front, because two speakers cannot say "behind". Item 1's
// first listening case is exactly this: at azimuth 90 -> 91 the pan stops
// moving right and starts coming back, with no other cue that the source went
// past.
func panMatrix(azimuth float32, channels int) Matrix {
	a := clamp32(azimuth, -180, 180)
	if a < -90 {
		a = -180 - a
	} else if a > 90 {
		a = 180 - a
	}

	var mat Matrix
	if channels == 1 {
		x := (a + 90) / 180
		mat[0][0] = cos32(x * math.Pi / 2)
		mat[0][1] = sin32(x * math.Pi / 2)
		return mat
	}

	var x float32
	if a <= 0 {
		x = (a + 90) / 90
	} else {
		x = a / 90
	}
	gl := cos32(x * math.Pi / 2)
	gr := sin32(x * math.Pi / 2)
	if a <= 0 {
		// outL = inL + inR*gl ; outR = inR*gr
		mat[0][0], mat[0][1] = 1, 0
		mat[1][0], mat[1][1] = gl, gr
	} else {
		// outL = inL*gl ; outR = inR + inL*gr
		mat[0][0], mat[0][1] = gl, gr
		mat[1][0], mat[1][1] = 0, 1
	}
	return mat
}

func (m Matrix) String() string {
	return fmt.Sprintf("[%.3f %.3f / %.3f %.3f]", m[0][0], m[0][1], m[1][0], m[1][1])
}

func degrees(radians float64) float32 { return float32(180 * radians / math.Pi) }
func cos32(v float32) float32         { return float32(math.Cos(float64(v))) }
func sin32(v float32) float32         { return float32(math.Sin(float64(v))) }
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
