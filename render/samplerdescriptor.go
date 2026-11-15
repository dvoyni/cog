package render

import (
	"cmp"
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// maxLodClamp is the highest mip level a sampler reads by WebGPU's default,
// which a zero SamplerDescriptor.LodMaxClamp means.
const maxLodClamp = 32

// SamplerDescriptor is a sampler, given by value and shared between every
// parameter given an equal one. Its zero value is WebGPU's default sampler:
// clamping to the edge, filtering nearest between texels and mip levels,
// reading every mip level, with no comparison and no anisotropy. A zero
// LodMaxClamp means 32, every level: a parameter reading the first level
// alone binds a view of it. A zero MaxAnisotropy means one; above one, every
// filter is linear. A comparison sampler, whose Compare is not zero, fills
// only a comparison binding, and only it does; a binding of a sampler that
// does not filter takes one whose filters are all nearest.
type SamplerDescriptor struct {
	AddressModeU, AddressModeV, AddressModeW AddressMode
	MagFilter, MinFilter, MipmapFilter       FilterMode
	LodMinClamp, LodMaxClamp                 float32
	Compare                                  CompareFunction
	MaxAnisotropy                            uint16
}

// makeGfxSamplerDescriptor answers d as gfx's descriptor, each zero spelt as
// its default. An error says why it does not fit binding, a sampler
// binding: a comparison sampler for another binding or another sampler for
// a comparison one, a filtering sampler for one that does not filter, or a
// sampler gfx cannot make, such as one with anisotropy and a nearest filter.
func (d SamplerDescriptor) makeGfxSamplerDescriptor(binding gfx.ShaderBinding) (gfx.SamplerDescriptor, error) {
	descriptor := gfx.SamplerDescriptor{
		AddressModeU: cmp.Or(gfx.AddressMode(d.AddressModeU), gfx.AddressModeClampToEdge),
		AddressModeV: cmp.Or(gfx.AddressMode(d.AddressModeV), gfx.AddressModeClampToEdge),
		AddressModeW: cmp.Or(gfx.AddressMode(d.AddressModeW), gfx.AddressModeClampToEdge),
		MagFilter:    cmp.Or(gfx.FilterMode(d.MagFilter), gfx.FilterModeNearest),
		MinFilter:    cmp.Or(gfx.FilterMode(d.MinFilter), gfx.FilterModeNearest),
		MipmapFilter: cmp.Or(gfx.FilterMode(d.MipmapFilter), gfx.FilterModeNearest),
		LodMinClamp:  d.LodMinClamp,
		LodMaxClamp:  cmp.Or(d.LodMaxClamp, maxLodClamp),
		Compare:      gfx.CompareFunction(d.Compare),
		Anisotropy:   max(d.MaxAnisotropy, 1),
	}
	filtering := descriptor.MagFilter == gfx.FilterModeLinear || descriptor.MinFilter == gfx.FilterModeLinear || descriptor.MipmapFilter == gfx.FilterModeLinear
	switch {
	case descriptor.AddressModeU > gfx.AddressModeMirrorRepeat || descriptor.AddressModeV > gfx.AddressModeMirrorRepeat || descriptor.AddressModeW > gfx.AddressModeMirrorRepeat:
		return gfx.SamplerDescriptor{}, errors.New("is a sampler of an address mode gfx has none of")
	case descriptor.MagFilter > gfx.FilterModeLinear || descriptor.MinFilter > gfx.FilterModeLinear || descriptor.MipmapFilter > gfx.FilterModeLinear:
		return gfx.SamplerDescriptor{}, errors.New("is a sampler of a filter gfx has none of")
	case descriptor.Anisotropy > 1 && !(descriptor.MagFilter == gfx.FilterModeLinear && descriptor.MinFilter == gfx.FilterModeLinear && descriptor.MipmapFilter == gfx.FilterModeLinear):
		return gfx.SamplerDescriptor{}, fmt.Errorf("is a sampler of anisotropy %d with a nearest filter, where every filter of one above 1 is linear", descriptor.Anisotropy)
	case (binding.Sampler.Type == gfx.SamplerBindingTypeComparison) != (descriptor.Compare != gfx.CompareFunctionUndefined):
		return gfx.SamplerDescriptor{}, fmt.Errorf("is a sampler of comparison %d, where its binding compares: %t", descriptor.Compare, binding.Sampler.Type == gfx.SamplerBindingTypeComparison)
	case binding.Sampler.Type == gfx.SamplerBindingTypeNonFiltering && filtering:
		return gfx.SamplerDescriptor{}, errors.New("is a sampler that filters linearly, where its binding's filters are nearest")
	}
	return descriptor, nil
}

// makeInspectedSamplerDescriptor answers descriptor, gfx's, in render's
// terms as an inspection writes it down: each field at its default spelt
// zero.
func makeInspectedSamplerDescriptor(descriptor gfx.SamplerDescriptor) SamplerDescriptor {
	return SamplerDescriptor{
		AddressModeU:  AddressMode(spellDefaultZero(descriptor.AddressModeU, gfx.AddressModeClampToEdge)),
		AddressModeV:  AddressMode(spellDefaultZero(descriptor.AddressModeV, gfx.AddressModeClampToEdge)),
		AddressModeW:  AddressMode(spellDefaultZero(descriptor.AddressModeW, gfx.AddressModeClampToEdge)),
		MagFilter:     FilterMode(spellDefaultZero(descriptor.MagFilter, gfx.FilterModeNearest)),
		MinFilter:     FilterMode(spellDefaultZero(descriptor.MinFilter, gfx.FilterModeNearest)),
		MipmapFilter:  FilterMode(spellDefaultZero(descriptor.MipmapFilter, gfx.FilterModeNearest)),
		LodMinClamp:   descriptor.LodMinClamp,
		LodMaxClamp:   spellDefaultZero(descriptor.LodMaxClamp, maxLodClamp),
		Compare:       CompareFunction(descriptor.Compare),
		MaxAnisotropy: spellDefaultZero(descriptor.Anisotropy, 1),
	}
}

// AddressMode is how a sampler reads coordinates outside a texture, each
// mode gfx's of the same name; its zero value is clamping to the edge.
type AddressMode uint32

const (
	AddressModeClampToEdge  = AddressMode(gfx.AddressModeClampToEdge)
	AddressModeRepeat       = AddressMode(gfx.AddressModeRepeat)
	AddressModeMirrorRepeat = AddressMode(gfx.AddressModeMirrorRepeat)
)

// FilterMode is how a sampler filters between texels or mip levels, each
// mode gfx's of the same name; its zero value is nearest.
type FilterMode uint32

const (
	FilterModeNearest = FilterMode(gfx.FilterModeNearest)
	FilterModeLinear  = FilterMode(gfx.FilterModeLinear)
)
