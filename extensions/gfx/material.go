package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/extensions/gfx/internal"
)

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr = internal.MaterialDescr

// Material describes a material from a shader and its named parameters. It
// depth-tests and writes, which is what an opaque draw wants; a draw that wants
// anything else names its state through MaterialWithState.
func Material(shader ShaderDescr, params ...ParameterDescr) MaterialDescr {
	return internal.Material(shader, params...)
}

// MaterialWithState describes a material with explicit fixed pipeline state.
func MaterialWithState(shader ShaderDescr, state gpu.MaterialState, params ...ParameterDescr) MaterialDescr {
	return internal.MaterialWithState(shader, state, params...)
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
	return internal.FingerprintParams(params)
}
