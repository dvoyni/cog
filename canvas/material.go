package canvas

import "github.com/dvoyni/cog/gfx"

// family is sprite or triangles or texture: which built-in a draw replaces.
//
// Sprites and triangles can never be one shader - the sprite path samples the
// atlas, which is a texture_2d_array, and the triangles path samples one
// arbitrary texture_2d - so which family a draw belongs to is a property of the
// draw, and a material belongs to exactly one. The texture family is a third
// built-in rather than a parameter on the triangles one because the difference
// is the shader: triangles.wgsl runs every texel through the key-colour ramp,
// which is right for artwork and silent damage to a rendered image.
type family uint8

const (
	familySprite family = iota
	familyTriangles
	familyTexture
)

// MaterialSet is the shading a scope supplies to the draws beneath it that name
// none of their own: one material per family, plus one parameter list shared by
// all three.
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
// One parameter list serves all three slots because gfx drops a name the bound
// shader never declared, so a single fade amount reaches the sprite, triangles
// and texture shaders without being written three times. The parameters travel
// with the set rather than living on the material because one material set at
// two values is a real case: a cross-fade is two layers sharing one shader at
// different amounts, and mutating a shared material can only ever express one of
// them.
type MaterialSet struct {
	Sprite    *gfx.MaterialDescr
	Triangles *gfx.MaterialDescr
	Texture   *gfx.MaterialDescr
	Params    []gfx.ParameterDescr
}

// slot reports the material this set supplies for one family, or nil where it
// keeps the built-in.
func (s *MaterialSet) slot(f family) *gfx.MaterialDescr {
	switch f {
	case familySprite:
		return s.Sprite
	case familyTriangles:
		return s.Triangles
	}
	return s.Texture
}

// layerMaterials is one layer's set with its fingerprints taken lazily.
//
// A per-layer set is applied at flush rather than positionally, so it reaches
// draws recorded before the call that set it - which is the whole point, because
// the caller that wants a shader over a whole menu runs after every screen has
// recorded. Fingerprinting it there would otherwise be per-draw work, so each
// slot's key is taken on first use: at most three hashes per layer per frame,
// never one per draw. A draw that names its own material keeps the fingerprint
// it was recorded with and never touches this.
type layerMaterials struct {
	set MaterialSet
	// has separates a layer that was given a set from one that was not, so an
	// explicitly empty set still stops an outer default rather than reading as
	// "nothing was said".
	has   bool
	keys  [3]uint64
	taken [3]bool
}

// resolve reports the material a draw of one family shades with and the key that
// material contributes to its batch, following draw, then layer, then built-in.
//
// scopeParams is the scope's parameter list, and it is empty for a draw that
// named its own material: a scope's material and its parameters are one unit, so
// a draw that has said what it wants takes neither. The alternative - scope
// parameters always applying - turns a layer into a general parameter-injection
// channel, which is not what a material set is.
func (l *layerMaterials) resolve(f family, draw *gfx.MaterialDescr, drawKey uint64) (material *gfx.MaterialDescr, key uint64, scopeParams []gfx.ParameterDescr) {
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

// shadeSprite resolves one sprite draw's shading: the material it draws with -
// its own, else the layer set's sprite slot, else the built-in - and its
// parameters split by the frequency each one lands at.
//
// A reserved name reaches neither half: tint and keyColor are consumed into the
// instance record, and the texture and sampler are canvas's own bindings. A
// value parameter is per sprite and becomes an array; a texture, sampler or
// buffer has no per-instance form - there is one bind group per draw - so it is
// per batch. The scope's parameters follow the draw's, so the draw wins under
// first-wins.
func (p *Plugin) shadeSprite(materials *layerMaterials, material *gfx.MaterialDescr, fingerprint uint64, params []gfx.ParameterDescr) spriteShading {
	shading := spriteShading{}
	var scope []gfx.ParameterDescr
	shading.material, shading.fingerprint, scope = materials.resolve(familySprite, material, fingerprint)
	p.arrays, p.shared = p.arrays[:0], p.shared[:0]
	for i := range params {
		switch {
		case reservedName(params[i].Name()):
		case params[i].HasValue():
			p.arrays = append(p.arrays, params[i])
		default:
			p.shared = append(p.shared, params[i])
		}
	}
	p.shared = append(p.shared, scope...)
	shading.arrays, shading.shared = p.arrays, p.shared
	shading.sharedKey = gfx.FingerprintParams(shading.shared)
	return shading
}

// shadeTriangles resolves one triangle draw's shading. Its parameters are one
// list keyed by value rather than two split by frequency: a triangle batch is
// concatenated vertices with no instance index, so a parameter named at the call
// is per material and two values are two draws. That is a rule, not a
// shortcoming of the key - removing it would need somewhere per-vertex to put
// the value, which this geometry does not have.
func (p *Plugin) shadeTriangles(materials *layerMaterials, op *trianglesOp) trianglesShading {
	f := familyTriangles
	if op.unkeyed {
		f = familyTexture
	}
	shading := trianglesShading{}
	var scope []gfx.ParameterDescr
	shading.material, shading.fingerprint, scope = materials.resolve(f, op.namedMaterial(), op.fingerprint)
	if len(scope) == 0 {
		shading.params = op.params
	} else {
		p.trianglesParams = append(append(p.trianglesParams[:0], op.params...), scope...)
		shading.params = p.trianglesParams
	}
	shading.paramsKey = gfx.FingerprintParams(shading.params)
	return shading
}
