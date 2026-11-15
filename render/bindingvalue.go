package render

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// bindingValue is the value bound to one binding, a material's, a pass's or
// a draw's, of its kind: a texture seen through view; a sampler; a range of a
// buffer, its size filled in; or a uniform block, held in a buffer its
// material keeps, or for a pass or draw parameter in bufferRange, a block of
// its list's arena. A value of no kind is unbound, and its binding draws its
// default.
type bindingValue struct {
	kind        parameterKind
	texture     Texture
	view        gfx.TextureViewDescriptor
	sampler     gfx.SamplerDescriptor
	bufferRange BufferRange
}

// fillTexture binds view of texture, which gfx made from descriptor, as p,
// or answers why it does not fit binding.
func (p *bindingValue) fillTexture(view TextureView, descriptor gfx.TextureDescriptor, binding gfx.ShaderBinding) error {
	if binding.Texture.SampleType == gfx.TextureSampleTypeUndefined && binding.StorageTexture.Access == gfx.StorageTextureAccessUndefined {
		return fmt.Errorf("is a texture, where its binding is a %s", describeBindingKind(binding))
	}
	viewDescriptor, err := view.makeViewDescriptor(descriptor, binding)
	if err != nil {
		return err
	}
	p.kind = parameterKindTexture
	p.texture = view.Texture
	p.view = viewDescriptor
	return nil
}

// fillSampler binds sampler as p, or answers why it does not fit binding.
func (p *bindingValue) fillSampler(sampler SamplerDescriptor, binding gfx.ShaderBinding) error {
	if binding.Sampler.Type == gfx.SamplerBindingTypeUndefined {
		return fmt.Errorf("is a sampler, where its binding is a %s", describeBindingKind(binding))
	}
	descriptor, err := sampler.makeGfxSamplerDescriptor(binding)
	if err != nil {
		return err
	}
	p.kind = parameterKindSampler
	p.sampler = descriptor
	return nil
}

// fillBuffer binds bufferRange of a buffer gfx made from descriptor as p, or
// answers why it does not fit binding under limits: a binding of no buffer,
// a buffer without the binding's usage, a range at an offset no multiple of
// the binding's alignment, past the buffer's end, smaller than the binding's
// type or, for a uniform block, larger than a uniform binding may be.
func (p *bindingValue) fillBuffer(bufferRange BufferRange, descriptor gfx.BufferDescriptor, binding gfx.ShaderBinding, limits gfx.Limits) error {
	usage := gfx.BufferUsageFlagStorage
	alignment := uint64(limits.MinStorageBufferOffsetAlignment)
	maxSize := limits.MaxStorageBufferBindingSize
	switch binding.Buffer.Type {
	case gfx.BufferBindingTypeUndefined:
		return fmt.Errorf("is a buffer, where its binding is a %s", describeBindingKind(binding))
	case gfx.BufferBindingTypeUniform:
		usage = gfx.BufferUsageFlagUniform
		alignment = uint64(limits.MinUniformBufferOffsetAlignment)
		maxSize = limits.MaxUniformBufferBindingSize
	}
	size, err := bufferRange.findSize(descriptor.Size)
	switch {
	case err != nil:
		return err
	case descriptor.Usage&usage == 0:
		return fmt.Errorf("is buffer %v, made without the usage of its binding, a %s", bufferRange.Buffer, describeBindingKind(binding))
	case bufferRange.Offset%max(alignment, 1) != 0:
		return fmt.Errorf("is at offset %d, no multiple of %d, its binding's alignment", bufferRange.Offset, alignment)
	case size < binding.Buffer.MinBindingSize:
		return fmt.Errorf("is %d bytes, where its binding's type takes %d", size, binding.Buffer.MinBindingSize)
	case size > maxSize:
		return fmt.Errorf("is %d bytes, more than %d, the most its binding may take", size, maxSize)
	}
	p.kind = parameterKindBuffer
	p.bufferRange = BufferRange{Buffer: bufferRange.Buffer, Offset: bufferRange.Offset, Size: size}
	return nil
}

// fillUniform binds a uniform block as p, binding, of material's named
// name, held in uniformBuffer, which it makes, labelled with material's
// handle, when it holds none; an error when gfx cannot make it.
func (p *bindingValue) fillUniform(device gfx.Device, binding gfx.ShaderBinding, uniformBuffer *gfx.Buffer, material Material, name string) error {
	if *uniformBuffer == (gfx.Buffer{}) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Label: handle.MakeLabel(material, "uniform block "+name), Size: binding.Buffer.MinBindingSize, Usage: gfx.BufferUsageFlagUniform | gfx.BufferUsageFlagCopyDst})
		if err != nil {
			return fmt.Errorf("could not be held: %w", err)
		}
		*uniformBuffer = buffer
	}
	*p = bindingValue{kind: parameterKindUniform}
	return nil
}

// makeInspectedParameter answers v, bound to binding, as an inspection
// writes it down: Default when defaulted, the binding drawing its default;
// a uniform block as its bytes, materialBytes for one a material holds in a
// buffer of its own, and arena's for one in a temporary buffer, as a
// buffer bound to a uniform block in a temporary buffer is.
func (v bindingValue) makeInspectedParameter(binding gfx.ShaderBinding, defaulted bool, materialBytes []byte, arena *temporaryArena) InspectedParameter {
	parameter := InspectedParameter{Name: binding.Name, Group: binding.Group, Binding: binding.Binding, Default: defaulted}
	if defaulted {
		return parameter
	}
	switch v.kind {
	case parameterKindTexture:
		parameter.TextureView = makeInspectedTextureView(v.texture, v.view)
	case parameterKindSampler:
		parameter.Sampler = makeInspectedSamplerDescriptor(v.sampler)
	case parameterKindUniform, parameterKindBuffer:
		if v.kind == parameterKindUniform && v.bufferRange.Buffer == (Buffer{}) {
			parameter.Uniform = slices.Clone(materialBytes)
			break
		}
		if binding.Buffer.Type == gfx.BufferBindingTypeUniform {
			if uniform, ok := arena.findBytes(v.bufferRange); ok {
				parameter.Uniform = uniform
				break
			}
		}
		parameter.Buffer = v.bufferRange
	}
	return parameter
}

// overlapsTargets answers whether v is a texture overlapping one of targets,
// a pass's, where a binding may not read it: sampled, a binding sampling it,
// overlapping a target the pass writes, or, not sampled, a storage binding,
// overlapping any.
func (v bindingValue) overlapsTargets(sampled bool, targets []drawnTarget) bool {
	if v.kind != parameterKindTexture {
		return false
	}
	for _, target := range targets {
		if target.texture == v.texture && (!target.readOnly || !sampled) &&
			target.mipLevel-v.view.BaseMipLevel < v.view.MipLevelCount &&
			target.layer-v.view.BaseArrayLayer < v.view.ArrayLayerCount {
			return true
		}
	}
	return false
}

// parameterKind is the kind of value a bindingValue binds, or a
// parameterSetting sets.
type parameterKind uint8

const (
	// parameterKindUnbound is no value: the binding draws its default.
	parameterKindUnbound parameterKind = iota
	parameterKindTexture
	parameterKindSampler
	parameterKindBuffer
	parameterKindUniform
)

// describeBindingKind answers what binding holds, as a report names it.
func describeBindingKind(binding gfx.ShaderBinding) string {
	switch {
	case binding.Buffer.Type == gfx.BufferBindingTypeUniform:
		return "uniform block"
	case binding.Buffer.Type != gfx.BufferBindingTypeUndefined:
		return "storage buffer"
	case binding.Sampler.Type != gfx.SamplerBindingTypeUndefined:
		return "sampler"
	case binding.Texture.SampleType != gfx.TextureSampleTypeUndefined:
		return "texture"
	}
	return "storage texture"
}

// isStorage answers whether binding is a storage buffer or a storage
// texture, which no default fills for a function that reads it.
func isStorage(binding gfx.ShaderBinding) bool {
	return (binding.Buffer.Type != gfx.BufferBindingTypeUndefined && binding.Buffer.Type != gfx.BufferBindingTypeUniform) ||
		binding.StorageTexture.Access != gfx.StorageTextureAccessUndefined
}
