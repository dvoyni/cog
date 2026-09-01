package wgpu

import (
	"slices"

	"github.com/gogpu/wgpu"
)

type gfxbBindKind uint8

const (
	gfxbBindUniform gfxbBindKind = iota
	gfxbBindTexture
	gfxbBindSampler
	gfxbBindBuffer
)

type gfxbBindingKey struct {
	// Default WebGPU limits: binding < 1000, buffer offset/size <= 256 MiB.
	id         uint32
	generation uint32
	offset     uint32
	size       uint32
	binding    uint16
	kind       gfxbBindKind
}

type gfxbBindEntry struct {
	key    gfxbBindingKey
	native wgpu.BindGroupEntry
}

type gfxbBindBucketKey struct {
	shader *gfxbShader
	group  int
	hash   uint64
}

type gfxbCachedBindGroup struct {
	bindings []gfxbBindingKey
	group    *wgpu.BindGroup
}

type gfxbBindGroupCache struct {
	entries map[gfxbBindBucketKey][]gfxbCachedBindGroup
	create  func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error)
	release func(*wgpu.BindGroup)
}

func newGfxBindGroupCache(
	create func(*wgpu.BindGroupLayout, []wgpu.BindGroupEntry) (*wgpu.BindGroup, error),
	release func(*wgpu.BindGroup),
) *gfxbBindGroupCache {
	return &gfxbBindGroupCache{
		entries: map[gfxbBindBucketKey][]gfxbCachedBindGroup{},
		create:  create,
		release: release,
	}
}

func (c *gfxbBindGroupCache) get(shader *gfxbShader, group int, entries []gfxbBindEntry) *wgpu.BindGroup {
	if shader == nil || group < 0 || group >= len(shader.bgLayouts) || shader.bgLayouts[group] == nil {
		return nil
	}
	bucketKey := gfxbBindBucketKey{shader: shader, group: group, hash: gfxbBindingsHash(entries)}
	for i := range c.entries[bucketKey] {
		cached := &c.entries[bucketKey][i]
		if gfxbBindingsEqual(cached.bindings, entries) {
			return cached.group
		}
	}
	native := make([]wgpu.BindGroupEntry, len(entries))
	bindings := make([]gfxbBindingKey, len(entries))
	for i := range entries {
		native[i] = entries[i].native
		bindings[i] = entries[i].key
	}
	groupValue, err := c.create(shader.bgLayouts[group], native)
	if err != nil {
		return nil
	}
	c.entries[bucketKey] = append(c.entries[bucketKey], gfxbCachedBindGroup{
		bindings: bindings,
		group:    groupValue,
	})
	return groupValue
}

func (c *gfxbBindGroupCache) invalidateResource(kind gfxbBindKind, id uint32) {
	for bucketKey, bucket := range c.entries {
		kept := bucket[:0]
		for i := range bucket {
			cached := bucket[i]
			if slices.ContainsFunc(cached.bindings, func(binding gfxbBindingKey) bool {
				return binding.kind == kind && binding.id == id
			}) {
				c.release(cached.group)
				continue
			}
			kept = append(kept, cached)
		}
		clear(bucket[len(kept):])
		if len(kept) == 0 {
			delete(c.entries, bucketKey)
		} else {
			c.entries[bucketKey] = kept
		}
	}
}

func (c *gfxbBindGroupCache) invalidateShader(shader *gfxbShader) {
	for bucketKey, bucket := range c.entries {
		if bucketKey.shader != shader {
			continue
		}
		for i := range bucket {
			c.release(bucket[i].group)
		}
		delete(c.entries, bucketKey)
	}
}

func gfxbBindingsEqual(cached []gfxbBindingKey, entries []gfxbBindEntry) bool {
	if len(cached) != len(entries) {
		return false
	}
	for i := range cached {
		if cached[i] != entries[i].key {
			return false
		}
	}
	return true
}

func gfxbBindingsHash(entries []gfxbBindEntry) uint64 {
	const prime uint64 = 1099511628211
	hash := uint64(1469598103934665603)
	mix := func(value uint64) {
		hash ^= value
		hash *= prime
	}
	for i := range entries {
		key := entries[i].key
		mix(uint64(key.binding))
		mix(uint64(key.kind))
		mix(uint64(key.id))
		mix(uint64(key.generation))
		mix(uint64(key.offset))
		mix(uint64(key.size))
	}
	return hash
}
