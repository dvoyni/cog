package internal

import (
	"hash/maphash"

	"github.com/dvoyni/cog/slots/gfx"
)

// Material is how a canvas draw shades: a shader descriptor, the fixed pipeline
// state it draws with, and its own parameters - the defaults a draw's and a
// scope's parameters override by name. See canvas.Material.
//
// It is canvas's rather than gfx's because gfx no longer has materials: a gfx
// draw names a durable set of draw params, created once. Canvas keeps this
// descriptor as its public shape until it is rebuilt on scene, and turns each
// distinct one into a shader and a set the first time a batch draws with it.
type Material struct {
	shader gfx.ShaderDescr
	state  gfx.DrawState
	params []gfx.ShaderParameterDescr
}

// NewMaterial describes a material from a shader and its own parameters, with
// an opaque 3D draw's state: alpha blending, depth-tested and written. A
// canvas material almost always wants MaterialWithState and
// gfx.StateOverlay2D instead; see canvas.NewMaterial.
func NewMaterial(shader gfx.ShaderDescr, params ...gfx.ShaderParameterDescr) Material {
	return MaterialWithState(shader, gfx.DrawState{Blend: gfx.BlendAlpha, DepthCompare: gfx.CompareLess, DepthWrite: true}, params...)
}

// MaterialWithState describes a material with explicit fixed pipeline state;
// see canvas.MaterialWithState.
func MaterialWithState(shader gfx.ShaderDescr, state gfx.DrawState, params ...gfx.ShaderParameterDescr) Material {
	return Material{shader: shader, state: state, params: params}
}

// Shader reports the shader the material shades with.
func (m Material) Shader() gfx.ShaderDescr { return m.shader }

// State reports the material's fixed pipeline state.
func (m Material) State() gfx.DrawState { return m.state }

// Params reports the material's own parameters. The slice aliases the
// material's storage and must not be written to.
func (m Material) Params() []gfx.ShaderParameterDescr { return m.params }

// materialSeed is fixed for the process: a fingerprint compares only against
// others taken in the same process, which is all a batch key and the set cache
// need.
var materialSeed = maphash.MakeSeed()

// Fingerprint hashes everything that makes one material different from
// another: the shader by its whole descriptor, supply included, the pipeline
// state, and the parameters in order by name, kind and value, under
// gfx.FingerprintParams's rules. It is both the batch key a material
// contributes and the key of the set canvas draws it through, so two materials
// that fingerprint alike are one set.
func (m *Material) Fingerprint() uint64 {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	maphash.WriteComparable(&h, m.shader)
	maphash.WriteComparable(&h, m.state)
	maphash.WriteComparable(&h, gfx.FingerprintParams(m.params))
	return h.Sum64()
}

// cloneTo snapshots the material's parameters into arena, so a caller mutating
// theirs afterwards does not change what was recorded.
func (m Material) cloneTo(arena []gfx.ShaderParameterDescr) (Material, []gfx.ShaderParameterDescr) {
	start := len(arena)
	arena = append(arena, m.params...)
	m.params = arena[start:]
	return m, arena
}

// Family is sprite or triangles or texture: which built-in a draw replaces.
//
// Sprites and triangles can never be one shader - the sprite path samples the
// atlas, which is a texture_2d_array, and the triangles path samples one
// arbitrary texture_2d - so which family a draw belongs to is a property of the
// draw, and a material belongs to exactly one. The texture family is a third
// built-in rather than a parameter on the triangles one because the difference
// is the shader: triangles.wgsl runs every texel through the key-colour ramp,
// which is right for artwork and silent damage to a rendered image.
type Family uint8

const (
	FamilySprite Family = iota
	FamilyTriangles
	FamilyTexture
)

// MaterialSet is the shading a scope - the queue, a layer, a ui.Frame or a ui
// element subtree - supplies to the draws beneath it that name none of their
// own: one material per family, plus one parameter list shared by all three.
//
// A scope names a set rather than a material because a layer is never one
// family: every interesting layer carries sprites and triangles, and the two can
// never be one shader. Set is a scope word and material is a draw word; at a
// draw the family is already known, so a draw names a single material.
//
// A nil slot keeps its built-in, so a set is an override rather than a
// whole-cloth requirement, and an entirely zero set is the built-ins - which is
// what a scope that names none has.
//
// One parameter list serves all three slots because canvas drops a name the bound
// shader never declared, so a single fade amount reaches the sprite, triangles
// and texture shaders without being written three times. The parameters travel
// with the set rather than living on the material because one material set at
// two values is a real case: a cross-fade is two layers sharing one shader at
// different amounts, and mutating a shared material can only ever express one of
// them.
type MaterialSet struct {
	Sprite    *Material
	Triangles *Material
	Texture   *Material
	Params    []gfx.ShaderParameterDescr
}

// slot reports the material this set supplies for one family, or nil where it
// keeps the built-in.
func (s *MaterialSet) slot(f Family) *Material {
	switch f {
	case FamilySprite:
		return s.Sprite
	case FamilyTriangles:
		return s.Triangles
	}
	return s.Texture
}

// ScopeMaterials is one scope's set - a layer's, or the queue's - with its
// fingerprints taken lazily.
//
// A scope's set is applied at flush rather than positionally, so it reaches
// draws recorded before the call that set it - which is the whole point, because
// the caller that wants a shader over a whole menu runs after every screen has
// recorded. Fingerprinting it there would otherwise be per-draw work, so each
// slot's key is taken on first use: at most three hashes per scope per frame,
// never one per draw. A draw that names its own material keeps the fingerprint
// it was recorded with and never touches this.
type ScopeMaterials struct {
	set MaterialSet
	// has separates a scope that was given a set from one that was not, so an
	// explicitly empty set still stops an outer default rather than reading as
	// "nothing was said". That is how a layer opts out of the queue's set: it
	// names an empty one, whose nil slots keep the built-ins.
	has   bool
	keys  [3]uint64
	taken [3]bool
}

// Has reports whether the scope was given a set at all, an empty one included.
func (l *ScopeMaterials) Has() bool { return l.has }

// Resolve reports the material a draw of one family shades with and the key that
// material contributes to its batch, following draw, then scope, then built-in.
// The caller has already picked the scope: a layer's own set where it has one,
// the queue's otherwise.
//
// scopeParams is the scope's parameter list, and it is empty for a draw that
// named its own material: a scope's material and its parameters are one unit, so
// a draw that has said what it wants takes neither. The alternative - scope
// parameters always applying - turns a layer into a general parameter-injection
// channel, which is not what a material set is.
func (l *ScopeMaterials) Resolve(f Family, draw *Material, drawKey uint64) (material *Material, key uint64, scopeParams []gfx.ShaderParameterDescr) {
	if draw != nil {
		return draw, drawKey, nil
	}
	if l.has {

		if slot := l.set.slot(f); slot != nil {
			if !l.taken[f] {
				l.keys[f], l.taken[f] = slot.Fingerprint(), true
			}
			return slot, l.keys[f], l.set.Params
		}
		return builtinMaterials[f], builtinFingerprints[f], l.set.Params
	}
	return builtinMaterials[f], builtinFingerprints[f], nil
}
