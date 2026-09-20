package types

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
)

// The arithmetic in this file is the W3C Web Audio API specification's,
// transcribed from the W3C document itself and adopted normatively: the three
// distance models, the cone gain, the azimuth and elevation of a source, and
// equalpower constant-power panning from azimuth.
//
// The reason is structural rather than aesthetic. A future PannerNode backend
// agrees with our own mixer by construction instead of by careful matching, so
// a game moved from otosound to jssound hears the same thing. The cost is
// inheriting someone else's parameter names, and two quirks: maxDistance
// clamps the linear model alone and never silences a Voice, and the linear
// formula is odd.
//
// The transcription is deliberately literal, down to the order of operations
// and the degrees-then-radians round trip, because a formula tidied up here is
// a formula a reader can no longer check against the document. What is added
// is named where it is added, and is only ever a guard against a value that
// must not reach a device: a NaN gain, or the normalization of a zero vector.
//
// Precision. sound promises run-to-run determinism on one build and explicitly
// not bit-equality across architectures - math.Acos and math.Cos carry no such
// guarantee - so the arithmetic runs in float64 and lands in float32, and every
// test of it compares with a tolerance.

// distanceGain is W3C's distance model: how much quieter a source is for being
// far away. distance is in the game's own units, and so is falloff.
//
// Only the linear model reads Max, and only the linear model can reach zero.
// That is W3C's, and correcting it here would be the one place our numbers and
// a PannerNode's disagreed.
func distanceGain(falloff Falloff, distance float64) float64 {
	reference := float64(falloff.Ref)
	maximum := float64(falloff.Max)

	// W3C's nominal range for rolloffFactor is [0, inf), reduced to [0, 1] for
	// the linear model.
	rolloff := math.Max(float64(falloff.Rolloff), 0)

	var gain float64
	switch falloff.Model {
	case DistanceLinear:
		// 1 - rolloffFactor * (max(min(d, dMax), dRef) - dRef) / (dMax - dRef)
		rolloff = math.Min(rolloff, 1)
		clamped := math.Max(math.Min(distance, maximum), reference)
		gain = 1 - rolloff*(clamped-reference)/(maximum-reference)
	case DistanceExponential:
		// pow(max(d, dRef) / dRef, -rolloffFactor)
		gain = math.Pow(math.Max(distance, reference)/reference, -rolloff)
	default:
		// dRef / (dRef + rolloffFactor * (max(d, dRef) - dRef))
		gain = reference / (reference + rolloff*(math.Max(distance, reference)-reference))
	}

	// The one guard. Every divisor above is a number the game chose, and a
	// Falloff whose fields make one of them zero divides zero by zero; W3C
	// leaves that undefined and a NaN gain silences or explodes a device
	// rather than being quiet. An undefined attenuation is no attenuation.
	if math.IsNaN(gain) {
		return 1
	}
	return gain
}

// coneGain is W3C's cone gain: how much quieter a source is for pointing away
// from the Listener. forward is the source's own facing, already a unit vector.
//
// It is 1 when there is no cone, which is what W3C's own early return says and
// is also what a Positional Voice with no Orientation gets - sound checks that
// before calling this, because "equally loud in every direction" must not
// depend on a default facing nobody chose.
func coneGain(cone Cone, source, forward, listener m.Vec3) float64 {
	// W3C: if the source orientation is a zero vector, or both cone angles are
	// 360, there is no cone.
	if forward == (m.Vec3{}) || (cone.Inner == 360 && cone.Outer == 360) {
		return 1
	}

	// Normalized source-listener vector. A source standing exactly where the
	// Listener does has no direction to the Listener at all, so it is inside
	// every cone rather than outside one; this is the same degenerate case the
	// azimuth guards, met from the other side.
	sourceToListener := listener.Sub(source)
	if sourceToListener == (m.Vec3{}) {
		return 1
	}
	sourceToListener = sourceToListener.Normalize()

	// Angle between the source orientation vector and the source-listener
	// vector, in degrees. The dot is clamped because two unit vectors in
	// float32 can produce a product a hair outside [-1, 1], and math.Acos
	// answers NaN there.
	angle := 180 * math.Acos(clampCosine(float64(sourceToListener.Dot(forward)))) / math.Pi
	absAngle := math.Abs(angle)

	// Halved here because the API is the entire angle and not the half-angle.
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
		// Between inner and outer cones: inner -> outer, x goes 0 -> 1.
		x := (absAngle - absInner) / (absOuter - absInner)
		return (1-x)*1 + x*outerGain
	}
}

// azimuthElevation is W3C's azimuth and elevation of a source about a
// Listener, in degrees. front and up are the Listener's own axes.
//
// Azimuth comes out on the landmarks exactly: a source ahead is 0, to the
// right +90, to the left -90, and behind -180. Elevation is computed and
// retained and no equalpower gain reads it - W3C's equalpower panner takes
// azimuth alone, and elevation is what an HRTF effort would want.
func azimuthElevation(source, listener, front, up m.Vec3) (azimuth, elevation float64) {
	// Calculate the source-listener vector. W3C handles the degenerate case of
	// a source at the Listener's exact position by answering 0 and 0 rather
	// than normalizing a zero vector, and so does this.
	sourceListener := source.Sub(listener)
	if sourceListener == (m.Vec3{}) {
		return 0, 0
	}
	sourceListener = sourceListener.Normalize()

	// Align axes. The up vector the projection uses is the listener's up made
	// orthogonal to its front, which is why it is rebuilt from the right
	// vector rather than taken as given.
	listenerRight := front.Cross(up).Normalize()
	listenerFront := front.Normalize()
	upVector := listenerRight.Cross(listenerFront)

	upProjection := sourceListener.Dot(upVector)
	projectedSource := sourceListener.Sub(upVector.MulS(upProjection))

	// The zenith snap, stated rather than stumbled into: a source directly
	// overhead or underfoot projects to nothing, and azimuth is 0 there by
	// definition. Never normalize a zero vector.
	if projectedSource != (m.Vec3{}) {
		projectedSource = projectedSource.Normalize()

		azimuth = 180 * math.Acos(clampCosine(float64(projectedSource.Dot(listenerRight)))) / math.Pi

		// Source in front of or behind the listener.
		if projectedSource.Dot(listenerFront) < 0 {
			azimuth = 360 - azimuth
		}

		// Make azimuth relative to "front" and not "right" listener vector.
		if azimuth >= 0 && azimuth <= 270 {
			azimuth = 90 - azimuth
		} else {
			azimuth = 450 - azimuth
		}
	}

	elevation = 90 - 180*math.Acos(clampCosine(float64(sourceListener.Dot(upVector))))/math.Pi
	switch {
	case elevation > 90:
		elevation = 180 - elevation
	case elevation < -90:
		elevation = -180 - elevation
	}
	return azimuth, elevation
}

// equalPowerGains is W3C's equalpower panning, as the source-to-output matrix
// the seam carries: Gains[src][out], row 0 being a mono Clip's only row.
//
// A stereo Clip keeps both channels and shifts weight between them rather than
// being mixed down to mono first, because mixing down is a departure a
// PannerNode backend would have to imitate by hand. What that does, in W3C's
// own terms, is pass one channel at unity and bleed the other into it: a stereo
// source panned hard right is its left channel folded into its right, not its
// left channel silenced.
//
// It is a 2x2 matrix and not a scalar pan for exactly that reason - a scalar
// cannot say outL = inL + inR*cos(x) - and a non-positional Voice runs the same
// equations at azimuth 0, which is what "heard centred" means precisely.
func equalPowerGains(azimuth float64, stereo bool) [2][2]float32 {
	// First, clamp azimuth to the allowed range of [-180, 180].
	azimuth = math.Max(-180, azimuth)
	azimuth = math.Min(180, azimuth)

	// Then wrap to the range [-90, 90]. This is the fold behind: a source
	// passing behind pans identically to its mirror image in front, because
	// two speakers cannot say behind and W3C folds rather than inventing a cue.
	switch {
	case azimuth < -90:
		azimuth = -180 - azimuth
	case azimuth > 90:
		azimuth = 180 - azimuth
	}

	// A normalized value x is calculated from azimuth.
	var x float64
	switch {
	case !stereo:
		x = (azimuth + 90) / 180
	case azimuth <= 0:
		// Inputs L and R move from -90 -> 0 to 0 -> +90.
		x = (azimuth + 90) / 90
	default:
		x = azimuth / 90
	}

	gainLeft := math.Cos(x * math.Pi / 2)
	gainRight := math.Sin(x * math.Pi / 2)

	var gains [2][2]float32
	switch {
	case !stereo:
		// outputL = input * gainL; outputR = input * gainR.
		gains[0][0], gains[0][1] = float32(gainLeft), float32(gainRight)
	case azimuth <= 0:
		// outputL = inputL + inputR * gainL; outputR = inputR * gainR.
		gains[0][0], gains[1][0] = 1, float32(gainLeft)
		gains[1][1] = float32(gainRight)
	default:
		// outputL = inputL * gainL; outputR = inputR + inputL * gainR.
		gains[0][0] = float32(gainLeft)
		gains[0][1], gains[1][1] = float32(gainRight), 1
	}
	return gains
}

// clampCosine holds a dot product of two unit vectors inside math.Acos's
// domain. It is not W3C's - it is float32's, since two vectors normalized in
// single precision can dot to a hair over 1, where math.Acos answers NaN and a
// NaN azimuth reaches a device as a NaN gain.
func clampCosine(cosine float64) float64 {
	return math.Max(-1, math.Min(1, cosine))
}
