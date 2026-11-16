package rendertest

import (
	"cmp"
	"slices"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/internal/handle"
)

// bindGroupReader reads what each bind group the recording's calls made
// binds, in render's terms: each view of one of render's textures, by the
// texture its label names and its descriptor, each sampler and buffer by
// its descriptor, and the bytes of each uniform block a material holds or
// the temporary arena does as gfx's submissions found them. What names no
// handle of render's, but a sampler a parameter set, is what a binding left
// unbound draws with.
type bindGroupReader struct {
	// shaderBindings are the bindings each module's reflection answered.
	shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding
	layouts        map[gfx.BindGroupLayout]gfx.BindGroupLayoutDescriptor
	bindGroups     map[gfx.BindGroup]gfx.BindGroupDescriptor
	textureViews   map[gfx.TextureView]viewTarget
	samplers       map[gfx.Sampler]gfx.SamplerDescriptor
	buffers        map[gfx.Buffer]gfx.BufferDescriptor
	// contents are the bytes of the buffers of materials' uniform blocks and
	// of the arena's chunks, each version from the submission that first
	// found it on.
	contents map[gfx.Buffer][]bufferVersion
}

// bufferVersion is a buffer's bytes from the submission of index submission
// on, until its next version.
type bufferVersion struct {
	submission int
	bytes      []byte
}

// newBindGroupReader answers the reader of the bind groups calls made, the
// views of render's textures being what textureViews says they are, its
// shaders' modules answering their bindings' reflection with
// shaderBindings.
func newBindGroupReader(calls []recording.Call, textureViews map[gfx.TextureView]viewTarget, shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding) *bindGroupReader {
	r := &bindGroupReader{
		shaderBindings: shaderBindings,
		layouts:        map[gfx.BindGroupLayout]gfx.BindGroupLayoutDescriptor{},
		bindGroups:     map[gfx.BindGroup]gfx.BindGroupDescriptor{},
		textureViews:   textureViews,
		samplers:       map[gfx.Sampler]gfx.SamplerDescriptor{},
		buffers:        map[gfx.Buffer]gfx.BufferDescriptor{},
		contents:       map[gfx.Buffer][]bufferVersion{},
	}
	// submission is the index of the next submission.
	submission := 0
	for _, call := range calls {
		switch arguments := call.Arguments.(type) {
		case recording.CreateSamplerArguments:
			r.samplers[arguments.Sampler] = arguments.Descriptor
		case recording.CreateBufferArguments:
			r.buffers[arguments.Buffer] = arguments.Descriptor
			_, uniform := handle.ParseLabel[render.Material](arguments.Descriptor.Label)
			_, label, held := handle.SplitLabel[render.Buffer](arguments.Descriptor.Label)
			if uniform || (held && label == handle.TemporaryChunkLabel) {
				r.contents[arguments.Buffer] = []bufferVersion{{submission: submission, bytes: make([]byte, arguments.Descriptor.Size)}}
			}
		case recording.CreateBindGroupLayoutArguments:
			r.layouts[arguments.Layout] = arguments.Descriptor
		case recording.CreateBindGroupArguments:
			r.bindGroups[arguments.Group] = arguments.Descriptor
		case recording.WriteBufferArguments:
			versions, ok := r.contents[arguments.Buffer]
			if !ok || call.Error != nil {
				break
			}
			if last := versions[len(versions)-1]; last.submission != submission {
				versions = append(versions, bufferVersion{submission: submission, bytes: slices.Clone(last.bytes)})
				r.contents[arguments.Buffer] = versions
			}
			copy(versions[len(versions)-1].bytes[arguments.Offset:], arguments.Data)
		}
		if call.Kind == recording.CallKindSubmit && call.Error == nil {
			submission++
		}
	}
	return r
}

// makeParameters answers what group, a bind group of module's @group of
// index groupIndex, bound at dynamicOffsets, binds to each of module's
// bindings of that group, by binding number, its uniform blocks as the
// submission of that index found them; none for a group no call made.
func (r *bindGroupReader) makeParameters(module gfx.ShaderModule, groupIndex uint32, group gfx.BindGroup, dynamicOffsets []uint32, submission int) []render.InspectedParameter {
	descriptor, ok := r.bindGroups[group]
	if !ok {
		return nil
	}
	// bindingOffsets are the dynamic offsets by binding number, given in the
	// order of the layout's entries that bind at one.
	bindingOffsets := map[uint32]uint64{}
	for _, entry := range r.layouts[descriptor.Layout].Entries {
		if entry.Buffer.HasDynamicOffset && len(bindingOffsets) < len(dynamicOffsets) {
			bindingOffsets[entry.Binding] = uint64(dynamicOffsets[len(bindingOffsets)])
		}
	}
	bindings := slices.DeleteFunc(slices.Clone(r.shaderBindings[module]), func(binding gfx.ShaderBinding) bool { return binding.Group != groupIndex })
	slices.SortFunc(bindings, func(a, b gfx.ShaderBinding) int { return cmp.Compare(a.Binding, b.Binding) })
	var parameters []render.InspectedParameter
	for _, binding := range bindings {
		index := slices.IndexFunc(descriptor.Entries, func(entry gfx.BindGroupEntry) bool { return entry.Binding == binding.Binding })
		if index < 0 {
			continue
		}
		parameters = append(parameters, r.makeParameter(binding, descriptor.Entries[index], bindingOffsets[binding.Binding], submission))
	}
	return parameters
}

// makeParameter answers what entry, bound at dynamicOffset, binds to
// binding, a uniform block of a material's or of the arena as the
// submission of that index found it.
func (r *bindGroupReader) makeParameter(binding gfx.ShaderBinding, entry gfx.BindGroupEntry, dynamicOffset uint64, submission int) render.InspectedParameter {
	parameter := render.InspectedParameter{Name: binding.Name, Group: binding.Group, Binding: binding.Binding}
	switch {
	case entry.TextureView != (gfx.TextureView{}):
		view, ok := r.textureViews[entry.TextureView]
		parameter.Default = !ok
		if ok {
			parameter.TextureView = render.TextureView{
				Texture:       view.texture,
				BaseMipLevel:  view.descriptor.BaseMipLevel,
				MipLevelCount: view.descriptor.MipLevelCount,
				BaseLayer:     view.descriptor.BaseArrayLayer,
				LayerCount:    view.descriptor.ArrayLayerCount,
				Dimension:     render.TextureViewDimension(view.descriptor.Dimension),
				Aspect:        render.TextureAspect(view.descriptor.Aspect),
			}
		}
	case entry.Sampler != (gfx.Sampler{}):
		sampler := r.samplers[entry.Sampler]
		parameter.Default = sampler.Label != ""
		if !parameter.Default {
			parameter.Sampler = makeSamplerDescriptor(sampler)
		}
	default:
		offset := entry.Offset + dynamicOffset
		versions, tracked := r.contents[entry.Buffer]
		buffer, held := handle.ParseLabel[render.Buffer](r.buffers[entry.Buffer].Label)
		switch {
		case tracked && binding.Buffer.Type == gfx.BufferBindingTypeUniform:
			index := slices.IndexFunc(versions, func(version bufferVersion) bool { return version.submission > submission })
			if index < 0 {
				index = len(versions)
			}
			if index > 0 {
				parameter.Uniform = slices.Clone(versions[index-1].bytes[offset : offset+entry.Size])
			}
		case held:
			parameter.Buffer = render.BufferRange{Buffer: buffer, Offset: offset, Size: entry.Size}
		default:
			parameter.Default = true
		}
	}
	return parameter
}

// makeSamplerDescriptor answers descriptor in render's terms, each field at
// its default spelt zero.
func makeSamplerDescriptor(descriptor gfx.SamplerDescriptor) render.SamplerDescriptor {
	return render.SamplerDescriptor{
		AddressModeU:  render.AddressMode(spellDefaultZero(descriptor.AddressModeU, gfx.AddressModeClampToEdge)),
		AddressModeV:  render.AddressMode(spellDefaultZero(descriptor.AddressModeV, gfx.AddressModeClampToEdge)),
		AddressModeW:  render.AddressMode(spellDefaultZero(descriptor.AddressModeW, gfx.AddressModeClampToEdge)),
		MagFilter:     render.FilterMode(spellDefaultZero(descriptor.MagFilter, gfx.FilterModeNearest)),
		MinFilter:     render.FilterMode(spellDefaultZero(descriptor.MinFilter, gfx.FilterModeNearest)),
		MipmapFilter:  render.FilterMode(spellDefaultZero(descriptor.MipmapFilter, gfx.FilterModeNearest)),
		LodMinClamp:   descriptor.LodMinClamp,
		LodMaxClamp:   spellDefaultZero(descriptor.LodMaxClamp, 32),
		Compare:       render.CompareFunction(descriptor.Compare),
		MaxAnisotropy: spellDefaultZero(descriptor.Anisotropy, 1),
	}
}
