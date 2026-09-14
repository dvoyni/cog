package internal

import (
	"github.com/dvoyni/cog/bundles/canvas/internal/types"
	"github.com/dvoyni/cog/slots/gfx"
)

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
func (p *plugin) shadeSprite(materials *types.ScopeMaterials, material *gfx.MaterialDescr, fingerprint uint64, params []gfx.ParameterDescr) spriteShading {
	shading := spriteShading{}
	var scope []gfx.ParameterDescr
	shading.material, shading.fingerprint, scope = materials.Resolve(types.FamilySprite, material, fingerprint)
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
func (p *plugin) shadeTriangles(materials *types.ScopeMaterials, op *types.TrianglesOp) trianglesShading {
	f := types.FamilyTriangles
	if op.Unkeyed {
		f = types.FamilyTexture
	}
	shading := trianglesShading{}
	var scope []gfx.ParameterDescr
	shading.material, shading.fingerprint, scope = materials.Resolve(f, op.NamedMaterial(), op.Fingerprint)
	if len(scope) == 0 {
		shading.params = op.Params
	} else {
		p.trianglesParams = append(append(p.trianglesParams[:0], op.Params...), scope...)
		shading.params = p.trianglesParams
	}
	shading.paramsKey = gfx.FingerprintParams(shading.params)
	return shading
}
