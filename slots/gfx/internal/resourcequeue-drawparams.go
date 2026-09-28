package internal

import (
	"github.com/dvoyni/cog/libs/m"
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// setRecord is the durable side of one set: what it draws with and where its
// values are. The values themselves sit in the queue's arenas, which is what
// keeps the table they are read from pointer-free; the program is the record's
// one reference, to data nothing ever writes.
type setRecord struct {
	state     setState
	shader    types.ShaderID
	drawState types.DrawState
	program   shader.ShaderProgram
	// values is the set's binding table in setValues, one slot per binding of
	// its program, and bytes its uniforms' bytes in setBytes.
	values, bytes span
}

// span is a run of an arena: where it starts and how long it is.
type span struct{ start, count int32 }

// drawParamsStore is the ResourceQueue's half of every set: the records, and
// the arenas their values live in. It is durable - reset leaves it - and it is
// read by the translator on the render thread, under the ResourceQueue lock the
// render already holds, so the values need no second copy on that side.
type drawParamsStore struct {
	registry *drawParamsRegistry
	records  []setRecord
	values   []bindingValue
	bytes    []byte
	// paths is every texture path a set has named, interned so that a set named
	// by path every time it is updated does not grow the table.
	paths     []string
	pathIndex map[string]int32
	// freeValues and freeBytes are the runs released sets gave back, reused by
	// the next set of the same size. Sets built on one shader are one size, so
	// exact size is the fit worth looking for.
	freeValues, freeBytes []span
	// durableBuffers and durableTextures mark every id this queue minted, one
	// bit an id: a set may name those and no other.
	durableBuffers, durableTextures []uint64
}

// NewDrawParams creates a durable set of draw params: shader, state and a value
// for some or all of the shader's bindings, each param naming one binding by its
// WGSL global name and supplying the whole of it. The set is resolved against
// the shader's program here, once: the translator never resolves a name.
//
// A texture or buffer param carrying inline bytes is baked into a resource the
// set owns and ReleaseDrawParams frees. A set names durable resources only - a
// temporary from an OpQueue is refused - and a texture path is resolved by the
// render thread's cache, as everywhere.
//
// The mistakes a call can make - a shader with no program, a param naming no
// binding, of the wrong kind or the wrong size, or a temporary - are reported
// through k, once each, and the param is ignored. A set whose shader had no
// program still exists and draws nothing.
func (q *ResourceQueue) NewDrawParams(k kernel.Kernel, shaderID types.ShaderID, state types.DrawState, params ...types.ShaderParameterDescr) types.DrawStateID {
	store := &q.drawParams
	if len(store.records) == 0 {
		// Id zero is the zero handle, which names no set.
		store.records = append(store.records, setRecord{})
	}
	id := uint32(len(store.records))
	set := types.DrawStateID(id)
	program, ok := q.shaderProgram(shaderID)
	if !ok {
		q.reportNoProgram(k, id, shaderID)
		store.records = append(store.records, setRecord{state: setFailed, shader: shaderID, drawState: state})
		store.registry.publish(id, shader.ShaderProgram{}, setFailed)
		return set
	}
	bindings := shader.ProgramBindings(program)
	record := setRecord{state: setLive, shader: shaderID, drawState: state, program: program}
	record.values = store.claimValues(len(bindings))
	size := 0
	for i := range bindings {
		if bindings[i].Kind.Base() == shader.ResourceUniformBuffer {
			size = alignUniform(size) + bindings[i].Size
		}
	}
	record.bytes = store.claimBytes(size)
	// Every uniform has its bytes from the start, zeroed: an unsupplied uniform
	// is zero, and binds as the set's own zero bytes with nothing to special.
	offset := int(record.bytes.start)
	values := store.values[record.values.start : record.values.start+record.values.count]
	for i := range bindings {
		if bindings[i].Kind.Base() == shader.ResourceUniformBuffer {
			offset = alignUniform(offset)
			values[i].offset, values[i].size = int32(offset), int32(bindings[i].Size)
			offset += bindings[i].Size
		}
	}
	store.records = append(store.records, record)
	store.registry.publish(id, program, setLive)
	q.applyDrawParams(k, id, "NewDrawParams", params)
	return set
}

// UpdateDrawParams changes a set's own values, for good. The ResourceQueue is
// consumed by whichever render comes next, so an update is for material-rate
// changes - a swapped texture, a tint - and may be seen one frame early by a
// re-rendered frame; a value that must match its frame belongs in the frame's
// version, through OpQueue.SetDrawParams. A set that is not live is reported
// through k once and the call ignored.
func (q *ResourceQueue) UpdateDrawParams(k kernel.Kernel, set types.DrawStateID, params ...types.ShaderParameterDescr) {
	id := uint32(set)
	if q.namedSet(k, id, "UpdateDrawParams") != setLive {
		return
	}
	q.applyDrawParams(k, id, "UpdateDrawParams", params)
}

// ReleaseDrawParams releases a set and every resource it baked from inline
// bytes. A frame still naming it - one rendered again after the release - drops
// its draws. A set that is not live is reported through k once and ignored.
func (q *ResourceQueue) ReleaseDrawParams(k kernel.Kernel, set types.DrawStateID) {
	id := uint32(set)
	// A failed set is released like a live one; its runs are empty.
	if state := q.namedSet(k, id, "ReleaseDrawParams"); state != setLive && state != setFailed {
		return
	}
	store := &q.drawParams
	record := &store.records[id]
	values := store.values[record.values.start : record.values.start+record.values.count]
	for i := range values {
		q.releaseOwned(&values[i])
	}
	if record.values.count > 0 {
		store.freeValues = append(store.freeValues, record.values)
	}
	if record.bytes.count > 0 {
		store.freeBytes = append(store.freeBytes, record.bytes)
	}
	*record = setRecord{state: setReleased, shader: record.shader}
	store.registry.release(id)
}

// namedSet returns the state of the set id names, reporting once under call
// when it names none that exists. A set whose creation failed exists, and is no
// mistake to name.
func (q *ResourceQueue) namedSet(k kernel.Kernel, id uint32, call string) setState {
	state := setUnknown
	if int(id) < len(q.drawParams.records) {
		state = q.drawParams.records[id].state
	}
	if state != setLive && state != setFailed {
		reportSetNotLive(k, id, state, call)
	}
	return state
}

// reportNoProgram says why a set was created failed: its shader is not live, or
// is live with no program yet.
func (q *ResourceQueue) reportNoProgram(k kernel.Kernel, set uint32, id types.ShaderID) {
	var state shaderState
	if int(id) < len(q.shaders) {
		state = q.shaders[id].state
	}
	var err error = types.ErrShaderNotLive{Shader: id, Call: "NewDrawParams", Released: state == shaderReleased}
	if state == shaderReserved {
		err = types.ErrShaderHasNoProgram{Shader: id, Call: "NewDrawParams"}
	}
	reportDrawParams(k, set, "", drawParamsFaultNoShader, "NewDrawParams", err)
}

// applyDrawParams writes params into a live set's own values.
func (q *ResourceQueue) applyDrawParams(k kernel.Kernel, id uint32, call string, params []types.ShaderParameterDescr) {
	store := &q.drawParams
	record := &store.records[id]
	values := store.values[record.values.start : record.values.start+record.values.count]
	for i := range params {
		param := &params[i]
		slot, _, fault, err := paramSlot(record.program, param, call)
		if err != nil {
			reportDrawParams(k, id, param.Name, fault, call, err)
			continue
		}
		value := &values[slot]
		switch param.Kind {
		case types.ShaderParameterKindRaw, types.ShaderParameterKindFloat, types.ShaderParameterKindVec4,
			types.ShaderParameterKindMat4, types.ShaderParameterKindColor:
			copy(store.bytes[value.offset:value.offset+value.size], param.Bytes())
			value.supplied = true
		case types.ShaderParameterKindSampler:
			value.sampler, value.supplied = param.Sampler, true
		case types.ShaderParameterKindTexture:
			resolved, ok := q.durableTexture(param.Texture)
			if !ok {
				reportDrawParams(k, id, param.Name, drawParamsFaultTemporary, call,
					types.ErrDrawParamTemporary{Shader: record.program.Label(), Parameter: param.Name, Call: call})
				continue
			}
			q.releaseOwned(value)
			*value = resolved
		case types.ShaderParameterKindBuffer:
			resolved, ok := q.durableBuffer(param)
			if !ok {
				reportDrawParams(k, id, param.Name, drawParamsFaultTemporary, call,
					types.ErrDrawParamTemporary{Shader: record.program.Label(), Parameter: param.Name, Call: call})
				continue
			}
			q.releaseOwned(value)
			*value = resolved
		}
	}
}

// durableTexture resolves a texture param's descriptor to the value a set
// keeps: a durable id as it is, a path interned, inline pixels baked into a
// texture the set owns. It refuses an id this queue did not mint.
func (q *ResourceQueue) durableTexture(texture types.TextureDescr) (bindingValue, bool) {
	value := bindingValue{supplied: true}
	switch {
	case texture.Params.ID != 0:
		if !hasBit(q.drawParams.durableTextures, uint32(texture.Params.ID)) {
			return bindingValue{}, false
		}
		value.texture, value.layers = texture.Params.ID, int32(texture.Params.Layers)
	case texture.Name != "":
		value.path = q.drawParams.internPath(texture.Name)
	case texture.Blob.Len() > 0:
		width, height := texture.Params.Width, texture.Params.Height
		if width <= 0 || height <= 0 {
			break
		}
		baked := q.NewTexture(width, height, 1, texture.Params.Format, texture.Params.Mipmaps)
		q.UploadTexture(baked, 0, m.Recti{}, texture.Blob.Data(), texture.Params.CopyData)
		value.texture, value.layers, value.owned = baked.Params.ID, 1, true
	}
	return value, true
}

// durableBuffer resolves a buffer param to the value a set keeps: a durable id
// and its range as they are, inline bytes baked into a buffer the set owns. It
// refuses an id this queue did not mint.
func (q *ResourceQueue) durableBuffer(param *types.ShaderParameterDescr) (bindingValue, bool) {
	buffer := param.Buffer
	value := bindingValue{
		supplied:     true,
		bufferOffset: int32(param.BufferOffset),
		bufferSize:   int32(param.BufferSize),
	}
	switch bytes := buffer.Bytes; {
	case buffer.ID != 0:
		if !hasBit(q.drawParams.durableBuffers, uint32(buffer.ID)) {
			return bindingValue{}, false
		}
		value.buffer = buffer.ID
	case bytes.Len() > 0:
		baked := q.UploadBuffer(q.NewBuffer(), bytes.Data(), buffer.CopyData)
		value.buffer, value.owned = baked.ID, true
	}
	return value, true
}

// releaseOwned releases the resource a value baked from inline bytes, if it
// did.
func (q *ResourceQueue) releaseOwned(value *bindingValue) {
	if !value.owned {
		return
	}
	if value.texture != 0 {
		q.ReleaseTexture(types.BakedTexture(value.texture, 0, 0))
	}
	if value.buffer != 0 {
		q.ReleaseBuffer(types.BufferDescrWithId(value.buffer, 0))
	}
	value.owned = false
}

// claimValues takes a run of count zeroed slots, reusing a released set's run
// of the same size when there is one.
func (s *drawParamsStore) claimValues(count int) span {
	if i := slices.IndexFunc(s.freeValues, func(free span) bool { return int(free.count) == count }); i >= 0 {
		run := s.freeValues[i]
		s.freeValues = slices.Delete(s.freeValues, i, i+1)
		clear(s.values[run.start : run.start+run.count])
		return run
	}
	start := len(s.values)
	s.values = append(s.values, make([]bindingValue, count)...)
	return span{start: int32(start), count: int32(count)}
}

// claimBytes takes a run of size zeroed bytes, on claimValues' terms. A run
// starts aligned, so every uniform in it does.
func (s *drawParamsStore) claimBytes(size int) span {
	if size == 0 {
		return span{}
	}
	if i := slices.IndexFunc(s.freeBytes, func(free span) bool { return int(free.count) == size }); i >= 0 {
		run := s.freeBytes[i]
		s.freeBytes = slices.Delete(s.freeBytes, i, i+1)
		clear(s.bytes[run.start : run.start+run.count])
		return run
	}
	start := alignUniform(len(s.bytes))
	s.bytes = append(s.bytes, make([]byte, start+size-len(s.bytes))...)
	return span{start: int32(start), count: int32(size)}
}

// internPath returns a path's index in the path table, plus one.
func (s *drawParamsStore) internPath(path string) int32 {
	if index, ok := s.pathIndex[path]; ok {
		return index
	}
	if s.pathIndex == nil {
		s.pathIndex = map[string]int32{}
	}
	s.paths = append(s.paths, path)
	index := int32(len(s.paths))
	s.pathIndex[path] = index
	return index
}

// setBit and hasBit keep a dense id table, one bit an id.
func setBit(bits *[]uint64, id uint32) {
	word := int(id / 64)
	if word >= len(*bits) {
		*bits = append(*bits, make([]uint64, word+1-len(*bits))...)
	}
	(*bits)[word] |= 1 << (id % 64)
}

func hasBit(bits []uint64, id uint32) bool {
	word := int(id / 64)
	return word < len(bits) && bits[word]&(1<<(id%64)) != 0
}
