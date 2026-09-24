package internal

import (
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
func (p *plugin) shadeSprite(materials *ScopeMaterials, material *gfx.MaterialDescr, fingerprint uint64, params []gfx.ParameterDescr) spriteShading {
	shading := spriteShading{}
	var scope []gfx.ParameterDescr
	shading.material, shading.fingerprint, scope = materials.Resolve(FamilySprite, material, fingerprint)
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

// builtinQuadLayoutID is the vertex-layout identity the plugin's own quads carry
// into the triangles batcher.
//
// An app's DrawTriangles op takes its layout id from the recording queue, which
// hands them out from zero in first-use order, so a negative constant can never
// collide with one. The plugin's quads are built here rather than recorded
// there and have no queue-assigned id to use; sharing one constant is what lets
// consecutive texture or tiled quads recognise each other as the same layout.
//
// The cost is that a quad never merges with an app's own DrawTriangles call
// even where that call used canvas.Vertex and the identical shading. That case
// would need the built-in layout to have one identity across both sources -
// TrianglesOp.BuiltinLayout already reports it - and it is a merge that
// has never existed rather than one this loses.
const builtinQuadLayoutID = -1

// shadeQuad resolves the shading of one quad the plugin emits itself: a
// texture-sourced sprite or a tiled one.
//
// The texture and the sampler go into the shading's parameters rather than being
// appended at the draw site, and that is the whole reason these quads batch. A
// parameter list is fingerprinted by value, and gfx hashes a texture parameter by
// its identity and a sampler parameter by its whole filter and address state, so
// two quads over one texture and one sampler agree on the key and merge, while a
// second texture or a repeat-versus-clamp difference splits them. Neither needs a
// key field of canvas's own, and neither can be forgotten: the value is in the
// list the batcher already keys on.
//
// The viewport, layer transform and clip rect are deliberately absent. They are
// batch state, not op state - the batcher holds them as key fields and prepends
// them at flush - and naming them here would put two writers on one parameter.
func (p *plugin) shadeQuad(
	material *gfx.MaterialDescr, fingerprint uint64, texture gfx.TextureDescr, sampler gfx.SamplerDesc,
	params, scope []gfx.ParameterDescr,
) trianglesShading {
	p.quadParams = append(p.quadParams[:0],
		gfx.TextureParam(TextureSlot, texture),
		gfx.SamplerParam(SamplerSlot, sampler),
	)
	p.quadParams = append(p.quadParams, params...)
	p.quadParams = append(p.quadParams, scope...)
	shading := trianglesShading{material: material, fingerprint: fingerprint, params: p.quadParams}
	shading.paramsKey = gfx.FingerprintParams(shading.params)
	return shading
}

// shadeTriangles resolves one triangle draw's shading. Its parameters are one
// list keyed by value rather than two split by frequency: a triangle batch is
// concatenated vertices with no instance index, so a parameter named at the call
// is per material and two values are two draws. That is a rule, not a
// shortcoming of the key - removing it would need somewhere per-vertex to put
// the value, which this geometry does not have.
func (p *plugin) shadeTriangles(materials *ScopeMaterials, op *TrianglesOp) trianglesShading {
	f := FamilyTriangles
	if op.Unkeyed {
		f = FamilyTexture
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
