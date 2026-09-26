package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// drawParamsKey is one set under one of the frame's versions of it: what a
// draw draws with.
type drawParamsKey struct {
	set     uint32
	version int32
}

// appendDrawParamsViews renders the sets one kept pass draws through, adding
// each set and version the first time a draw names it and counting every draw
// against the entry. seen maps an entry to its position in dst.
func appendDrawParamsViews(dst []DrawParamsView, seen map[drawParamsKey]int, queue *OpQueue, resources *ResourceQueue, index int) []DrawParamsView {
	draws := OpQueuePasses(queue)[index].Draws
	for i := range draws {
		op := &draws[i]
		id := descriptors.DrawParamsIndex(op.Set)
		if id == 0 {
			continue
		}
		key := drawParamsKey{set: id, version: op.Version}
		at, ok := seen[key]
		if !ok {
			at = len(dst)
			seen[key] = at
			dst = append(dst, drawParamsViewOf(queue, resources, key))
		}
		view := &dst[at]
		if len(view.Passes) == 0 || view.Passes[len(view.Passes)-1] != index {
			view.Passes = append(view.Passes, index)
		}
		view.Draws++
		view.Instances += op.Instances
	}
	return dst
}

// drawParamsViewOf renders one set under one version, reading the set's values
// where the translator would and the version's from the frame's arenas.
func drawParamsViewOf(queue *OpQueue, resources *ResourceQueue, key drawParamsKey) DrawParamsView {
	view := DrawParamsView{Set: key.set, Version: key.version, State: "unknown"}
	if resources == nil || int(key.set) >= len(resources.drawParams.records) {
		return view
	}
	store := &resources.drawParams
	record := &store.records[key.set]
	view.State, view.Shader = setStateName(record.state), record.shader
	if record.state != setLive && record.state != setFailed {
		return view
	}
	state := DrawStateViewOf(record.drawState)
	view.DrawState, view.Label = &state, record.program.Label()
	if record.state != setLive {
		return view
	}
	if _, uploaded := resources.shaderProgram(record.shader); !uploaded {
		view.State = "shaderReleased"
	}
	bindings := shader.ProgramBindings(record.program)
	set := drawSet{
		record: record, bindings: bindings,
		values: store.values[record.values.start : int(record.values.start)+len(bindings)],
	}
	if key.version > 0 {
		start := int(key.version - 1)
		set.version = queue.versionValues[start : start+len(bindings)]
	}
	view.Bindings = make([]DrawParamsBindingView, len(bindings))
	for i := range bindings {
		value, fromFrame := set.value(i)
		paths := store.paths
		if fromFrame {
			paths = queue.versionPaths
		}
		view.Bindings[i] = drawParamsBindingViewOf(&bindings[i], value, fromFrame, paths)
	}
	return view
}

// drawParamsBindingViewOf renders one binding's value. A value no param
// supplied is the binding's default, which the view says rather than showing
// the zero record behind it.
func drawParamsBindingViewOf(binding *shader.ShaderResource, value *bindingValue, fromFrame bool, paths []string) DrawParamsBindingView {
	view := DrawParamsBindingView{
		Name: binding.Name, Kind: bindingKindViewName(binding.Kind),
		Group: binding.Group, Binding: binding.Binding, Source: "default",
	}
	switch {
	case fromFrame:
		view.Source = "frame"
	case value.supplied:
		view.Source = "set"
	}
	switch binding.Kind.Base() {
	case shader.ResourceUniformBuffer:
		view.Size = int(value.size)
	case shader.ResourceSampler:
		if value.supplied {
			sampler := SamplerViewOf(value.sampler)
			view.Sampler = &sampler
		}
	case shader.ResourceStorageBuffer:
		view.Buffer, view.Offset, view.Range = value.buffer, int(value.bufferOffset), int(value.bufferSize)
	default:
		view.Texture, view.Dimension = value.texture, textureViewName(binding.TextureView)
		if value.texture == 0 && value.path != 0 {
			view.Path = paths[value.path-1]
		}
	}
	return view
}

// bindingKindViewName names a binding's kind with the flag that refines it,
// since binding a colour texture where a depth one is declared is exactly the
// mistake an agent reading this is after.
func bindingKindViewName(kind shader.ResourceKind) string {
	switch kind.Base() {
	case shader.ResourceUniformBuffer:
		return "uniform"
	case shader.ResourceStorageBuffer:
		if kind.Has(shader.ResourceWritable) {
			return "readWriteStorage"
		}
		return "storage"
	case shader.ResourceSampler:
		if kind.Has(shader.ResourceComparison) {
			return "comparisonSampler"
		}
		return "sampler"
	}
	if kind.Has(shader.ResourceDepth) {
		return "depthTexture"
	}
	return "texture"
}

func setStateName(state setState) string {
	switch state {
	case setUnknown:
		return "unknown"
	case setLive:
		return "live"
	case setFailed:
		return "failed"
	case setReleased:
		return "released"
	}
	return descriptors.UnknownName(int(state))
}
