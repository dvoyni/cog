package rendertest

import (
	"cmp"
	"maps"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/handle"
)

// bindGroupReader reads what each bind group the recording's calls made
// binds, in render's terms: each view of one of render's textures, by the
// texture its label names and its descriptor, each sampler and buffer by
// its descriptor, and the bytes of each material uniform block as gfx's
// submissions found them. What names no handle of render's, but a sampler
// a parameter set, is what a binding left unbound draws with.
type bindGroupReader struct {
	// shaderBindings are the bindings each module's reflection answered.
	shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding
	bindGroups     map[gfx.BindGroup]gfx.BindGroupDescriptor
	textureViews   map[gfx.TextureView]viewTarget
	samplers       map[gfx.Sampler]gfx.SamplerDescriptor
	buffers        map[gfx.Buffer]gfx.BufferDescriptor
	// uniformContents are the bytes of the uniform blocks' buffers as each
	// submission found them, by the submission's index.
	uniformContents []map[gfx.Buffer][]byte
}

// newBindGroupReader answers the reader of the bind groups calls made, the
// views of render's textures being what textureViews says they are, its
// shaders' modules answering their bindings' reflection with
// shaderBindings.
func newBindGroupReader(calls []recording.Call, textureViews map[gfx.TextureView]viewTarget, shaderBindings map[gfx.ShaderModule][]gfx.ShaderBinding) *bindGroupReader {
	r := &bindGroupReader{
		shaderBindings: shaderBindings,
		bindGroups:     map[gfx.BindGroup]gfx.BindGroupDescriptor{},
		textureViews:   textureViews,
		samplers:       map[gfx.Sampler]gfx.SamplerDescriptor{},
		buffers:        map[gfx.Buffer]gfx.BufferDescriptor{},
	}
	contents := map[gfx.Buffer][]byte{}
	for _, call := range calls {
		switch arguments := call.Arguments.(type) {
		case recording.CreateSamplerArguments:
			r.samplers[arguments.Sampler] = arguments.Descriptor
		case recording.CreateBufferArguments:
			r.buffers[arguments.Buffer] = arguments.Descriptor
			if _, ok := handle.ParseLabel[render.Material](arguments.Descriptor.Label); ok {
				contents[arguments.Buffer] = make([]byte, arguments.Descriptor.Size)
			}
		case recording.CreateBindGroupArguments:
			r.bindGroups[arguments.Group] = arguments.Descriptor
		case recording.WriteBufferArguments:
			if bytes, ok := contents[arguments.Buffer]; ok && call.Error == nil {
				copy(bytes[arguments.Offset:], arguments.Data)
			}
		}
		if call.Kind == recording.CallKindSubmit && call.Error == nil {
			snapshot := maps.Clone(contents)
			for buffer, bytes := range snapshot {
				snapshot[buffer] = slices.Clone(bytes)
			}
			r.uniformContents = append(r.uniformContents, snapshot)
		}
	}
	return r
}

// makeParameters answers what group, a bind group of module's @group(1),
// binds to each of module's bindings of that group, by binding number, its
// uniform blocks as the submission of that index found them; none for a
// group no call made.
func (r *bindGroupReader) makeParameters(module gfx.ShaderModule, group gfx.BindGroup, submission int) []render.InspectedParameter {
	descriptor, ok := r.bindGroups[group]
	if !ok {
		return nil
	}
	bindings := slices.DeleteFunc(slices.Clone(r.shaderBindings[module]), func(binding gfx.ShaderBinding) bool { return binding.Group != 1 })
	slices.SortFunc(bindings, func(a, b gfx.ShaderBinding) int { return cmp.Compare(a.Binding, b.Binding) })
	var parameters []render.InspectedParameter
	for _, binding := range bindings {
		index := slices.IndexFunc(descriptor.Entries, func(entry gfx.BindGroupEntry) bool { return entry.Binding == binding.Binding })
		if index < 0 {
			continue
		}
		parameters = append(parameters, r.makeParameter(binding.Name, descriptor.Entries[index], submission))
	}
	return parameters
}

// makeParameter answers what entry binds to the binding named name, its
// uniform block as the submission of that index found it.
func (r *bindGroupReader) makeParameter(name string, entry gfx.BindGroupEntry, submission int) render.InspectedParameter {
	parameter := render.InspectedParameter{Name: name}
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
		label := r.buffers[entry.Buffer].Label
		_, uniform := handle.ParseLabel[render.Material](label)
		buffer, held := handle.ParseLabel[render.Buffer](label)
		switch {
		case uniform:
			if submission < len(r.uniformContents) {
				parameter.Uniform = slices.Clone(r.uniformContents[submission][entry.Buffer][entry.Offset : entry.Offset+entry.Size])
			}
		case held:
			parameter.Buffer = render.BufferRange{Buffer: buffer, Offset: entry.Offset, Size: entry.Size}
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
