package gfx

import (
	"encoding/binary"
	"hash/maphash"
	"math"
	"unsafe"
)

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr struct {
	shader ShaderDescr
	params []ParameterDescr
	state  MaterialState
}

// MaterialState controls fixed render-pipeline state. Depth compare and depth
// write are independent because the states 3D needs most - test but do not
// write, or test with another compare - are inexpressible as one flag. Every
// zero value is both the WebGPU default and what the backend did before the
// field existed, so MaterialState{} renders as it always has.
type MaterialState struct {
	Blend        BlendMode
	DepthCompare CompareFunc
	DepthWrite   bool
	Cull         CullMode
	FrontFace    FrontFace
}

// The states the engine's three passes are made of: opaque geometry, then
// transparent geometry over it, then 2D on top of everything.
var (
	StateOpaque3D      = MaterialState{Blend: BlendOpaque, DepthCompare: CompareLess, DepthWrite: true, Cull: CullBack}
	StateTransparent3D = MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess}
	StateOverlay2D     = MaterialState{}
)

// Material describes a material from a shader and its named parameters. It
// depth-tests and writes, which is what an opaque draw wants; a draw that wants
// anything else names its state through MaterialWithState.
func Material(shader ShaderDescr, params ...ParameterDescr) MaterialDescr {
	return MaterialWithState(shader, MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess, DepthWrite: true}, params...)
}

// MaterialWithState describes a material with explicit fixed pipeline state.
func MaterialWithState(shader ShaderDescr, state MaterialState, params ...ParameterDescr) MaterialDescr {
	return MaterialDescr{shader: shader, params: params, state: state}
}

// Clone snapshots the material parameter descriptors while preserving shader
// and fixed pipeline state. Underlying buffer/texture byte ownership continues
// to follow each descriptor's copyData policy when the material is recorded.
func (m MaterialDescr) Clone() MaterialDescr {
	m.params = append([]ParameterDescr(nil), m.params...)
	return m
}

// CloneTo snapshots the material parameter descriptors into arena and returns
// both the clone and the extended arena.
func (m MaterialDescr) CloneTo(arena []ParameterDescr) (MaterialDescr, []ParameterDescr) {
	start := len(arena)
	arena = append(arena, m.params...)
	m.params = arena[start:]
	return m, arena
}

// State reports the material's fixed pipeline state.
func (m MaterialDescr) State() MaterialState { return m.state }

// Fingerprint hashes everything that makes one material different from
// another: the shader by source kind and text-or-path, the pipeline state, and
// every parameter in order by name, kind and value. Two descriptors with the
// same content fingerprint the same regardless of which backing their
// parameters live in, so a recorder that builds its material inline every draw
// still sorts those draws together.
//
// Inline texture and buffer bytes hash by identity - pointer and length -
// rather than by content, so two descriptors around different byte slices
// count as different materials even when the bytes agree. Missing a merge
// costs a batch; merging two materials that differ would draw the wrong one.
//
// It lives here rather than beside its consumer because the fields it covers
// are unexported, and a fingerprint that a new field can silently fall out of
// is worse than none.
func (m MaterialDescr) Fingerprint() uint64 {
	var h maphash.Hash
	h.SetSeed(fingerprintSeed)
	writeUint(&h, uint64(m.shader.source))
	h.WriteString(m.shader.textOrPath)
	writeUint(&h, uint64(m.state.Blend)|uint64(m.state.DepthCompare)<<8|
		uint64(m.state.Cull)<<16|uint64(m.state.FrontFace)<<24|boolBit(m.state.DepthWrite)<<32)
	for i := range m.params {
		m.params[i].fingerprint(&h)
	}
	return h.Sum64()
}

// fingerprintSeed is fixed for the process, so a fingerprint compares only
// against others taken in the same process - which is all a per-frame intern
// needs.
var fingerprintSeed = maphash.MakeSeed()

func (p *ParameterDescr) fingerprint(h *maphash.Hash) {
	h.WriteString(p.name)
	writeUint(h, uint64(p.kind))
	switch p.kind {
	case paramTexture:
		t := &p.texture
		writeUint(h, uint64(t.source)|uint64(t.id)<<8)
		h.WriteString(t.path)
		writeUint(h, uint64(t.width)|uint64(t.height)<<32)
		writeUint(h, uint64(t.format)|boolBit(t.mipmaps)<<8|uint64(len(t.pixels))<<16)
		writeUint(h, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(t.pixels)))))
	case paramBuffer:
		b := &p.buffer
		writeUint(h, uint64(b.source)|uint64(b.id)<<8)
		writeUint(h, uint64(b.size)|uint64(len(b.bytes))<<32)
		writeUint(h, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(b.bytes)))))
		writeUint(h, uint64(p.bufferOffset)|uint64(p.bufferSize)<<32)
	case paramColor:
		writeFloats(h, p.color.R, p.color.G, p.color.B, p.color.A)
	case paramFloat:
		writeFloats(h, p.num)
	case paramVec4:
		writeFloats(h, p.vec.X, p.vec.Y, p.vec.Z, p.vec.W)
	case paramMat4:
		writeFloats(h, p.mat[:]...)
	case paramSampler:
		s := &p.sampler
		writeUint(h, uint64(s.AddressU)|uint64(s.AddressV)<<8|uint64(s.Mag)<<16|
			uint64(s.Min)<<24|uint64(s.Mip)<<32|uint64(s.Anisotropy)<<40|
			boolBit(s.Comparison)<<48|uint64(s.Compare)<<56)
	}
}

func writeUint(h *maphash.Hash, value uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	h.Write(buf[:])
}

func writeFloats(h *maphash.Hash, values ...float32) {
	var buf [4]byte
	for _, value := range values {
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(value))
		h.Write(buf[:])
	}
}

func boolBit(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
