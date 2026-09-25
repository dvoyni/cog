package internal

import (
	"hash/maphash"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"
)

// fingerprintSeed is fixed for the process, so a fingerprint compares only
// against others taken in the same process - which is all a per-frame intern
// needs.
var fingerprintSeed = maphash.MakeSeed()

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr struct {
	shader shader.ShaderDescr
	params []ParameterDescr
	state  MaterialState
	// recorded is set by OpQueue.FrameMaterial and zero otherwise; see there.
	recorded frameRecording
}

// frameRecording is what OpQueue.FrameMaterial attaches to the material it
// returns: the queue and the queue frame its params were copied into, where in
// that queue's parameter arena the copy starts, and the shape state of their
// names. The material's own params stay the caller's, so a recording that is
// stale or another queue's draws as the material it was recorded from.
//
// It is held by value: recording happens every frame, and a pointer would be
// an allocation every frame for every material a renderer records.
type frameRecording struct {
	queue *OpQueue
	frame uint64
	start int
	shape uint64
}

// Material describes a material from a shader and its named parameters. It
// depth-tests and writes, which is what an opaque draw wants; a draw that wants
// anything else names its state through MaterialWithState.
func Material(shaderDescr shader.ShaderDescr, params ...ParameterDescr) MaterialDescr {
	return MaterialWithState(shaderDescr, MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess, DepthWrite: true}, params...)
}

// MaterialWithState describes a material with explicit fixed pipeline state.
func MaterialWithState(shaderDescr shader.ShaderDescr, state MaterialState, params ...ParameterDescr) MaterialDescr {
	return MaterialDescr{shader: shaderDescr, params: params, state: state}
}

// Clone snapshots the material parameter descriptors while preserving shader
// and fixed pipeline state. Underlying buffer/texture byte ownership continues
// to follow each descriptor's copyData policy when the material is recorded.
func (m MaterialDescr) Clone() MaterialDescr {
	m.params = append([]ParameterDescr(nil), m.params...)
	m.recorded = frameRecording{}
	return m
}

// CloneTo snapshots the material parameter descriptors into arena and returns
// both the clone and the extended arena.
func (m MaterialDescr) CloneTo(arena []ParameterDescr) (MaterialDescr, []ParameterDescr) {
	start := len(arena)
	arena = append(arena, m.params...)
	m.params = arena[start:]
	m.recorded = frameRecording{}
	return m, arena
}

// State reports the material's fixed pipeline state.
func (m MaterialDescr) State() MaterialState { return m.state }

// Shader reports the shader the material shades with, supply included: one
// path under two supplies is two shaders, so the descriptor answers rather
// than the path alone.
func (m MaterialDescr) Shader() shader.ShaderDescr { return m.shader }

// Params reports the material's own parameters, which a draw's same-named
// parameters override. The slice aliases the material's storage and must not
// be written to.
func (m MaterialDescr) Params() []ParameterDescr { return m.params }

// Fingerprint hashes everything that makes one material different from
// another: the shader by its whole descriptor, the pipeline state, and every
// parameter in order by name, kind and value. Two descriptors with the same
// content fingerprint the same regardless of which backing their parameters
// live in, so a recorder that builds its material inline every draw still sorts
// those draws together.
//
// Inline bytes - a texture's pixels, a buffer's contents, a shader's text -
// hash by identity, pointer and length, rather than by content, so two
// descriptors around different runs count as different materials even when the
// bytes agree. That is the same identity the caches key on, which is the point:
// missing a merge costs a batch, and merging two materials that are two cache
// entries would draw the wrong one.
//
// It lives here rather than beside its consumer because the fields it covers
// are unexported, and a fingerprint that a new field can silently fall out of
// is worse than none.
func (m MaterialDescr) Fingerprint() uint64 {
	var h maphash.Hash
	h.SetSeed(fingerprintSeed)
	// The whole descriptor goes in as one comparable value rather than field by
	// field, because every field of it is part of the shader's identity and so
	// part of the material's: without the supply, two materials differing only
	// in their defines fingerprint the same, merge into one batch, and one of
	// them draws the wrong module.
	maphash.WriteComparable(&h, m.shader)
	writeUint(&h, uint64(m.state.Blend)|uint64(m.state.DepthCompare)<<8|
		uint64(m.state.Cull)<<16|uint64(m.state.FrontFace)<<24|boolBit(m.state.DepthWrite)<<32)
	for i := range m.params {
		m.params[i].fingerprint(&h)
	}
	return h.Sum64()
}

// FingerprintParams hashes a parameter slice in order by name, kind and value,
// under exactly the rules Fingerprint applies to a material's own parameters -
// same seed, same per-kind encoding, inline texture and buffer bytes by pointer
// identity rather than by content.
//
// It exists because a recorder that keys a batch on a draw's parameters cannot
// write the comparison itself: ParameterDescr exposes an accessor for some
// kinds and none for others, so a hand-written type switch would silently
// mis-key every kind it forgot - and mis-keying merges two draws that differ,
// which draws the wrong picture rather than costing a batch.
func FingerprintParams(params []ParameterDescr) uint64 {
	var h maphash.Hash
	h.SetSeed(fingerprintSeed)
	for i := range params {
		params[i].fingerprint(&h)
	}
	return h.Sum64()
}

// shapePrime and shapeOffset are FNV-1a's 64-bit constants.
const (
	shapePrime  uint64 = 1099511628211
	shapeOffset uint64 = 1469598103934665603
)

// ParameterShapeState is the first half of a draw's parameter-shape hash: the
// FNV-1a state after its material's param names, each NUL-terminated, and a
// 0xff separator. ContinueParameterShape finishes it over the draw's own names.
// It is split so that OpQueue.FrameMaterial can take a recorded material's half
// once, and the translator hash only what a draw adds.
func ParameterShapeState(material []ParameterDescr) uint64 {
	hash := shapeOffset
	for i := range material {
		hash = mixShapeName(hash, material[i].name)
	}
	return mixShapeByte(hash, 0xff)
}

// ContinueParameterShape finishes a parameter-shape hash over a draw's own
// param names, from the state ParameterShapeState took of its material's.
func ContinueParameterShape(state uint64, draw []ParameterDescr) uint64 {
	for i := range draw {
		state = mixShapeName(state, draw[i].name)
	}
	return state
}

func mixShapeName(hash uint64, name string) uint64 {
	for i := range len(name) {
		hash = mixShapeByte(hash, name[i])
	}
	return mixShapeByte(hash, 0)
}

func mixShapeByte(hash uint64, value byte) uint64 {
	hash ^= uint64(value)
	return hash * shapePrime
}
