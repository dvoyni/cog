package types

// SamplerDesc describes a sampler to create. Its zero value clamps both axes
// and filters linearly at every step, and it stays comparable so the translator
// can dedup identical samplers - a glTF material with five textures whose
// samplers happen to match costs one GPU object.
type SamplerDesc struct {
	AddressU AddressMode `json:"addressU"`
	AddressV AddressMode `json:"addressV"`
	// Mag, Min and Mip are separate because glTF specifies magnification,
	// minification and mip selection independently. Zero is FilterLinear.
	Mag FilterMode `json:"mag"`
	Min FilterMode `json:"min"`
	Mip FilterMode `json:"mip"`
	// Anisotropy is the maximum anisotropic sample count. 0 and 1 both mean
	// off, and it is clamped to 16. WebGPU requires all three filters linear
	// whenever it is above 1.
	Anisotropy uint8 `json:"anisotropy,omitempty"`
	// Comparison makes this a comparison sampler, which a shadow map needs and
	// which cannot be the same object as a colour sampler.
	Comparison bool        `json:"comparison,omitempty"`
	Compare    CompareFunc `json:"compare,omitempty"`
	Label      string      `json:"label,omitempty"`
}
