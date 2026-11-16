package render

import (
	"bytes"
	"fmt"

	"github.com/dvoyni/cog/plugins/gfx"
)

// zeroBufferSize is the size of the zero buffer every uniform block and
// storage buffer left unbound shares, the largest uniform block WebGPU's
// default limits allow; a storage buffer larger still gets one of its own
// size.
const zeroBufferSize = 64 << 10

// multisampledDefaultSampleCount is the sample count of the textures a
// multisampled binding left unbound reads.
const multisampledDefaultSampleCount = 4

// defaultLabel labels every gfx object a binding left unbound draws with.
const defaultLabel = "render: the default"

// bindingResourceCache is the gfx objects bindings share, each made the first
// time a binding needs it and kept for the program's life: the samplers
// parameters bind, one for each descriptor, and what a binding left unbound
// draws with, a float texture of one white texel in every view dimension, a
// depth texture at depth 1.0, integer textures of zeros, a placeholder of
// each storage texture's format, a linear sampler clamping to the edge, a
// nearest one for a binding that does not filter, a less-equal comparison
// sampler, and buffers of zeros. A multisampled texture, which no write
// fills, holds zeros. The asset table's mutex guards it.
type bindingResourceCache struct {
	samplers     map[gfx.SamplerDescriptor]gfx.Sampler
	textures     map[defaultTextureKey]gfx.Texture
	textureViews map[defaultTextureViewKey]gfx.TextureView
	zeroBuffers  map[uint64]gfx.Buffer
}

// findSampler answers the sampler of descriptor, made the first time and
// shared by every binding of it.
func (r *bindingResourceCache) findSampler(device gfx.Device, descriptor gfx.SamplerDescriptor) (gfx.Sampler, error) {
	if sampler, ok := r.samplers[descriptor]; ok {
		return sampler, nil
	}
	sampler, err := device.CreateSampler(descriptor)
	if err != nil {
		return gfx.Sampler{}, err
	}
	if r.samplers == nil {
		r.samplers = map[gfx.SamplerDescriptor]gfx.Sampler{}
	}
	r.samplers[descriptor] = sampler
	return sampler, nil
}

// makeEntry answers the entry of layoutEntry's binding as it draws unbound,
// at the size of a buffer's binding, making what it binds the first time.
func (r *bindingResourceCache) makeEntry(device gfx.Device, layoutEntry gfx.BindGroupLayoutEntry) (gfx.BindGroupEntry, error) {
	entry := gfx.BindGroupEntry{Binding: layoutEntry.Binding}
	var err error
	switch {
	case layoutEntry.Buffer.Type != gfx.BufferBindingTypeUndefined:
		entry.Size = max((layoutEntry.Buffer.MinBindingSize+3)&^3, 4)
		entry.Buffer, err = r.findZeroBuffer(device, entry.Size)
	case layoutEntry.Sampler.Type != gfx.SamplerBindingTypeUndefined:
		entry.Sampler, err = r.findSampler(device, makeDefaultSamplerDescriptor(layoutEntry.Sampler.Type))
	case layoutEntry.Texture.SampleType != gfx.TextureSampleTypeUndefined:
		sampleType := layoutEntry.Texture.SampleType
		if sampleType == gfx.TextureSampleTypeUnfilterableFloat {
			sampleType = gfx.TextureSampleTypeFloat
		}
		entry.TextureView, err = r.findTextureView(device, defaultTextureViewKey{defaultTextureKind: defaultTextureKind{sampleType: sampleType, multisampled: layoutEntry.Texture.Multisampled}, dimension: layoutEntry.Texture.ViewDimension})
	default:
		entry.TextureView, err = r.findTextureView(device, defaultTextureViewKey{defaultTextureKind: defaultTextureKind{storageFormat: layoutEntry.StorageTexture.Format}, dimension: layoutEntry.StorageTexture.ViewDimension})
	}
	return entry, err
}

// findZeroBuffer answers a buffer of zeros of size bytes or more.
func (r *bindingResourceCache) findZeroBuffer(device gfx.Device, size uint64) (gfx.Buffer, error) {
	size = max(size, zeroBufferSize)
	if buffer, ok := r.zeroBuffers[size]; ok {
		return buffer, nil
	}
	buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Label: defaultLabel + " zeros", Size: size, Usage: gfx.BufferUsageFlagUniform | gfx.BufferUsageFlagStorage})
	if err != nil {
		return gfx.Buffer{}, err
	}
	if r.zeroBuffers == nil {
		r.zeroBuffers = map[uint64]gfx.Buffer{}
	}
	r.zeroBuffers[size] = buffer
	return buffer, nil
}

// findTextureView answers the default texture view key names, of the
// texture of its kind and its dimension's texture dimension, making both the
// first time.
func (r *bindingResourceCache) findTextureView(device gfx.Device, key defaultTextureViewKey) (gfx.TextureView, error) {
	if view, ok := r.textureViews[key]; ok {
		return view, nil
	}
	textureKey := defaultTextureKey{defaultTextureKind: key.defaultTextureKind, dimension: gfx.TextureDimension2D}
	switch key.dimension {
	case gfx.TextureViewDimension1D:
		textureKey.dimension = gfx.TextureDimension1D
	case gfx.TextureViewDimension3D:
		textureKey.dimension = gfx.TextureDimension3D
	}
	texture, err := r.findTexture(device, textureKey)
	if err != nil {
		return gfx.TextureView{}, err
	}
	layerCount := uint32(1)
	switch key.dimension {
	case gfx.TextureViewDimension2DArray, gfx.TextureViewDimensionCube, gfx.TextureViewDimensionCubeArray:
		layerCount = device.TextureDescriptor(texture).Size.DepthOrArrayLayers
	}
	aspect := gfx.TextureAspectAll
	if key.sampleType == gfx.TextureSampleTypeDepth {
		aspect = gfx.TextureAspectDepthOnly
	}
	view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{Dimension: key.dimension, Aspect: aspect, MipLevelCount: 1, ArrayLayerCount: layerCount})
	if err != nil {
		return gfx.TextureView{}, err
	}
	if r.textureViews == nil {
		r.textureViews = map[defaultTextureViewKey]gfx.TextureView{}
	}
	r.textureViews[key] = view
	return view, nil
}

// findTexture answers the texture key names, made the first time, as
// key.makeDescriptor describes it, and filled with its texel.
func (r *bindingResourceCache) findTexture(device gfx.Device, key defaultTextureKey) (gfx.Texture, error) {
	if texture, ok := r.textures[key]; ok {
		return texture, nil
	}
	descriptor, texel := key.makeDescriptor()
	texture, err := device.CreateTexture(descriptor)
	if err != nil {
		return gfx.Texture{}, err
	}
	if err := writeDefaultTexture(device, texture, descriptor, texel); err != nil {
		device.ReleaseTexture(texture)
		return gfx.Texture{}, err
	}
	if r.textures == nil {
		r.textures = map[defaultTextureKey]gfx.Texture{}
	}
	r.textures[key] = texture
	return texture, nil
}

// refillTextures fills every default texture made so far with its texel
// again, after a device loss left it empty. The error is gfx's, the first
// a write answers.
func (r *bindingResourceCache) refillTextures(device gfx.Device) error {
	for key, texture := range r.textures {
		descriptor, texel := key.makeDescriptor()
		if err := writeDefaultTexture(device, texture, descriptor, texel); err != nil {
			return err
		}
	}
	return nil
}

// defaultTextureKind is the kind of default texture a binding left unbound
// reads: its sample type, Float for every float binding, or none for a
// storage texture of storageFormat; and whether it is multisampled.
type defaultTextureKind struct {
	sampleType    gfx.TextureSampleType
	storageFormat gfx.TextureFormat
	multisampled  bool
}

// defaultTextureViewKey is which default texture view a binding left unbound
// reads: its kind, and its view dimension.
type defaultTextureViewKey struct {
	defaultTextureKind
	dimension gfx.TextureViewDimension
}

// writeDefaultTexture fills every layer of texture, a default texture of
// descriptor, with texel; nothing for no texel, as a new texture holds zeros.
func writeDefaultTexture(device gfx.Device, texture gfx.Texture, descriptor gfx.TextureDescriptor, texel []byte) error {
	if texel == nil {
		return nil
	}
	layers := descriptor.Size.DepthOrArrayLayers
	err := device.WriteTexture(gfx.ImageCopyTexture{Texture: texture, Aspect: gfx.TextureAspectAll}, bytes.Repeat(texel, int(layers)),
		gfx.ImageDataLayout{BytesPerRow: uint32(len(texel)), RowsPerImage: 1}, descriptor.Size)
	if err != nil {
		return fmt.Errorf("writing the default texture: %w", err)
	}
	return nil
}

// makeDefaultSamplerDescriptor answers the sampler a binding of samplerType
// draws unbound with: linear, nearest for one that does not filter, and a
// less-equal comparison for a comparison one, each clamping to the edge,
// labelled apart from every sampler a parameter binds.
func makeDefaultSamplerDescriptor(samplerType gfx.SamplerBindingType) gfx.SamplerDescriptor {
	sampler := SamplerDescriptor{MagFilter: FilterModeLinear, MinFilter: FilterModeLinear, MipmapFilter: FilterModeLinear}
	switch samplerType {
	case gfx.SamplerBindingTypeNonFiltering:
		sampler = SamplerDescriptor{}
	case gfx.SamplerBindingTypeComparison:
		sampler.Compare = CompareFunctionLessEqual
	}
	descriptor, _ := sampler.makeGfxSamplerDescriptor(gfx.ShaderBinding{Sampler: gfx.SamplerBindingLayout{Type: samplerType}})
	descriptor.Label = defaultLabel + " sampler"
	return descriptor
}
