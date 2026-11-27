package sfx

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The functions in this file are the W3C Web Audio API specification's
// equations, transcribed from the W3C document and adopted normatively: the
// three distance models, the cone gain, the azimuth and elevation of a
// source, and equal-power panning from azimuth. A PannerNode backend agrees
// with them by construction rather than by careful matching.
//
// The transcription is literal, down to the order of operations and the
// degrees-then-radians round trip, so a reader checks it against the
// document. What is added is named where it is added, and is only ever a
// guard against a value that must not reach a device: a NaN gain, or the
// normalization of a zero vector. Every cosine is clamped into [-1, 1] with
// m.Clamp before math.Acos, since two vectors normalized in float32 can dot a
// hair past 1, where math.Acos answers NaN.
//
// The arithmetic runs in float64 and lands in float32. sfx promises
// run-to-run determinism on one build, and not bit-equality across
// architectures: math.Acos and math.Cos carry no such guarantee.

// computeDistanceGain answers W3C's distance gain: how much quieter a source
// is for being distance from the listener, in the game's units, as falloff's
// are.
func computeDistanceGain(falloff Falloff, distance float64) float64 {
	reference := float64(falloff.Ref)
	maximum := float64(falloff.Max)
	// W3C's nominal range of rolloffFactor is [0, ∞), held to [0, 1] for the
	// linear model.
	rolloff := math.Max(float64(falloff.Rolloff), 0)

	var gain float64
	switch falloff.Model {
	case DistanceLinear:
		// 1 - rolloffFactor * (max(min(d, dMax), dRef) - dRef) / (dMax - dRef)
		rolloff = math.Min(rolloff, 1)
		gain = 1 - rolloff*(math.Max(math.Min(distance, maximum), reference)-reference)/(maximum-reference)
	case DistanceExponential:
		// pow(max(d, dRef) / dRef, -rolloffFactor)
		gain = math.Pow(math.Max(distance, reference)/reference, -rolloff)
	default:
		// dRef / (dRef + rolloffFactor * (max(d, dRef) - dRef))
		gain = reference / (reference + rolloff*(math.Max(distance, reference)-reference))
	}

	// Added: a falloff whose own fields make a divisor zero divides zero by
	// zero, which W3C leaves undefined. An undefined attenuation is no
	// attenuation.
	if math.IsNaN(gain) {
		return 1
	}
	return gain
}

// computeConeGain answers W3C's cone gain: how much quieter a source at
// source, facing forward, a unit vector, is for pointing away from a listener
// at listener. A voice with no facing never reaches it: the caller answers 1
// for one, so "equally loud in every direction" does not depend on a facing
// nobody chose.
func computeConeGain(cone Cone, source, forward, listener m.Vec3) float64 {
	// W3C: a zero orientation, or both angles 360, is no cone.
	if forward == (m.Vec3{}) || (cone.Inner == 360 && cone.Outer == 360) {
		return 1
	}

	// Added: a source standing on the listener has no direction to it, so it
	// is inside every cone, and nothing normalizes the zero vector.
	sourceToListener := listener.Sub(source)
	if sourceToListener == (m.Vec3{}) {
		return 1
	}
	sourceToListener = sourceToListener.Normalize()

	// The angle between the source's orientation and the source-listener
	// vector, in degrees.
	angle := 180 * math.Acos(float64(m.Clamp(sourceToListener.Dot(forward), -1, 1))) / math.Pi
	absAngle := math.Abs(angle)

	// Divided by 2 because the API takes the whole angle, not the half-angle.
	absInner := math.Abs(float64(cone.Inner)) / 2
	absOuter := math.Abs(float64(cone.Outer)) / 2
	outerGain := float64(cone.OuterGain)

	switch {
	case absAngle <= absInner:
		// No attenuation.
		return 1
	case absAngle >= absOuter:
		// Max attenuation.
		return outerGain
	default:
		// Between the inner and the outer cones: inner -> outer, x goes
		// 0 -> 1.
		x := (absAngle - absInner) / (absOuter - absInner)
		return (1-x)*1 + x*outerGain
	}
}

// computeAzimuthElevation answers W3C's azimuth and elevation, in degrees, of
// a source at source about a listener at listener whose front and up are its
// axes. Ahead is azimuth 0, the right +90, the left -90 and behind -180;
// overhead is elevation +90 and underfoot -90.
func computeAzimuthElevation(source, listener, front, up m.Vec3) (azimuth, elevation float64) {
	// The source-listener vector. W3C answers 0 and 0 for a source at the
	// listener's exact position, and nothing normalizes the zero vector.
	sourceListener := source.Sub(listener)
	if sourceListener == (m.Vec3{}) {
		return 0, 0
	}
	sourceListener = sourceListener.Normalize()

	// Align the axes: the up the projection takes is the listener's up made
	// orthogonal to its front, rebuilt from the right vector.
	listenerRight := front.Cross(up).Normalize()
	listenerFront := front.Normalize()
	upVector := listenerRight.Cross(listenerFront)

	upProjection := sourceListener.Dot(upVector)
	projectedSource := sourceListener.Sub(upVector.MulS(upProjection))

	// Added: a source straight overhead or underfoot projects to the zero
	// vector, which is never normalized; its azimuth is 0, the zenith snap.
	if projectedSource != (m.Vec3{}) {
		projectedSource = projectedSource.Normalize()
		azimuth = 180 * math.Acos(float64(m.Clamp(projectedSource.Dot(listenerRight), -1, 1))) / math.Pi

		// The source in front of the listener or behind it.
		if projectedSource.Dot(listenerFront) < 0 {
			azimuth = 360 - azimuth
		}

		// The azimuth relative to the listener's front, not its right.
		if azimuth >= 0 && azimuth <= 270 {
			azimuth = 90 - azimuth
		} else {
			azimuth = 450 - azimuth
		}
	}

	elevation = 90 - 180*math.Acos(float64(m.Clamp(sourceListener.Dot(upVector), -1, 1)))/math.Pi
	if elevation > 90 {
		elevation = 180 - elevation
	} else if elevation < -90 {
		elevation = -180 - elevation
	}
	return azimuth, elevation
}

// computeEqualPowerGains answers W3C's equal-power panning from azimuth, as
// the matrix the seam carries, Gains[source channel][output channel], row 0
// being a mono clip's only row.
//
// A stereo clip keeps both channels and shifts weight between them rather
// than being mixed down: W3C's stereo arm passes one channel at unity and
// bleeds the other into it, so a stereo source panned hard right is its left
// channel folded into its right, not its left channel silenced. A
// non-positional voice runs it at azimuth 0, which leaves a mono clip at
// cos(π/4) in each ear and a stereo clip on its own diagonal.
func computeEqualPowerGains(azimuth float64, stereo bool) [2][2]float32 {
	// Clamp the azimuth to [-180, 180].
	azimuth = math.Max(-180, azimuth)
	azimuth = math.Min(180, azimuth)

	// Then wrap it to [-90, 90]: the fold behind, a source behind panning as
	// its mirror image in front.
	if azimuth < -90 {
		azimuth = -180 - azimuth
	} else if azimuth > 90 {
		azimuth = 180 - azimuth
	}

	// The normalized value x, from the azimuth.
	var x float64
	switch {
	case !stereo:
		x = (azimuth + 90) / 180
	case azimuth <= 0:
		x = (azimuth + 90) / 90
	default:
		x = azimuth / 90
	}

	gainLeft := math.Cos(x * math.Pi / 2)
	gainRight := math.Sin(x * math.Pi / 2)

	var gains [2][2]float32
	switch {
	case !stereo:
		// outputL = input * gainLeft; outputR = input * gainRight
		gains[0][0] = float32(gainLeft)
		gains[0][1] = float32(gainRight)
	case azimuth <= 0:
		// outputL = inputL + inputR * gainLeft; outputR = inputR * gainRight
		gains[0][0] = 1
		gains[1][0] = float32(gainLeft)
		gains[1][1] = float32(gainRight)
	default:
		// outputL = inputL * gainLeft; outputR = inputR + inputL * gainRight
		gains[0][0] = float32(gainLeft)
		gains[0][1] = float32(gainRight)
		gains[1][1] = 1
	}
	return gains
}
