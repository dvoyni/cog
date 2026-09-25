package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

func TestSamplerDescZeroValueIsClampAndLinear(t *testing.T) {
	var desc types.SamplerDesc
	if desc.AddressU != types.AddressClamp || desc.AddressV != types.AddressClamp {
		t.Errorf("zero address = (%v, %v), want clamp on both axes", desc.AddressU, desc.AddressV)
	}
	if desc.Mag != types.FilterLinear || desc.Min != types.FilterLinear || desc.Mip != types.FilterLinear {
		t.Errorf("zero filters = (%v, %v, %v), want linear throughout", desc.Mag, desc.Min, desc.Mip)
	}
	if desc.Anisotropy != 0 || desc.Comparison {
		t.Errorf("zero desc = %+v, want no anisotropy and no comparison", desc)
	}
	// Comparability is what makes the translator's dedup map work, so five
	// samplers on one material cost one GPU object each at most.
	deduped := map[types.SamplerDesc]int{desc: 1, {AddressU: types.AddressRepeat}: 2}
	if len(deduped) != 2 {
		t.Errorf("sampler dedup map = %v, want two distinct keys", deduped)
	}
}

// samplerOps reports the (group, binding) of every sampler bind the backend saw.
func samplerOps(backend *fakeBackend) [][2]int {
	var binds [][2]int
	for _, op := range backend.lastOps {
		if op.kind == testOpSetSampler {
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
	backend := &fakeBackend{layout: &shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}},
			{Name: "groundSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "groundTexture", Group: 1, Binding: 1},
			{Name: "decalSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 2},
			{Name: "decalTexture", Group: 1, Binding: 3},
		},
	}}
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	material := testMaterial(
		descriptors.SamplerParam("groundSampler", types.SamplerDesc{AddressU: types.AddressRepeat, AddressV: types.AddressRepeat}),
		descriptors.SamplerParam("decalSampler", types.SamplerDesc{}),
		descriptors.TextureParam("groundTexture", descriptors.TextureWithBytes(1, 1, descriptors.FormatRGBA8Srgb, []byte{1, 2, 3, 4}, true, false)),
		descriptors.TextureParam("decalTexture", descriptors.TextureWithBytes(1, 1, descriptors.FormatRGBA8Srgb, []byte{5, 6, 7, 8}, true, false)),
	)
	w := recordList(t, k)
	w.Draw(triangle(), material, descriptors.MatParam("mvp", m.NewMat4()))
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
