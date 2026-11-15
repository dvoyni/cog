package render

// Inspection is one frame written down in render's terms: its command lists
// by order. render/rendertest's reader answers one for each frame gfx's
// recording adapter recorded.
type Inspection struct {
	CommandLists []InspectedCommandList
}

// InspectedCommandList is one command list of an inspected frame: its passes,
// its compute passes and its copies and buffer clears, each in the order
// recorded, its Position saying where it stands among all three.
type InspectedCommandList struct {
	Passes        []InspectedPass
	ComputePasses []InspectedComputePass
	Copies        []InspectedCopy
}

// InspectedPass is one pass of an inspected command list: its position,
// its index among the list's passes, compute passes and copies, in the
// order recorded; its label; its colour targets and depth-stencil target
// with their loads, stores and resolves; and its draws, in the order
// recorded.
type InspectedPass struct {
	Position     int
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
	Draws        []InspectedDraw
}

// InspectedDraw is one draw of an inspected pass: the material it drew with;
// its pass parameters, one for each binding of its shader's @group(0), and
// its draw parameters, one for each of @group(2), by binding number; and its
// counts, an indexed draw's IndexCount and any other's VertexCount, with its
// instances.
type InspectedDraw struct {
	Material                                              InspectedMaterial
	PassParameters, DrawParameters                        []InspectedParameter
	VertexCount, IndexCount, InstanceCount, FirstInstance uint32
}

// InspectedComputePass is one compute pass of an inspected command list: its
// position, as an InspectedPass's; its label; and its dispatches, in the
// order recorded.
type InspectedComputePass struct {
	Position   int
	Label      string
	Dispatches []InspectedDispatch
}

// InspectedDispatch is one dispatch of an inspected compute pass: its
// shader, the compute function it ran, its parameters, one for each binding
// of its shader, by group and binding number, and its workgroup counts.
type InspectedDispatch struct {
	Shader     Shader
	EntryPoint string
	Parameters []InspectedParameter
	X, Y, Z    uint32
}

// InspectedCopy is one copy or buffer clear of an inspected command list:
// its position, as an InspectedPass's; and its source and its destination,
// each a range of a buffer or a region of a texture, sizes filled in. A
// clear has no source, and zeroes its destination buffer's range. Of a copy
// between a buffer and a texture, the buffer's range gives its offset alone,
// its rows laid out as CommandList.CopyBufferToTexture says.
type InspectedCopy struct {
	Position                          int
	SourceBuffer, DestinationBuffer   BufferRange
	SourceTexture, DestinationTexture Texture
	SourceRegion, DestinationRegion   TextureRegion
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

// InspectedParameter is the value a draw bound to one binding, a pass's, its
// material's or its own, by the binding's name: a texture as the view the
// binding saw of it, its counts, dimension and aspect filled in; a sampler,
// each field at its default spelt zero; a range of a buffer, its size filled
// in, at the offset it bound, a dynamic offset included; or the bytes of a
// uniform block set as a value, or bound to a temporary buffer, as the
// frame's submit found them. Default says the binding drew render's
// default, unbound, refused or naming an asset since freed, and holds
// nothing else.
type InspectedParameter struct {
	Name        string
	Default     bool
	TextureView TextureView
	Sampler     SamplerDescriptor
	Buffer      BufferRange
	Uniform     []byte
}
