package gfximpl

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

func TestSamplerDescZeroValueIsClampAndLinear(t *testing.T) {
	var desc gpu.SamplerDesc
	if desc.AddressU != gpu.AddressClamp || desc.AddressV != gpu.AddressClamp {
		t.Errorf("zero address = (%v, %v), want clamp on both axes", desc.AddressU, desc.AddressV)
	}
	if desc.Mag != gpu.FilterLinear || desc.Min != gpu.FilterLinear || desc.Mip != gpu.FilterLinear {
		t.Errorf("zero filters = (%v, %v, %v), want linear throughout", desc.Mag, desc.Min, desc.Mip)
	}
	if desc.Anisotropy != 0 || desc.Comparison {
		t.Errorf("zero desc = %+v, want no anisotropy and no comparison", desc)
	}
	// Comparability is what makes the translator's dedup map work, so five
	// samplers on one material cost one GPU object each at most.
	deduped := map[gpu.SamplerDesc]int{desc: 1, {AddressU: gpu.AddressRepeat}: 2}
	if len(deduped) != 2 {
		t.Errorf("sampler dedup map = %v, want two distinct keys", deduped)
	}
}

// samplerOps reports the (group, binding) of every sampler bind the backend saw.
func samplerOps(backend *fakeBackend) [][2]int {
	var binds [][2]int
	for _, op := range backend.lastOps {
		if op.kind == opSetSampler {
			binds = append(binds, [2]int{op.group, op.binding})
		}
	}
	return binds
}

func TestEveryReflectedSamplerBindsIndependentlyByName(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	// A material with a tiling texture beside a clamped one: two samplers, two
	// textures, all in one bind group.
	backend := &fakeBackend{layout: &gpu.ShaderLayout{
		UniformSize: 64, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gpu.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gpu.ShaderResource{
			{Name: "groundSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "groundTexture", Group: 1, Binding: 1},
			{Name: "decalSampler", Sampler: true, Group: 1, Binding: 2},
			{Name: "decalTexture", Group: 1, Binding: 3},
		},
	}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	material := testMaterial(
		gfx.SamplerParam("groundSampler", gpu.SamplerDesc{AddressU: gpu.AddressRepeat, AddressV: gpu.AddressRepeat}),
		gfx.SamplerParam("decalSampler", gpu.SamplerDesc{}),
		gfx.TextureParam("groundTexture", gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8Srgb, []byte{1, 2, 3, 4}, true, false)),
		gfx.TextureParam("decalTexture", gfx.TextureWithBytes(1, 1, gpu.FormatRGBA8Srgb, []byte{5, 6, 7, 8}, true, false)),
	)
	w := recordList(t, k)
	w.Draw(triangle(), material, gfx.MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[gfx.PresentCmd](gfx.PresentRequest{})
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
