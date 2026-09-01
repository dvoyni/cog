package wgpu

import (
	"testing"

	"github.com/gogpu/wgpu"
)

func BenchmarkGfxBindGroupCacheHit(b *testing.B) {
	cache := newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			return &wgpu.BindGroup{}, nil
		},
		func(*wgpu.BindGroup) {},
	)
	shader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}}}
	entries := []gfxbBindEntry{
		{key: gfxbBindingKey{kind: gfxbBindUniform, binding: 0, id: 1, size: 256}},
		{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 1, id: 2, generation: 1}},
		{key: gfxbBindingKey{kind: gfxbBindSampler, binding: 2, id: 3}},
		{key: gfxbBindingKey{kind: gfxbBindBuffer, binding: 3, id: 4, generation: 1, offset: 8, size: 16}},
	}
	cache.get(shader, 0, entries)
	b.ReportAllocs()
	for b.Loop() {
		cache.get(shader, 0, entries)
	}
}

func TestGfxBindGroupCacheReusesAndInvalidatesResources(t *testing.T) {
	created := 0
	released := 0
	cache := newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			created++
			return &wgpu.BindGroup{}, nil
		},
		func(*wgpu.BindGroup) { released++ },
	)
	shader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}}}
	entries := []gfxbBindEntry{
		{key: gfxbBindingKey{kind: gfxbBindUniform, binding: 0, id: 2}},
		{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 1, id: 7, generation: 1}},
	}

	first := cache.get(shader, 0, entries)
	second := cache.get(shader, 0, entries)
	if first == nil || second != first || created != 1 {
		t.Fatalf("cache reuse = (%p, %p, %d creates), want same non-nil group and 1 create", first, second, created)
	}

	cache.invalidateResource(gfxbBindTexture, 7)
	third := cache.get(shader, 0, entries)
	if third == nil || third == first || created != 2 || released != 1 {
		t.Fatalf("cache after invalidation = (%p, %d creates, %d releases), want new group, 2 creates, 1 release", third, created, released)
	}
}

func TestGfxBindGroupCacheKeysAllBindingState(t *testing.T) {
	created := 0
	cache := newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			created++
			return &wgpu.BindGroup{}, nil
		},
		func(*wgpu.BindGroup) {},
	)
	shader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}, {}}}
	otherShader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}, {}}}
	base := []gfxbBindEntry{
		{key: gfxbBindingKey{kind: gfxbBindUniform, binding: 0, id: 1, size: 256}},
		{key: gfxbBindingKey{kind: gfxbBindTexture, binding: 1, id: 2, generation: 1}},
		{key: gfxbBindingKey{kind: gfxbBindSampler, binding: 2, id: 3}},
		{key: gfxbBindingKey{kind: gfxbBindBuffer, binding: 3, id: 4, generation: 1, offset: 8, size: 16}},
	}
	cache.get(shader, 0, base)

	tests := []struct {
		name   string
		shader *gfxbShader
		group  int
		mutate func([]gfxbBindEntry)
	}{
		{name: "shader", shader: otherShader},
		{name: "group", group: 1},
		{name: "uniform slot", mutate: func(entries []gfxbBindEntry) { entries[0].key.id++ }},
		{name: "texture id", mutate: func(entries []gfxbBindEntry) { entries[1].key.id++ }},
		{name: "texture generation", mutate: func(entries []gfxbBindEntry) { entries[1].key.generation++ }},
		{name: "sampler", mutate: func(entries []gfxbBindEntry) { entries[2].key.id++ }},
		{name: "buffer id", mutate: func(entries []gfxbBindEntry) { entries[3].key.id++ }},
		{name: "buffer generation", mutate: func(entries []gfxbBindEntry) { entries[3].key.generation++ }},
		{name: "buffer offset", mutate: func(entries []gfxbBindEntry) { entries[3].key.offset++ }},
		{name: "buffer size", mutate: func(entries []gfxbBindEntry) { entries[3].key.size++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := append([]gfxbBindEntry(nil), base...)
			if test.mutate != nil {
				test.mutate(entries)
			}
			gotShader := test.shader
			if gotShader == nil {
				gotShader = shader
			}
			before := created
			cache.get(gotShader, test.group, entries)
			if created != before+1 {
				t.Fatalf("bind group creates = %d, want %d", created, before+1)
			}
		})
	}
}

func TestGfxBindGroupCacheInvalidatesShader(t *testing.T) {
	created := 0
	released := 0
	cache := newGfxBindGroupCache(
		func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			created++
			return &wgpu.BindGroup{}, nil
		},
		func(*wgpu.BindGroup) { released++ },
	)
	shader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}, {}}}
	otherShader := &gfxbShader{bgLayouts: []*wgpu.BindGroupLayout{{}}}
	entries := []gfxbBindEntry{{key: gfxbBindingKey{kind: gfxbBindUniform, binding: 0, id: 1}}}
	cache.get(shader, 0, entries)
	cache.get(shader, 1, entries)
	other := cache.get(otherShader, 0, entries)

	cache.invalidateShader(shader)
	if released != 2 {
		t.Fatalf("released shader groups = %d, want 2", released)
	}
	before := created
	if got := cache.get(otherShader, 0, entries); got != other || created != before {
		t.Fatal("shader invalidation removed another shader's bind group")
	}
}
