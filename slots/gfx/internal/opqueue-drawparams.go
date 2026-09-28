package internal

import (
	"slices"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// setCursor is one set's current version in the frame being recorded.
type setCursor struct {
	// frame is the queue frame the cursor belongs to; in any other it is
	// stale and the set has no version.
	frame uint64
	// version is where the version's slots start in versionValues, plus one,
	// and zero for none.
	version int32
	// bytesMark is how long versionBytes was when the version was made. Bytes
	// past it are the version's own, and it may overwrite them; bytes before it
	// may be shared with a version a draw captured, and it may not.
	bytesMark int32
	// captured is whether a draw has recorded the version. A captured version
	// is copied before it changes; an uncaptured one is patched in place.
	captured bool
}

// SetDrawParams changes a set for the rest of this frame, from this point on:
// every draw of it recorded after the call sees the values given here, and the
// next frame starts from the set's own values again. It is frame-local because
// the OpQueue is: a frame may be dropped, and a set's own values must not
// depend on which frames survived.
//
// A draw sees the values the set had when it was recorded, however the passes
// are ordered. So a change makes a version: the bindings it names get the
// frame's values and every other binding stays the set's, copy-on-write per
// binding. A version no draw has recorded yet is patched in place, so several
// Systems each setting a few bindings before the draws pay one copy.
//
// Params are whole bindings, checked as NewDrawParams checks them; a temporary
// is legal here, since it lives exactly as long as the version. The mistakes a
// call can make are reported through k once each, keyed by the set and the
// binding, and the param ignored. A set whose creation failed takes nothing and
// says nothing.
func (q *OpQueue) SetDrawParams(k kernel.Kernel, set types.DrawStateID, params ...types.ShaderParameterDescr) {
	id := uint32(set)
	state, program := q.sets.state(id)
	if state != setLive {
		if state != setFailed {
			reportSetNotLive(k, id, state, "SetDrawParams")
		}
		return
	}
	count := len(shader.ProgramBindings(program))
	cursor := q.cursor(id)
	if cursor.frame != q.frame || cursor.version == 0 || cursor.captured {
		start := len(q.versionValues)
		if cursor.frame == q.frame && cursor.version != 0 {
			// Copy the version the draws so far captured, sharing its bytes.
			previous := int(cursor.version - 1)
			q.versionValues = append(q.versionValues, q.versionValues[previous:previous+count]...)
		} else {
			q.versionValues = slices.Grow(q.versionValues, count)[:start+count]
			clear(q.versionValues[start:])
		}
		*cursor = setCursor{frame: q.frame, version: int32(start + 1), bytesMark: int32(len(q.versionBytes))}
	}
	values := q.versionValues[cursor.version-1 : int(cursor.version-1)+count]
	for i := range params {
		param := &params[i]
		slot, _, fault, err := paramSlot(program, param, "SetDrawParams")
		if err != nil {
			reportDrawParams(k, id, param.Name, fault, "SetDrawParams", err)
			continue
		}
		q.setVersionValue(&values[slot], param, cursor.bytesMark)
	}
}

// setVersionValue writes one param into a version's slot. Bytes are written
// over the slot's own when this version wrote them, and appended otherwise,
// since earlier bytes may be a captured version's too.
func (q *OpQueue) setVersionValue(value *bindingValue, param *types.ShaderParameterDescr, bytesMark int32) {
	switch param.Kind {
	case types.ShaderParameterKindRaw, types.ShaderParameterKindFloat, types.ShaderParameterKindVec4,
		types.ShaderParameterKindMat4, types.ShaderParameterKindColor:
		bytes := param.Bytes()
		if value.supplied && value.offset >= bytesMark && int(value.size) == len(bytes) {
			copy(q.versionBytes[value.offset:], bytes)
			return
		}
		end := len(q.versionBytes)
		start := alignUniform(end)
		q.versionBytes = slices.Grow(q.versionBytes, start+len(bytes)-end)[:start+len(bytes)]
		clear(q.versionBytes[end:start])
		copy(q.versionBytes[start:], bytes)
		*value = bindingValue{supplied: true, offset: int32(start), size: int32(len(bytes))}
	case types.ShaderParameterKindSampler:
		*value = bindingValue{supplied: true, sampler: param.Sampler}
	case types.ShaderParameterKindTexture:
		texture := q.bakeTextureIfNeeded(param.Texture)
		*value = bindingValue{supplied: true, texture: texture.Params.ID, layers: int32(texture.Params.Layers)}
		if texture.Params.ID == 0 && texture.Name != "" {
			q.versionPaths = append(q.versionPaths, texture.Name)
			value.path = int32(len(q.versionPaths))
		}
	case types.ShaderParameterKindBuffer:
		buffer := q.bakeBufferIfNeeded(param.Buffer, types.BufferStorage)
		*value = bindingValue{
			supplied: true, buffer: buffer.ID,
			bufferOffset: int32(param.BufferOffset),
			bufferSize:   int32(param.BufferSize),
		}
	}
}

// cursor returns set id's cursor, growing the table to reach it. The table is
// indexed by id and outlives the frame: a stale frame is what says a cursor is
// empty, so reset clears nothing.
func (q *OpQueue) cursor(id uint32) *setCursor {
	if int(id) >= len(q.cursors) {
		q.cursors = append(q.cursors, make([]setCursor, int(id)+1-len(q.cursors))...)
	}
	return &q.cursors[id]
}

// Draw records a draw of mesh into pass through a set of draw params, which
// replays the mesh instances times starting at firstInstance; a plain draw is
// 1, 0, and instances below 1 draw once. The draw sees the set as it stands in
// this frame at this call - its own values, and whatever SetDrawParams changed
// before it.
//
// Per-instance data is packed outside gfx and bound as a storage buffer the
// shader indexes by instance_index, which WebGPU starts at firstInstance, so a
// batch reads its own slice of a shared instance arena with no offset plumbing
// of its own.
//
// It takes no kernel: what can go wrong with a draw is the frame's to report. A
// draw whose pass was not declared this frame is dropped and counted, and one
// naming a set that is not live is dropped silently when the frame is
// rendered - it is a frame rendered after what it names was let go.
func (q *OpQueue) Draw(pass types.PassID, mesh types.MeshDescr, set types.DrawStateID, instances, firstInstance int) {
	index := q.passIndex(pass)
	if index < 0 {
		q.strayDraws++
		return
	}
	o := DrawOp{Set: set, Instances: instances, FirstInstance: firstInstance}
	if id := uint32(set); int(id) < len(q.cursors) {
		if cursor := &q.cursors[id]; cursor.frame == q.frame && cursor.version != 0 {
			o.Version = cursor.version
			cursor.captured = true
		}
	}
	o.Mesh = mesh
	o.Mesh.Layout = q.copyVertexAttrs(mesh.Layout)
	o.Mesh.Vertices = q.bakeBufferIfNeeded(mesh.Vertices, types.BufferVertex)
	o.Mesh.Indices = q.bakeBufferIfNeeded(mesh.Indices, types.BufferIndex)
	q.passes[index].Draws = append(q.passes[index].Draws, o)
}
