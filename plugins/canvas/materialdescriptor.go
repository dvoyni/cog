package canvas

import (
	"cmp"

	"github.com/dvoyni/cog/plugins/render"
)

// MaterialDescriptor is what Assets.MakeMaterial makes a 2D material of.
type MaterialDescriptor struct {
	Label string
	// Shader is the path in platform's FS of a whole WGSL file that
	// includes canvas's include, "cog.canvas/canvas.wgsl", which declares
	// every binding of @group(0) and @group(2) and the functions over them,
	// and declares only @group(1), the material's parameters, and, for a
	// material taking an argument, its struct Argument, which canvas
	// preprocesses it with the define CANVAS_ARGUMENT for.
	Shader string
	// VertexEntryPoint is the shader's vertex stage, empty meaning canvas's
	// own, canvas_sprite_vertex, so a material of a fragment stage alone
	// draws sprites and shapes alike. A mesh of canvas.Vertex draws through
	// canvas's mesh vertex stage, canvas_mesh_vertex, whatever it names, and
	// a mesh of another layout through it. FragmentEntryPoint is its
	// fragment stage, empty meaning the shader's only one.
	VertexEntryPoint   string
	FragmentEntryPoint string
	// Blend is how the material's entries blend into what the target holds,
	// their colour premultiplied: zero means BlendNormal(), unlike render's
	// zero, which blends none, so an opaque material is BlendOpaque(). Any
	// other state is taken as it is.
	Blend render.BlendState
	// Argument is the type of the value each entry gives the material,
	// ArgumentOf's, its zero value none. Its layout is checked against the
	// shader's struct Argument as the material is made, and a mismatch is
	// reported once for the shader and the type as
	// ReportKindArgumentLayoutMismatch, the material's entries skipped.
	Argument ArgumentType
	// CullMargin grows the bounds of the material's entries, in world units,
	// zero meaning none, so a vertex stage that moves vertices, as wind
	// sways grass, draws entries whose own bounds lie outside a camera's
	// view.
	CullMargin float32
}

// BlendNormal answers premultiplied colour drawn over what the target
// holds: One, OneMinusSourceAlpha, for colour and alpha alike. An entry
// whose colour is of zero alpha adds it, as a glow does.
func BlendNormal() render.BlendState {
	return makeBlendState(render.BlendFactorOne, render.BlendFactorOneMinusSourceAlpha)
}

// BlendAdditive answers premultiplied colour added to what the target
// holds: One, One.
func BlendAdditive() render.BlendState {
	return makeBlendState(render.BlendFactorOne, render.BlendFactorOne)
}

// BlendMultiply answers what the target holds multiplied by premultiplied
// colour, and kept where it is transparent: Destination, OneMinusSourceAlpha.
func BlendMultiply() render.BlendState {
	return makeBlendState(render.BlendFactorDestination, render.BlendFactorOneMinusSourceAlpha)
}

// BlendScreen answers premultiplied colour screened over what the target
// holds, lightening it: One, OneMinusSource.
func BlendScreen() render.BlendState {
	return makeBlendState(render.BlendFactorOne, render.BlendFactorOneMinusSource)
}

// BlendOpaque answers premultiplied colour replacing what the target
// holds: One, Zero, adding them, written out in full, so it is not the zero
// state, which means BlendNormal(), and render keys it as no blending.
func BlendOpaque() render.BlendState {
	return makeBlendState(render.BlendFactorOne, render.BlendFactorZero)
}

// makeRenderMaterials makes the render materials of d through inventory,
// its shader acquired, with the define CANVAS_ARGUMENT where d takes an
// argument, which the materials hold until they are released, and answers
// them and the shader: one of its own vertex stage, canvas's own for an
// entry drawn as a quad where d names none, and one of canvas's mesh vertex
// stage, for a mesh of canvas.Vertex, both of d's fragment stage and of
// its blending premultiplied, BlendNormal() where d's is zero, with no
// depth test, since the sort key orders entries, and no culling.
func (d MaterialDescriptor) makeRenderMaterials(inventory render.Inventory) (render.Material, render.Material, render.Shader) {
	blend := d.Blend
	if blend == (render.BlendState{}) {
		blend = BlendNormal()
	}
	var state render.State
	state.ColorTargets[0].Blend = blend
	var defines []render.ShaderDefine
	if d.Argument.goType != nil {
		defines = []render.ShaderDefine{{Name: argumentDefine}}
	}
	shader := inventory.AcquireShader(d.Shader, defines...)
	descriptor := render.MaterialDescriptor{
		Label:              d.Label,
		Shader:             shader,
		State:              state,
		VertexEntryPoint:   cmp.Or(d.VertexEntryPoint, spriteVertexEntryPoint),
		FragmentEntryPoint: d.FragmentEntryPoint,
	}
	material := inventory.MakeMaterial(descriptor)
	descriptor.VertexEntryPoint = meshVertexEntryPoint
	meshMaterial := inventory.MakeMaterial(descriptor)
	return material, meshMaterial, shader
}

// makeBlendState answers the state blending colour and alpha alike, by
// sourceFactor and destinationFactor, adding them.
func makeBlendState(sourceFactor, destinationFactor render.BlendFactor) render.BlendState {
	component := render.BlendComponent{SourceFactor: sourceFactor, DestinationFactor: destinationFactor, Operation: render.BlendOperationAdd}
	return render.BlendState{Color: component, Alpha: component}
}
