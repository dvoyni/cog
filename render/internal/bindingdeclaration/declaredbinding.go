package bindingdeclaration

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// bufferBindingTypes, samplerBindingTypes, textureSampleTypes,
// textureViewDimensions, storageTextureAccesses and storageTextureFormats
// are the values a declared binding names, by WebGPU's spelling.
var (
	bufferBindingTypes = map[string]gfx.BufferBindingType{
		"uniform":           gfx.BufferBindingTypeUniform,
		"storage":           gfx.BufferBindingTypeStorage,
		"read-only-storage": gfx.BufferBindingTypeReadOnlyStorage,
	}
	samplerBindingTypes = map[string]gfx.SamplerBindingType{
		"filtering":     gfx.SamplerBindingTypeFiltering,
		"non-filtering": gfx.SamplerBindingTypeNonFiltering,
		"comparison":    gfx.SamplerBindingTypeComparison,
	}
	textureSampleTypes = map[string]gfx.TextureSampleType{
		"float":              gfx.TextureSampleTypeFloat,
		"unfilterable-float": gfx.TextureSampleTypeUnfilterableFloat,
		"depth":              gfx.TextureSampleTypeDepth,
		"sint":               gfx.TextureSampleTypeSint,
		"uint":               gfx.TextureSampleTypeUint,
	}
	textureViewDimensions = map[string]gfx.TextureViewDimension{
		"1d":         gfx.TextureViewDimension1D,
		"2d":         gfx.TextureViewDimension2D,
		"2d-array":   gfx.TextureViewDimension2DArray,
		"cube":       gfx.TextureViewDimensionCube,
		"cube-array": gfx.TextureViewDimensionCubeArray,
		"3d":         gfx.TextureViewDimension3D,
	}
	storageTextureAccesses = map[string]gfx.StorageTextureAccess{
		"write-only": gfx.StorageTextureAccessWriteOnly,
		"read-only":  gfx.StorageTextureAccessReadOnly,
		"read-write": gfx.StorageTextureAccessReadWrite,
	}
	storageTextureFormats = map[string]gfx.TextureFormat{
		"r32float":    gfx.TextureFormatR32Float,
		"r32uint":     gfx.TextureFormatR32Uint,
		"r32sint":     gfx.TextureFormatR32Sint,
		"rg32float":   gfx.TextureFormatRG32Float,
		"rg32uint":    gfx.TextureFormatRG32Uint,
		"rg32sint":    gfx.TextureFormatRG32Sint,
		"rgba8unorm":  gfx.TextureFormatRGBA8Unorm,
		"rgba8snorm":  gfx.TextureFormatRGBA8Snorm,
		"rgba8uint":   gfx.TextureFormatRGBA8Uint,
		"rgba8sint":   gfx.TextureFormatRGBA8Sint,
		"bgra8unorm":  gfx.TextureFormatBGRA8Unorm,
		"rgba16uint":  gfx.TextureFormatRGBA16Uint,
		"rgba16sint":  gfx.TextureFormatRGBA16Sint,
		"rgba16float": gfx.TextureFormatRGBA16Float,
		"rgba32float": gfx.TextureFormatRGBA32Float,
		"rgba32uint":  gfx.TextureFormatRGBA32Uint,
		"rgba32sint":  gfx.TextureFormatRGBA32Sint,
	}
)

// declaredBinding is one binding a shader declares, as gfx.ShaderBinding
// holds it: exactly one of Buffer, Sampler, Texture and StorageTexture is
// set.
type declaredBinding struct {
	Group          uint32                  `json:"group"`
	Binding        uint32                  `json:"binding"`
	Name           string                  `json:"name"`
	Buffer         *declaredBuffer         `json:"buffer,omitempty"`
	Sampler        *declaredSampler        `json:"sampler,omitempty"`
	Texture        *declaredTexture        `json:"texture,omitempty"`
	StorageTexture *declaredStorageTexture `json:"storageTexture,omitempty"`
	EntryPoints    []string                `json:"entryPoints,omitempty"`
}

// makeDeclaredBinding answers shaderBinding as a declaration spells it, its
// kind the one of its layouts that is set, or the error naming what no name
// spells, a binding of no kind among them.
func makeDeclaredBinding(shaderBinding gfx.ShaderBinding) (declaredBinding, error) {
	binding := declaredBinding{Group: shaderBinding.Group, Binding: shaderBinding.Binding, Name: shaderBinding.Name, EntryPoints: shaderBinding.EntryPoints}
	var err error
	switch {
	case shaderBinding.Buffer.Type != gfx.BufferBindingTypeUndefined:
		binding.Buffer = &declaredBuffer{Size: shaderBinding.Buffer.MinBindingSize}
		for _, member := range shaderBinding.Members {
			binding.Buffer.Members = append(binding.Buffer.Members, declaredMember(member))
		}
		binding.Buffer.Type, err = findName(bufferBindingTypes, "buffer type", shaderBinding.Buffer.Type)
	case shaderBinding.Sampler.Type != gfx.SamplerBindingTypeUndefined:
		binding.Sampler = &declaredSampler{}
		binding.Sampler.Type, err = findName(samplerBindingTypes, "sampler type", shaderBinding.Sampler.Type)
	case shaderBinding.Texture.SampleType != gfx.TextureSampleTypeUndefined:
		binding.Texture = &declaredTexture{Multisampled: shaderBinding.Texture.Multisampled}
		if binding.Texture.SampleType, err = findName(textureSampleTypes, "sample type", shaderBinding.Texture.SampleType); err == nil {
			binding.Texture.ViewDimension, err = findName(textureViewDimensions, "view dimension", shaderBinding.Texture.ViewDimension)
		}
	case shaderBinding.StorageTexture.Access != gfx.StorageTextureAccessUndefined:
		binding.StorageTexture = &declaredStorageTexture{}
		if binding.StorageTexture.Access, err = findName(storageTextureAccesses, "storage access", shaderBinding.StorageTexture.Access); err != nil {
			break
		}
		if binding.StorageTexture.Format, err = findName(storageTextureFormats, "storage format", shaderBinding.StorageTexture.Format); err != nil {
			break
		}
		binding.StorageTexture.ViewDimension, err = findName(textureViewDimensions, "view dimension", shaderBinding.StorageTexture.ViewDimension)
	default:
		err = errors.New("is of none of buffer, sampler, texture and storageTexture")
	}
	return binding, err
}

// makeShaderBinding answers b as gfx reflects it, or the error naming what
// spells no gfx value: a binding of no kind or of several among them.
func (b declaredBinding) makeShaderBinding() (gfx.ShaderBinding, error) {
	kindCount := 0
	for _, isSet := range []bool{b.Buffer != nil, b.Sampler != nil, b.Texture != nil, b.StorageTexture != nil} {
		if isSet {
			kindCount++
		}
	}
	if kindCount != 1 {
		return gfx.ShaderBinding{}, fmt.Errorf("declares %d of buffer, sampler, texture and storageTexture, want one", kindCount)
	}
	binding := gfx.ShaderBinding{Name: b.Name, Group: b.Group, Binding: b.Binding, EntryPoints: b.EntryPoints}
	var err error
	switch {
	case b.Buffer != nil:
		binding.Buffer.MinBindingSize = b.Buffer.Size
		for _, member := range b.Buffer.Members {
			binding.Members = append(binding.Members, gfx.ShaderStructMember(member))
		}
		binding.Buffer.Type, err = findNamed(bufferBindingTypes, "buffer type", b.Buffer.Type)
	case b.Sampler != nil:
		binding.Sampler.Type, err = findNamed(samplerBindingTypes, "sampler type", b.Sampler.Type)
	case b.Texture != nil:
		binding.Texture.Multisampled = b.Texture.Multisampled
		if binding.Texture.SampleType, err = findNamed(textureSampleTypes, "sample type", b.Texture.SampleType); err == nil {
			binding.Texture.ViewDimension, err = findNamed(textureViewDimensions, "view dimension", b.Texture.ViewDimension)
		}
	default:
		if binding.StorageTexture.Access, err = findNamed(storageTextureAccesses, "storage access", b.StorageTexture.Access); err != nil {
			break
		}
		if binding.StorageTexture.Format, err = findNamed(storageTextureFormats, "storage format", b.StorageTexture.Format); err != nil {
			break
		}
		binding.StorageTexture.ViewDimension, err = findNamed(textureViewDimensions, "view dimension", b.StorageTexture.ViewDimension)
	}
	return binding, err
}

// declaredBuffer is a buffer binding: its type, uniform, storage or
// read-only-storage, its type's size as WGSL lays it out, and its struct's
// members.
type declaredBuffer struct {
	Type    string           `json:"type"`
	Size    uint64           `json:"size"`
	Members []declaredMember `json:"members,omitempty"`
}

// declaredMember is one member of a buffer's struct.
type declaredMember struct {
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
	Size   uint64 `json:"size"`
}

// declaredSampler is a sampler binding: its type, filtering or comparison.
type declaredSampler struct {
	Type string `json:"type"`
}

// declaredTexture is a sampled-texture binding.
type declaredTexture struct {
	SampleType    string `json:"sampleType"`
	ViewDimension string `json:"viewDimension"`
	Multisampled  bool   `json:"multisampled,omitempty"`
}

// declaredStorageTexture is a storage-texture binding.
type declaredStorageTexture struct {
	Access        string `json:"access"`
	Format        string `json:"format"`
	ViewDimension string `json:"viewDimension"`
}
