package render

// Inspection is one frame written down in render's terms: its command lists
// by order. render/rendertest's reader answers one for each frame gfx's
// recording adapter recorded.
type Inspection struct {
	CommandLists []InspectedCommandList
}

// InspectedCommandList is one command list of an inspected frame: its passes,
// in the order recorded.
type InspectedCommandList struct {
	Passes []InspectedPass
}

// InspectedPass is one pass of an inspected command list: its label; its
// colour targets and depth-stencil target with their loads, stores and
// resolves; and its draws, in the order recorded.
type InspectedPass struct {
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
	Draws        []InspectedDraw
}

// InspectedDraw is one draw of an inspected pass: the material it drew with,
// and its counts, an indexed draw's IndexCount and any other's VertexCount,
// with its instances.
type InspectedDraw struct {
	Material                                              InspectedMaterial
	VertexCount, IndexCount, InstanceCount, FirstInstance uint32
}

// InspectedMaterial is a material as a draw drew with it: its shader, the
// functions its stages ran, an empty fragment one meaning none, its render
// state, of the colour targets the pass has, each value its field's default
// spelt zero, as a State's zero value spells it, and its parameters, one for
// each binding of its shader's @group(1), by binding number.
type InspectedMaterial struct {
	Shader                               Shader
	VertexEntryPoint, FragmentEntryPoint string
	State                                State
	Parameters                           []InspectedParameter
}

// InspectedParameter is the value a draw bound to one binding of its
// material, by the binding's name: a texture as the view the binding saw of
// it, its counts, dimension and aspect filled in; a sampler, each field at
// its default spelt zero; a range of a buffer, its size filled in; or the
// bytes of a uniform block set as a value. Default says the binding drew
// render's default, unbound, refused or naming an asset since freed, and
// holds nothing else.
type InspectedParameter struct {
	Name        string
	Default     bool
	TextureView TextureView
	Sampler     SamplerDescriptor
	Buffer      BufferRange
	Uniform     []byte
}
