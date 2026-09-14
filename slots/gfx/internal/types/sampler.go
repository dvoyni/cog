package types

// AddressMode selects how texture coordinates outside [0,1] are sampled on one
// axis. It is an enum rather than a bitmask because mirroring is a third mode,
// not a combination of the other two, and a flag that reads as a combination is
// a flag that gets silently reinterpreted.
type AddressMode uint8

const (
	AddressClamp AddressMode = iota
	AddressRepeat
	AddressMirror
)

// Name spells the address mode for a debug document.
func (mode AddressMode) Name() string {
	switch mode {
	case AddressClamp:
		return "clamp"
	case AddressRepeat:
		return "repeat"
	case AddressMirror:
		return "mirror"
	}
	return UnknownName(int(mode))
}

// FilterMode selects texture minification/magnification filtering.
type FilterMode uint8

const (
	FilterLinear FilterMode = iota
	FilterNearest
)

// Name spells the filter for a debug document. canvas reports one on every
// sprite transform, so a sampler filter reaches an agent in one spelling
// whichever tool showed it.
func (mode FilterMode) Name() string {
	switch mode {
	case FilterLinear:
		return "linear"
	case FilterNearest:
		return "nearest"
	}
	return UnknownName(int(mode))
}

// SamplerDesc describes a sampler to create. Its zero value clamps both axes
// and filters linearly at every step, and it stays comparable so the translator
// can dedup identical samplers - a glTF material with five textures whose
// samplers happen to match costs one GPU object.
type SamplerDesc struct {
	AddressU, AddressV AddressMode
	// Mag, Min and Mip are separate because glTF specifies magnification,
	// minification and mip selection independently. Zero is FilterLinear.
	Mag, Min, Mip FilterMode
	// Anisotropy is the maximum anisotropic sample count. 0 and 1 both mean
	// off, and it is clamped to 16. WebGPU requires all three filters linear
	// whenever it is above 1.
	Anisotropy uint8
	// Comparison makes this a comparison sampler, which a shadow map needs and
	// which cannot be the same object as a colour sampler.
	Comparison bool
	Compare    CompareFunc
	Label      string
}
