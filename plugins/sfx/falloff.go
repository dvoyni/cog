package sfx

// DistanceModel is which of W3C's three distance models a Falloff uses: W3C's
// PannerNode distanceModel, its "inverse", "linear" and "exponential". The
// inverse model is the default, and zero, so it is the model of a Falloff
// nobody filled in.
type DistanceModel uint8

const (
	// DistanceInverse is W3C's "inverse" model, and the default.
	DistanceInverse DistanceModel = iota
	// DistanceLinear is W3C's "linear" model: the only one Max takes part in,
	// and the only one that can reach silence.
	DistanceLinear
	// DistanceExponential is W3C's "exponential" model.
	DistanceExponential
)

// Falloff is how a positional voice grows quieter with distance from the
// listener: W3C's PannerNode distance parameters, their prefixes dropped,
// since the struct already says which distance. Each voice carries its own,
// given whole.
//
// It is comparable and pointer-free, so a component can carry one. An
// all-zero Falloff is W3C's defaults, as m.Transform's zero scale is the
// identity, so a component an author never filled in is not silent; a partly
// filled one is taken literally, so Falloff{Ref: 64} is a reference distance
// of 64 with no rolloff and no maximum. An author who wants the defaults with
// one field moved starts from MakeDefaultFalloff.
//
// Units are the game's: sfx never learns a scale. Set Ref to the world's
// own. W3C's default of 1 is a trap off metre scale: on the defaults a
// source at a distance of 4 is -12 dB, and at 10 it is -20 dB. That is
// correct, and far quieter than an author expects. A game measured in pixels
// passes its own Falloff, typically a constant it declares once.
type Falloff struct {
	// Model is the model that attenuates the voice. W3C distanceModel,
	// default DistanceInverse.
	Model DistanceModel
	// Ref is the distance below which there is no attenuation, and from which
	// every model measures. W3C refDistance, default 1.
	Ref float32
	// Max is where the linear model reaches its floor. W3C maxDistance,
	// default 10000. It clamps the linear model alone and never silences a
	// voice under the other two, which is W3C's, inherited rather than
	// corrected.
	Max float32
	// Rolloff is how fast the gain falls with distance. W3C rolloffFactor,
	// default 1. A negative one is 0, and the linear model holds it to
	// [0, 1].
	Rolloff float32
}

// MakeDefaultFalloff answers W3C's PannerNode defaults: the inverse model,
// a Ref of 1, a Max of 10000 and a Rolloff of 1. An author moving one field
// starts here, so the other three are not zero.
func MakeDefaultFalloff() Falloff {
	return Falloff{Model: DistanceInverse, Ref: 1, Max: 10000, Rolloff: 1}
}

// resolve answers an all-zero falloff as W3C's defaults, and any other as it
// is written.
func (f Falloff) resolve() Falloff {
	if f == (Falloff{}) {
		return MakeDefaultFalloff()
	}
	return f
}
