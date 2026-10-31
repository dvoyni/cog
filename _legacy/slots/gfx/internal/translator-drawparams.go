package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// drawSet is one draw's set as the translator reads it: the set's record, its
// program's bindings, its own values and the version the draw captured, if
// any. value picks a binding's value from the two.
type drawSet struct {
	record   *setRecord
	bindings []shader.ShaderResource
	values   []bindingValue
	version  []bindingValue
}

// resolveSet finds the set a draw names, or reports that it has none to draw
// with: a set that is not live - released since the frame was recorded, never
// created, or created failed - and a set whose shader is gone are all a frame
// rendered after what it names was let go, and drop the draw silently.
func (t *translator) resolveSet(f *frame, op *DrawOp) (drawSet, bool) {
	if f.resources == nil {
		return drawSet{}, false
	}
	store := &f.resources.drawParams
	id := uint32(op.Set)
	if int(id) >= len(store.records) || store.records[id].state != types.DrawParamsSetLive {
		return drawSet{}, false
	}
	record := &store.records[id]
	if _, uploaded := f.resources.shaderProgram(record.shader); !uploaded {
		return drawSet{}, false
	}
	bindings := shader.ProgramBindings(record.program)
	set := drawSet{
		record: record, bindings: bindings,
		values: store.values[record.values.start : int(record.values.start)+len(bindings)],
	}
	if op.Version > 0 {
		start := int(op.Version - 1)
		set.version = f.queue.versionValues[start : start+len(bindings)]
	}
	return set, true
}

// value is binding i's value for the draw, and whether it is the frame's: the
// version's when it supplied one, the set's otherwise.
func (s *drawSet) value(i int) (*bindingValue, bool) {
	if s.version != nil && s.version[i].supplied {
		return &s.version[i], true
	}
	return &s.values[i], false
}

// translateSetDraw emits one draw through a set of draw params into the open
// pass. Everything the draw names was resolved when the set was created and
// its version recorded, so nothing here looks a name up: it binds each slot's
// record, uploads each distinct uniform once a frame, and keeps only the checks
// that depend on the frame.
func (t *translator) translateSetDraw(f *frame, op *DrawOp, pass types.PassDescr, firstErr *error) {
	m := &op.Mesh
	vertices, indices := m.Vertices, m.Indices
	if vertices.ID == 0 || m.VertexCount <= 0 || m.Stride <= 0 {
		return
	}
	// A draw naming a released id - its vertex buffer, or the index buffer it
	// draws through - goes before anything else, reports included and silently:
	// it is a frame rendered again after what it names was let go, not a
	// mistake. The check is written out here rather than behind a helper so it
	// inlines; a helper naming both buffers is over the inliner's budget, and
	// the call alone measured about two percent of TranslateSteadyState.
	if t.released.buffer(vertices.ID) || (m.IndexCount > 0 && t.released.buffer(indices.ID)) {
		return
	}
	set, ok := t.resolveSet(f, op)
	if !ok {
		return
	}
	label := set.record.program.Label()
	if indices.ID != 0 && indices.Size%m.IndexWidth.Bytes() != 0 {
		if err := t.reportIndexLengthOf(m, label); err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	pipeline, err := t.ensurePipeline(f.backend, set.record.shader, set.record.program, m, set.record.drawState, pass)
	if err != nil && *firstErr == nil {
		*firstErr = err
	}
	if pipeline == 0 {
		return
	}
	if drop, err := t.checkSetBindings(&set, pass); drop {
		if err != nil && *firstErr == nil {
			*firstErr = err
		}
		return
	}
	if !t.suppliesStorage(&set, firstErr) {
		return
	}
	t.ops.SetPipeline(pipeline)
	t.emitSetBindings(f, &set)
	t.ops.SetVertexBuffer(vertices.ID, 0)

	instances := op.Instances
	if instances < 1 {
		instances = 1
	}
	if m.IndexCount > 0 && indices.ID != 0 {
		t.ops.SetIndexBuffer(indices.ID, 0, m.IndexWidth)
		t.ops.Draw(0, m.IndexCount, instances, op.FirstInstance, true)
	} else {
		t.ops.Draw(0, m.VertexCount, instances, op.FirstInstance, false)
	}
}

// checkSetBindings reports whether a draw is dropped over a texture it names,
// and the error to report for it: a texture its own pass renders into, and one
// of the wrong view dimension. Both depend on the frame - the pass, and the
// texture a version supplied - so they are the checks the set could not make
// when it was created. The view mismatch is reported once a binding, on
// reportTextureView's terms, and dropped every time.
func (t *translator) checkSetBindings(set *drawSet, pass types.PassDescr) (bool, error) {
	for i := range set.bindings {
		binding := &set.bindings[i]
		if binding.Kind.Base() != shader.ResourceTexture {
			continue
		}
		value, _ := set.value(i)
		if value.texture == 0 {
			continue
		}
		if (pass.Target.Kind == types.TargetTexture && pass.Target.Texture == value.texture) ||
			(pass.Depth.Kind == types.DepthKindTexture && pass.Depth.Texture == value.texture) {
			return true, types.ErrDrawSamplesAttachment{Pass: pass.Label, Parameter: binding.Name}
		}
		if value.layers == 0 || (value.layers > 1) == (binding.TextureView == shader.TextureView2DArray) {
			continue
		}
		key := textureViewKey{shader: set.record.shader, parameter: binding.Name}
		if _, seen := t.textureViewMismatches[key]; seen {
			return true, nil
		}
		t.textureViewMismatches[key] = struct{}{}
		return true, types.ErrTextureViewDimensionMismatch{
			Shader: set.record.program.Label(), Parameter: binding.Name,
			Group: binding.Group, Binding: binding.Binding,
			Declared: textureViewName(binding.TextureView), Supplied: textureViewNameOfLayers(int(value.layers)),
		}
	}
	return false, nil
}

// suppliesStorage reports whether every storage binding of the draw's set has
// a buffer, and reports the first that has none, once. A storage buffer is the
// one binding with no default: a draw missing one has no data. A buffer
// released since the frame was recorded is missing too, silently - it is the
// re-rendered frame, as a released mesh is.
func (t *translator) suppliesStorage(set *drawSet, firstErr *error) bool {
	for i := range set.bindings {
		binding := &set.bindings[i]
		if binding.Kind.Base() != shader.ResourceStorageBuffer {
			continue
		}
		value, _ := set.value(i)
		if value.buffer != 0 {
			if t.released.buffer(value.buffer) {
				return false
			}
			continue
		}
		key := unsuppliedBufferKey{shader: set.record.shader, parameter: binding.Name}
		if _, seen := t.unsuppliedBuffers[key]; !seen {
			t.unsuppliedBuffers[key] = struct{}{}
			if *firstErr == nil {
				*firstErr = types.ErrStorageBufferUnsupplied{
					Shader: set.record.program.Label(), Parameter: binding.Name,
					Group: binding.Group, Binding: binding.Binding, Unbaked: value.supplied,
				}
			}
		}
		return false
	}
	return true
}

// emitSetBindings binds every binding of the draw's set: each uniform by its
// offset in the frame's uniform arena, uploaded the first time a draw names it
// this frame; each sampler, texture and buffer from its record. An unsupplied
// uniform is the set's own zero bytes, an unsupplied sampler the default and an
// unsupplied texture white.
func (t *translator) emitSetBindings(f *frame, set *drawSet) {
	store := &f.resources.drawParams
	for i := range set.bindings {
		binding := &set.bindings[i]
		value, fromFrame := set.value(i)
		switch binding.Kind.Base() {
		case shader.ResourceUniformBuffer:
			var offset int
			if fromFrame {
				offset = t.uploadUniform(&t.versionUniforms, int(value.offset)/uniformArenaAlign, f.queue.versionBytes, value)
			} else {
				offset = t.uploadUniform(&t.setUniforms, int(set.record.values.start)+i, store.bytes, value)
			}
			t.ops.BindUniformBlock(binding.Group, binding.Binding, offset, int(value.size))
		case shader.ResourceSampler:
			t.ops.SetSampler(t.ensureSampler(f.backend, value.sampler), binding.Group, binding.Binding)
		case shader.ResourceStorageBuffer:
			t.ops.SetBuffer(binding.Group, binding.Binding, value.buffer, int(value.bufferOffset), int(value.bufferSize))
		default:
			texture := value.texture
			if texture == 0 && value.path != 0 {
				paths := store.paths
				if fromFrame {
					paths = f.queue.versionPaths
				}
				texture = t.ensureTexture(f, types.TextureWithResource(paths[value.path-1]))
			}
			t.ops.SetTexture(texture, binding.Group, binding.Binding)
		}
	}
}

// uploadUniform returns where one value sits in the frame's uniform arena,
// copying it there the first time this translation asks. uploads is the table
// of what this translation copied, indexed by key.
func (t *translator) uploadUniform(uploads *[]uniformUpload, key int, bytes []byte, value *bindingValue) int {
	if key >= len(*uploads) {
		*uploads = append(*uploads, make([]uniformUpload, key+1-len(*uploads))...)
	}
	upload := &(*uploads)[key]
	if upload.stamp != t.stamp {
		offset, block := t.ops.ClaimUniform(int(value.size))
		copy(block, bytes[value.offset:value.offset+value.size])
		*upload = uniformUpload{stamp: t.stamp, offset: int32(offset)}
	}
	return int(upload.offset)
}

// collectSetSampled adds every texture a set draw samples to the run's sampled
// set. It runs before the pass opens, so it resolves nothing past the set: a
// set that will not draw costs a barrier nothing reads, which is the safe
// direction to be wrong in.
func (t *translator) collectSetSampled(f *frame, op *DrawOp) {
	set, ok := t.resolveSet(f, op)
	if !ok {
		return
	}
	for i := range set.bindings {
		if set.bindings[i].Kind.Base() == shader.ResourceTexture {
			value, _ := set.value(i)
			t.noteSampled(value.texture)
		}
	}
}
