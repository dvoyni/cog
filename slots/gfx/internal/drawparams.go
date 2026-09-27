package internal

import (
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// setState is where one set of draw params is in its life. The zero state is an
// id NewDrawParams never handed out, which is what a gap in any table reads as.
type setState uint32

const (
	setUnknown setState = iota
	setLive
	// setFailed is a set whose creation was refused - its shader had no
	// program. It exists, so naming it is no mistake, and it draws nothing.
	setFailed
	setReleased
)

// bindingValue is one binding's value in a set or in a frame's version of one:
// a slot of the pointer-free binding table, in the order of its shader
// program's bindings. Which fields mean anything follows from the binding's
// kind, which the program says; the value does not repeat it.
type bindingValue struct {
	// supplied is whether a param gave this value. In a version it is also
	// the source: an unsupplied slot is the set's own, read as the render finds
	// it, and a supplied one is the frame's.
	supplied bool
	// owned marks a resource a durable set baked from a param's inline bytes:
	// the set releases it when the value is replaced or the set released.
	owned bool
	// offset and size place a uniform's bytes in the byte arena of whatever
	// holds the value - the ResourceQueue's for a set, the OpQueue's for a
	// version.
	offset, size int32
	texture      types.TextureID
	// path is a texture named by resource path, as an index plus one into the
	// path table of whatever holds the value, and zero for none. The render
	// thread's texture cache resolves it, as it resolves every path.
	path int32
	// layers is how many layers the texture was allocated with, and zero when
	// its descriptor could not say; see TextureDescr.Layers.
	layers       int32
	sampler      types.SamplerDesc
	buffer       types.BufferID
	bufferOffset int32
	bufferSize   int32
}

// drawParamsRegistry is what the OpQueue knows of every set: its program and
// its state. It is the one piece of a set a queue other than the ResourceQueue
// reads, and it is read without a lock.
//
// SetDrawParams resolves names against a set's program, and it is called from
// Systems that hold the OpQueue and not the ResourceQueue. Taking the
// ResourceQueue's lock there would widen every such System's lock set, so the
// ResourceQueue publishes each set here instead: the program is immutable and
// written before the state, and the state is atomic, so a reader that sees a
// set live sees its program. The table grows by chunks behind an atomic
// pointer and entries never move, so a reader never races a writer growing it.
// Every write happens under the ResourceQueue's own lock, so there is one
// writer at a time.
type drawParamsRegistry struct {
	chunks atomic.Pointer[[]*setChunk]
}

// setChunkBits sizes a chunk of the registry: 256 sets.
const setChunkBits = 8

type setChunk [1 << setChunkBits]setEntry

// setEntry is one set as the registry holds it.
type setEntry struct {
	program shader.ShaderProgram
	state   atomic.Uint32
}

// entry returns set id's entry, or nil for an id past every chunk. A nil
// registry - a queue built outside the plugin - knows no set.
func (r *drawParamsRegistry) entry(id uint32) *setEntry {
	if r == nil {
		return nil
	}
	chunks := r.chunks.Load()
	if chunks == nil || int(id>>setChunkBits) >= len(*chunks) {
		return nil
	}
	return &(*chunks)[id>>setChunkBits][id&(1<<setChunkBits-1)]
}

// state reads a set's state, setUnknown for an id nothing created, and its
// program when it is live.
func (r *drawParamsRegistry) state(id uint32) (setState, shader.ShaderProgram) {
	entry := r.entry(id)
	if entry == nil {
		return setUnknown, shader.ShaderProgram{}
	}
	state := setState(entry.state.Load())
	if state != setLive {
		return state, shader.ShaderProgram{}
	}
	return state, entry.program
}

// publish makes a new set visible: its program first, then its state.
func (r *drawParamsRegistry) publish(id uint32, program shader.ShaderProgram, state setState) {
	var chunks []*setChunk
	if loaded := r.chunks.Load(); loaded != nil {
		chunks = *loaded
	}
	if index := int(id >> setChunkBits); index >= len(chunks) {
		grown := make([]*setChunk, index+1)
		copy(grown, chunks)
		for i := len(chunks); i <= index; i++ {
			grown[i] = new(setChunk)
		}
		r.chunks.Store(&grown)
	}
	entry := r.entry(id)
	entry.program = program
	entry.state.Store(uint32(state))
}

// release marks a set released. Its program stays where it was, since a reader
// that loaded the state before this may still be reading it.
func (r *drawParamsRegistry) release(id uint32) {
	if entry := r.entry(id); entry != nil {
		entry.state.Store(uint32(setReleased))
	}
}

// drawParamsFault is one of the programmer mistakes a set call can make.
type drawParamsFault uint8

const (
	drawParamsFaultNotLive drawParamsFault = iota
	drawParamsFaultNoShader
	drawParamsFaultUnknownBinding
	drawParamsFaultKind
	drawParamsFaultSize
	drawParamsFaultTemporary
)

// drawParamsReportKey is the report-once key of one mistake about one set's
// one binding. Set ids are never reused, so it names exactly one mistake for the
// engine's life, and one repeated every frame is said once.
type drawParamsReportKey struct {
	set       uint32
	parameter string
	fault     drawParamsFault
	call      string
}

// reportDrawParams reports one mistake about a set once.
func reportDrawParams(k kernel.Kernel, set uint32, parameter string, fault drawParamsFault, call string, err error) {
	k.ReportErrorOnce(drawParamsReportKey{set: set, parameter: parameter, fault: fault, call: call}, err)
}

// reportSetNotLive reports a call naming a set that is not live.
func reportSetNotLive(k kernel.Kernel, set uint32, state setState, call string) {
	reportDrawParams(k, set, "", drawParamsFaultNotLive, call,
		types.ErrDrawParamsNotLive{Set: set, Call: call, Released: state == setReleased})
}

// paramSlot resolves one param against a set's program: the slot it fills and
// the binding there, or the fault and the report saying why it fills none.
//
// The check is the same for a set and its version: the binding exists, the
// param is of its kind, and uniform bytes are its reflected size. Their layout
// was checked once per Go type when the param was built.
func paramSlot(program shader.ShaderProgram, param *types.ShaderParameterDescr, call string) (int, *shader.ShaderResource, drawParamsFault, error) {
	slot, ok := shader.ProgramBindingIndex(program, param.Name)
	if !ok {
		return 0, nil, drawParamsFaultUnknownBinding, types.ErrDrawParamUnknown{Shader: program.Label(), Parameter: param.Name, Call: call}
	}
	binding := &shader.ProgramBindings(program)[slot]
	kind := param.Kind
	declared, fills := bindingFilledBy(binding.Kind, kind)
	if !fills {
		return 0, nil, drawParamsFaultKind, types.ErrParameterKindMismatch{
			Shader: program.Label(), Parameter: param.Name, Supplied: kind.String(), Declared: declared,
		}
	}
	// A size the reflection could not give is zero, and zero checks nothing
	// rather than refusing every value.
	if size := param.ValueSize(); kind.IsValue() && binding.Size > 0 && size != binding.Size {
		return 0, nil, drawParamsFaultSize, types.ErrUniformSizeMismatch{
			Shader: program.Label(), Parameter: param.Name, Supplied: size, Declared: binding.Size,
		}
	}
	return slot, binding, 0, nil
}

// bindingFilledBy is the kind a binding declares as a report names it, and
// whether a param of kind fills it. A uniform takes any value kind: its bytes
// are its own size whatever built them.
func bindingFilledBy(binding shader.ResourceKind, kind types.ShaderParameterKind) (string, bool) {
	switch binding.Base() {
	case shader.ResourceUniformBuffer:
		return "a uniform", kind.IsValue()
	case shader.ResourceSampler:
		return "a sampler", kind == types.ShaderParameterKindSampler
	case shader.ResourceStorageBuffer:
		return "a storage buffer", kind == types.ShaderParameterKindBuffer
	}
	return "a texture", kind == types.ShaderParameterKindTexture
}

// uniformArenaAlign is where each uniform's bytes start in a set's or a
// version's byte arena. It is WGSL's widest uniform alignment, so a value is
// copied out to the frame's uniform arena as it lies.
const uniformArenaAlign = 16

func alignUniform(n int) int {
	return (n + uniformArenaAlign - 1) / uniformArenaAlign * uniformArenaAlign
}
