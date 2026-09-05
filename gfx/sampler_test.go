package gfx

import (
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

func TestSamplerDescZeroValueIsClampAndLinear(t *testing.T) {
	var desc SamplerDesc
	if desc.AddressU != AddressClamp || desc.AddressV != AddressClamp {
		t.Errorf("zero address = (%v, %v), want clamp on both axes", desc.AddressU, desc.AddressV)
	}
	if desc.Mag != FilterLinear || desc.Min != FilterLinear || desc.Mip != FilterLinear {
		t.Errorf("zero filters = (%v, %v, %v), want linear throughout", desc.Mag, desc.Min, desc.Mip)
	}
	if desc.Anisotropy != 0 || desc.Comparison {
		t.Errorf("zero desc = %+v, want no anisotropy and no comparison", desc)
	}
	// Comparability is what makes the translator's dedup map work, so five
	// samplers on one material cost one GPU object each at most.
	deduped := map[SamplerDesc]int{desc: 1, {AddressU: AddressRepeat}: 2}
	if len(deduped) != 2 {
		t.Errorf("sampler dedup map = %v, want two distinct keys", deduped)
	}
}

// samplerOps reports the (group, binding) of every sampler bind the backend saw.
func samplerOps(backend *fakeBackend) [][2]int {
	var binds [][2]int
	for _, op := range backend.lastOps {
		if op.kind == gpuSetSampler {
			binds = append(binds, [2]int{int(op.arg0), int(op.arg1)})
		}
	}
	return binds
}

func TestEveryReflectedSamplerBindsIndependentlyByName(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	// A material with a tiling texture beside a clamped one: two samplers, two
	// textures, all in one bind group.
	backend := &fakeBackend{layout: &ShaderLayout{
		UniformSize: 64, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []ShaderResource{
			{Name: "groundSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "groundTexture", Group: 1, Binding: 1},
			{Name: "decalSampler", Sampler: true, Group: 1, Binding: 2},
			{Name: "decalTexture", Group: 1, Binding: 3},
		},
	}}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	material := testMaterial(
		SamplerParam("groundSampler", SamplerDesc{AddressU: AddressRepeat, AddressV: AddressRepeat}),
		SamplerParam("decalSampler", SamplerDesc{}),
		TextureParam("groundTexture", TextureWithBytes(1, 1, FormatRGBA8Srgb, []byte{1, 2, 3, 4}, true, false)),
		TextureParam("decalTexture", TextureWithBytes(1, 1, FormatRGBA8Srgb, []byte{5, 6, 7, 8}, true, false)),
	)
	w := recordList(t, k)
	w.Draw(triangle(), material, MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	binds := samplerOps(backend)
	if len(binds) != 2 || binds[0] != [2]int{1, 0} || binds[1] != [2]int{1, 2} {
		t.Fatalf("sampler binds = %v, want group 1 bindings 0 and 2", binds)
	}
	// The two descriptors differ, so they are two GPU samplers; a single shared
	// one would silently mis-sample the tiling texture.
	if backend.samplers != 2 {
		t.Errorf("samplers created = %d, want 2", backend.samplers)
	}
}
