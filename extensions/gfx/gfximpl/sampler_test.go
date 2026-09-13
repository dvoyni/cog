package gfximpl

import (
	"testing"

	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/internal"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

func TestSamplerDescZeroValueIsClampAndLinear(t *testing.T) {
	var desc gfx.SamplerDesc
	if desc.AddressU != gfx.AddressClamp || desc.AddressV != gfx.AddressClamp {
		t.Errorf("zero address = (%v, %v), want clamp on both axes", desc.AddressU, desc.AddressV)
	}
	if desc.Mag != gfx.FilterLinear || desc.Min != gfx.FilterLinear || desc.Mip != gfx.FilterLinear {
		t.Errorf("zero filters = (%v, %v, %v), want linear throughout", desc.Mag, desc.Min, desc.Mip)
	}
	if desc.Anisotropy != 0 || desc.Comparison {
		t.Errorf("zero desc = %+v, want no anisotropy and no comparison", desc)
	}
	// Comparability is what makes the translator's dedup map work, so five
	// samplers on one material cost one GPU object each at most.
	deduped := map[gfx.SamplerDesc]int{desc: 1, {AddressU: gfx.AddressRepeat}: 2}
	if len(deduped) != 2 {
		t.Errorf("sampler dedup map = %v, want two distinct keys", deduped)
	}
}

// samplerOps reports the (group, binding) of every sampler bind the backend saw.
func samplerOps(backend *fakeBackend) [][2]int {
	var binds [][2]int
	for _, op := range backend.lastOps {
		if op.Kind == internal.GpuSetSampler {
			binds = append(binds, [2]int{int(op.Arg0), int(op.Arg1)})
		}
	}
	return binds
}

func TestEveryReflectedSamplerBindsIndependentlyByName(t *testing.T) {
	p := newPlugin()
	k := newTestKernel(t, p)
	// A material with a tiling texture beside a clamped one: two samplers, two
	// textures, all in one bind group.
	backend := &fakeBackend{layout: &gfx.ShaderLayout{
		UniformSize: 64, UniformGroup: 0, UniformBinding: 0,
		Uniforms: []gfx.UniformMember{{Name: "mvp", Offset: 0}},
		Resources: []gfx.ShaderResource{
			{Name: "groundSampler", Sampler: true, Group: 1, Binding: 0},
			{Name: "groundTexture", Group: 1, Binding: 1},
			{Name: "decalSampler", Sampler: true, Group: 1, Binding: 2},
			{Name: "decalTexture", Group: 1, Binding: 3},
		},
	}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	material := testMaterial(
		gfx.SamplerParam("groundSampler", gfx.SamplerDesc{AddressU: gfx.AddressRepeat, AddressV: gfx.AddressRepeat}),
		gfx.SamplerParam("decalSampler", gfx.SamplerDesc{}),
		gfx.TextureParam("groundTexture", gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8Srgb, []byte{1, 2, 3, 4}, true, false)),
		gfx.TextureParam("decalTexture", gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8Srgb, []byte{5, 6, 7, 8}, true, false)),
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
