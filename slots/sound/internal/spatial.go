package internal

// DistanceModel names which of W3C's three distance models a Falloff uses. It
// is W3C's `distanceModel`, and the three members are its `"inverse"`,
// `"linear"` and `"exponential"`.
//
// Inverse is the default, and is zero so that it is the default of a Falloff
// nobody filled in.
type DistanceModel uint8

const (
	// DistanceInverse is W3C's "inverse" model, and the default.
	DistanceInverse DistanceModel = iota
	// DistanceLinear is W3C's "linear" model. It is the only model Max takes
	// part in, and the only one that can reach silence.
	DistanceLinear
	// DistanceExponential is W3C's "exponential" model.
	DistanceExponential
)

// Falloff is how a Positional Voice gets quieter with distance: W3C's
// PannerNode distance parameters, with the prefixes dropped because the struct
// already says which distance.
//
// It is comparable and pointer-free, so a Component can carry one, and it is
// replaced whole by SetVoice: seven separate Maybe fields would buy finer
// updates and cost a fourteen-field Params, for a group nobody changes
// piecemeal.
//
// Its zero value is the W3C defaults, the way m.Transform's zero Scale is the
// identity (libs/m/transform.go): a Component an author never filled in must
// not be silent, and every one of these defaults is a number rather than a
// zero. A partly filled one is taken literally, so Falloff{Ref: 64} is a
// reference distance of 64 with no rolloff and no maximum, and an author who
// wants the defaults with one field moved starts from DefaultFalloff.
//
// Units are the game's. sound never learns a scale, so set Ref to the world's
// own: W3C's default of 1 is metres, and off metre scale it is a trap - a
// source at radius 4 is -12 dB and at radius 10 is -20 dB. Correct, and far
// quieter than an author expects.
type Falloff struct {
	// Model is which of the three models attenuates this Voice. W3C
	// distanceModel, default "inverse".
	Model DistanceModel
	// Ref is the distance at which there is no attenuation at all, and the
	// distance every model measures from. W3C refDistance, default 1.
	Ref float32
	// Max is where the linear model reaches its floor. W3C maxDistance,
	// default 10000. It clamps the linear model alone and never silences a
	// Voice under the other two, which is W3C's oddness and is inherited
	// rather than corrected.
	Max float32
	// Rolloff is how fast the gain falls between Ref and Max. W3C
	// rolloffFactor, default 1. W3C's nominal range is [0, inf), reduced to
	// [0, 1] for the linear model.
	Rolloff float32
}

// DefaultFalloff is the falloff of a Positional Voice that was given none:
// W3C's PannerNode defaults. An author moving one field starts here, so that
// the other three are not silently zeroed.
func DefaultFalloff() Falloff {
	return Falloff{Model: DistanceInverse, Ref: 1, Max: 10000, Rolloff: 1}
}

// Cone is how a Positional Voice gets quieter off its own axis: W3C's
// PannerNode cone parameters, with the prefixes dropped.
//
// It is comparable and pointer-free, and replaced whole, exactly as Falloff is,
// and its zero value is the W3C defaults for the same reason - an all-zero Cone
// read literally is an outer angle of 0 degrees at an outer gain of 0, which is
// a Voice that is silent everywhere.
//
// A cone needs a facing. A Positional Voice with no Orientation is equally loud
// in every direction whatever its Cone says, so a Voice can never be
// accidentally directional along an axis nobody chose; W3C's (1,0,0)
// PannerNode.orientation default is deliberately not inherited.
type Cone struct {
	// Inner is the full angle, in degrees, inside which there is no
	// attenuation. W3C coneInnerAngle, default 360.
	Inner float32
	// Outer is the full angle, in degrees, outside which OuterGain applies.
	// W3C coneOuterAngle, default 360.
	Outer float32
	// OuterGain is the gain outside the outer cone. W3C coneOuterGain, default
	// 0.
	OuterGain float32
}

// DefaultCone is the cone of a Voice that was given none: W3C's PannerNode
// defaults, which are 360 degrees of inner cone and so no cone at all.
func DefaultCone() Cone { return Cone{Inner: 360, Outer: 360} }

// resolve reads an all-zero Falloff as the W3C defaults and any other as
// written, which is m.Transform.scale's rule one Slot over.
func (f Falloff) resolve() Falloff {
	if f == (Falloff{}) {
		return DefaultFalloff()
	}
	return f
}

// resolve reads an all-zero Cone as the W3C defaults and any other as written.
func (c Cone) resolve() Cone {
	if c == (Cone{}) {
		return DefaultCone()
	}
	return c
}
