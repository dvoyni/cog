package scene

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// overrideRecord merges a draw's OverrideParams into its own copy of the
// bundled PBR record, by name.
//
// This is one of the two destinations an override has, and the only one scene
// resolves itself. The record is a bound range of the frame's material arena
// rather than a set of reflected uniforms - the binding is the addressing - so
// gfx never sees its members and cannot match a parameter against them. The
// other destination needs no code at all: an override reaches the draw's gfx
// parameter list, and gfx already resolves a draw parameter over a material one
// of the same name against the reflected layout of the entry's own shader.
//
// A name neither destination declares is ignored rather than reported, and that
// silence is load-bearing: OverrideParams broadcasts to every material the draw
// binds, and an alphaMode MASK shadow shader declares baseColorTexture and
// alphaCutoff where an OPAQUE one declares neither. A report would fire on the
// materials that legitimately do not carry the name.
func overrideRecord(record *scenePbrRecord, params []gfx.ParameterDescr) {
	for i := range params {
		vec, scalar := record.member(params[i].Name())
		switch {
		case vec != nil:
			if color, ok := params[i].ColorValue(); ok {
				*vec = m.Vec4{X: color.R, Y: color.G, Z: color.B, W: color.A}
			} else if value, ok := params[i].VecValue(); ok {
				*vec = value
			}
		case scalar != nil:
			if value, ok := params[i].FloatValue(); ok {
				*scalar = value
			}
		}
	}
}

// member locates the record member one parameter name addresses, as either a
// vec4 or a scalar destination. Both are nil for a name the record has no
// member for.
//
// The names are the shader's, which are glTF's verbatim, which is the whole
// point of naming them that way: the loader maps 1:1 with no translation table
// to drift and the glTF specification is the parameter documentation.
//
// uvSets and pad are deliberately absent. pad is not a member anyone means, and
// uvSets is a packed five-bit selector with no parameter kind that expresses it
// - which TEXCOORD set a slot samples is the file's statement about its own
// mesh, not a per-draw knob.
func (r *scenePbrRecord) member(name string) (*m.Vec4, *float32) {
	switch name {
	case "baseColorFactor":
		return &r.BaseColorFactor, nil
	case "emissiveFactor":
		return &r.EmissiveFactor, nil
	case "metallicFactor":
		return nil, &r.MetallicFactor
	case "roughnessFactor":
		return nil, &r.RoughnessFactor
	case "normalScale":
		return nil, &r.NormalScale
	case "occlusionStrength":
		return nil, &r.OcclusionStrength
	case "alphaCutoff":
		return nil, &r.AlphaCutoff
	}
	// The five slots' transforms and rotations are one table rather than ten
	// more cases, because they are already a table everywhere else in the
	// package and a second copy of the names is one more thing to drift.
	for slot := range pbrSlots {
		switch name {
		case pbrSlots[slot].transform:
			return &r.Transforms[slot], nil
		case pbrSlots[slot].rotation:
			return nil, &r.Rotations[slot]
		}
	}
	return nil, nil
}
