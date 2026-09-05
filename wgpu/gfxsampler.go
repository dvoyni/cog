package wgpu

import (
	"fmt"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// maxAnisotropy is the ceiling WebGPU implementations agree on.
const maxAnisotropy = 16

func addressMode(mode cgfx.AddressMode) gputypes.AddressMode {
	switch mode {
	case cgfx.AddressRepeat:
		return gputypes.AddressModeRepeat
	case cgfx.AddressMirror:
		return gputypes.AddressModeMirrorRepeat
	default:
		return gputypes.AddressModeClampToEdge
	}
}

func filterMode(filter cgfx.FilterMode) gputypes.FilterMode {
	if filter == cgfx.FilterNearest {
		return gputypes.FilterModeNearest
	}
	return gputypes.FilterModeLinear
}

// samplerDescriptor translates a sampler description for the device. Anisotropy
// of 0 and 1 both mean off, and anything higher is clamped.
func samplerDescriptor(desc cgfx.SamplerDesc) *wgpu.SamplerDescriptor {
	anisotropy := uint16(max(desc.Anisotropy, 1))
	compare := gputypes.CompareFunctionUndefined
	if desc.Comparison {
		compare = compareFunc(desc.Compare)
	}
	return &wgpu.SamplerDescriptor{
		Label:        desc.Label,
		AddressModeU: addressMode(desc.AddressU),
		AddressModeV: addressMode(desc.AddressV),
		AddressModeW: addressMode(desc.AddressU),
		MagFilter:    filterMode(desc.Mag),
		MinFilter:    filterMode(desc.Min),
		MipmapFilter: filterMode(desc.Mip),
		LodMaxClamp:  1000,
		Compare:      compare,
		Anisotropy:   min(anisotropy, maxAnisotropy),
	}
}

// validateSampler rejects the one combination WebGPU forbids outright:
// anisotropic filtering without linear mag, min and mip. Silently clamping it
// would hide the mistake until a browser refused the sampler.
func validateSampler(desc cgfx.SamplerDesc) error {
	if desc.Anisotropy <= 1 {
		return nil
	}
	if desc.Mag != cgfx.FilterLinear || desc.Min != cgfx.FilterLinear || desc.Mip != cgfx.FilterLinear {
		return fmt.Errorf("wgpu: sampler %q asks for anisotropy %d with a non-linear filter", desc.Label, desc.Anisotropy)
	}
	return nil
}
