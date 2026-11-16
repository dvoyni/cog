package render

import (
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
)

func TestASamplerSpellsItsZerosAsWebGPUsDefaultsAndIsRefusedForABindingItDoesNotFit(t *testing.T) {
	filtering := gfx.ShaderBinding{Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}}
	nonFiltering := gfx.ShaderBinding{Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeNonFiltering}}
	comparison := gfx.ShaderBinding{Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeComparison}}
	got, err := SamplerDescriptor{}.makeGfxSamplerDescriptor(filtering)
	want := gfx.SamplerDescriptor{
		AddressModeU: gfx.AddressModeClampToEdge, AddressModeV: gfx.AddressModeClampToEdge, AddressModeW: gfx.AddressModeClampToEdge,
		MagFilter: gfx.FilterModeNearest, MinFilter: gfx.FilterModeNearest, MipmapFilter: gfx.FilterModeNearest, LodMaxClamp: 32, Anisotropy: 1,
	}
	if err != nil || got != want {
		t.Errorf("the zero sampler answered %+v, %v, want %+v", got, err, want)
	}
	linear := SamplerDescriptor{MagFilter: FilterModeLinear}
	for name, test := range map[string]struct {
		sampler SamplerDescriptor
		binding gfx.ShaderBinding
		wantErr bool
	}{
		"a nearest sampler for a binding that does not filter": {binding: nonFiltering},
		"a linear sampler for a binding that does not filter":  {sampler: linear, binding: nonFiltering, wantErr: true},
		"a comparison sampler for a filtering binding":         {sampler: SamplerDescriptor{Compare: CompareFunctionLess}, binding: filtering, wantErr: true},
		"another sampler for a comparison binding":             {binding: comparison, wantErr: true},
		"a comparison sampler for a comparison binding":        {sampler: SamplerDescriptor{Compare: CompareFunctionLess}, binding: comparison},
		"anisotropy with a nearest filter":                     {sampler: SamplerDescriptor{MaxAnisotropy: 4, MagFilter: FilterModeLinear}, binding: filtering, wantErr: true},
		"an address mode gfx has none of":                      {sampler: SamplerDescriptor{AddressModeV: 9}, binding: filtering, wantErr: true},
	} {
		if _, err := test.sampler.makeGfxSamplerDescriptor(test.binding); (err != nil) != test.wantErr {
			t.Errorf("%s: answered %v, want an error: %t", name, err, test.wantErr)
		}
	}
}
