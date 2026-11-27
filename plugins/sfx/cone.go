package sfx

// Cone is the directions a positional voice is loud in: W3C's PannerNode cone
// parameters, their prefixes dropped. The angles are whole angles, as W3C's
// are, so a cone of 90 is loud up to 45 degrees either side of the voice's
// forward. Each voice carries its own, given whole.
//
// It is comparable and pointer-free, so a component can carry one. An
// all-zero Cone is W3C's defaults, since read literally it would be a voice
// silent everywhere; a partly filled one is taken literally. An author who
// wants the defaults with one field moved starts from MakeDefaultCone.
//
// A cone needs a facing. A positional voice with no Orientation is equally
// loud in every direction whatever its Cone says, so a voice is never
// directional along an axis nobody chose: W3C's (1,0,0) default orientation
// is not inherited.
type Cone struct {
	// Inner is the whole angle, in degrees, inside which there is no
	// attenuation. W3C coneInnerAngle, default 360.
	Inner float32
	// Outer is the whole angle, in degrees, outside which the gain is
	// OuterGain. W3C coneOuterAngle, default 360.
	Outer float32
	// OuterGain is the gain outside the outer angle. W3C coneOuterGain,
	// default 0.
	OuterGain float32
}

// MakeDefaultCone answers W3C's PannerNode defaults: 360 degrees inside and
// out, which is no cone at all.
func MakeDefaultCone() Cone { return Cone{Inner: 360, Outer: 360} }

// resolve answers an all-zero cone as W3C's defaults, and any other as it is
// written.
func (c Cone) resolve() Cone {
	if c == (Cone{}) {
		return MakeDefaultCone()
	}
	return c
}
