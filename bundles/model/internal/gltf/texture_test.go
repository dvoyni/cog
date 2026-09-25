package gltf

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
)

// glTF specifies the three filters and the two wrap modes independently, which
// is the shape SamplerDesc took to hold them.
func TestModelSamplerMapsWrapAndFilter(t *testing.T) {
	doc := testDoc()
	doc.Samplers = []*gltf.Sampler{{
		WrapS: gltf.WrapMirroredRepeat, WrapT: gltf.WrapClampToEdge,
		MagFilter: gltf.MagNearest, MinFilter: gltf.MinLinearMipMapNearest,
	}}
	sampler := modelSampler(doc, 0)
	if sampler.AddressU != gfx.AddressMirror || sampler.AddressV != gfx.AddressClamp {
		t.Errorf("address = %v/%v, want mirror/clamp", sampler.AddressU, sampler.AddressV)
	}
	if sampler.Mag != gfx.FilterNearest {
		t.Errorf("mag = %v, want nearest", sampler.Mag)
	}
	if sampler.Min != gfx.FilterLinear || sampler.Mip != gfx.FilterNearest {
		t.Errorf("min/mip = %v/%v, want linear/nearest", sampler.Min, sampler.Mip)
	}
	// A texture that names no sampler takes glTF's own default, which is repeat
	// filtered linearly - not SamplerDesc's zero value, which clamps.
	if DefaultSampler.AddressU != gfx.AddressRepeat {
		t.Error("glTF's default wrap is repeat")
	}
}
