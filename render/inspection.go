package render

import "github.com/dvoyni/cog/metronome"

// Inspection is one frame written down in render's terms: its beat; its
// command lists by order; the textures, buffers and shaders it made and the
// writes into them; the temporaries it took from render's pools and the
// materials it versioned; the pipelines and bind groups it made; and its
// reports. Frame.ReadInspection answers the one Frame.RequestInspection
// asked for, and render/rendertest's reader one for each frame gfx's
// recording adapter recorded, which gfx's recording cannot show all of:
// the reader leaves the temporaries, the versioned materials and each
// draw's Material empty, and its harness fills the beat and the reports
// from what it saw of the frame's beat.
type Inspection struct {
	Beat         metronome.Beat
	CommandLists []InspectedCommandList
	// MadeAssets are the textures, buffers and shaders whose gfx objects
	// the frame made, loaded, made from a slice or, for a texture, taken
	// from the pool when it had none free, in the order made.
	MadeAssets []InspectedAsset
	// Writes are the writes into the frame's textures and buffers that land
	// before its command lists: Inventory's, a made asset's first contents,
	// and a load's body, in the order written.
	Writes []InspectedWrite
	// TemporaryTextures, TemporaryBuffers and TemporaryMeshes are what the
	// frame's lists took from render's pools, each list's in the order
	// taken.
	TemporaryTextures []Texture
	TemporaryBuffers  []BufferRange
	TemporaryMeshes   []Mesh
	// VersionedMaterials are the materials a change after a draw versioned
	// in the frame, in the order versioned.
	VersionedMaterials []Material
	// MadePipelines and MadeBindGroups are the pipelines and bind groups
	// the frame made, in the order made: a first draw's or dispatch's.
	MadePipelines  []InspectedPipeline
	MadeBindGroups []InspectedBindGroup
	// Reports are the reports render logged while the frame recorded, in
	// the order logged.
	Reports []Report
}

// InspectedCommandList is one command list of an inspected frame: its
// label; and its passes, its compute passes, its copies and buffer clears
// and its readbacks, each in the order recorded, its Position saying where
// it stands among all four.
type InspectedCommandList struct {
	Label         string
	Passes        []InspectedPass
	ComputePasses []InspectedComputePass
	Copies        []InspectedCopy
	Readbacks     []InspectedReadback
}

// InspectedPass is one pass of an inspected command list: its position,
// its index among the list's passes, compute passes, copies and readbacks,
// in the order recorded; its label; its colour targets and depth-stencil
// target with their loads, stores and resolves, a window's at mip level and
// layer zero; and its draws, in the order recorded.
type InspectedPass struct {
	Position     int
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
	Draws        []InspectedDraw
}

// InspectedDraw is one draw of an inspected pass: the material it drew with;
// its pass parameters, one for each binding of its shader's @group(0), and
// its draw parameters, one for each of @group(2), by binding number; its
// instance buffers, each a range of a buffer at the offset it was bound,
// with no size, in the order bound; and its counts, an indexed draw's
// IndexCount and any other's VertexCount, with its instances.
type InspectedDraw struct {
	Material                                              InspectedMaterial
	PassParameters, DrawParameters                        []InspectedParameter
	InstanceBuffers                                       []BufferRange
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

// InspectedReadback is one readback of an inspected command list: its
// position, as an InspectedPass's; and what it read, a range of a buffer,
// its size filled in, or a region of a texture, its size filled in.
type InspectedReadback struct {
	Position      int
	SourceBuffer  BufferRange
	SourceTexture Texture
	SourceRegion  TextureRegion
}

// InspectedMaterial is a material as a draw drew with it: the material,
// which render/rendertest's reader leaves zero; its shader, the functions
// its stages ran, an empty fragment one meaning none, its render state, of
// the colour targets the pass has, each value its field's default spelt
// zero, as a State's zero value spells it, and its parameters, one for
// each binding of its shader's @group(1), by binding number.
type InspectedMaterial struct {
	Material                             Material
	Shader                               Shader
	VertexEntryPoint, FragmentEntryPoint string
	State                                State
	Parameters                           []InspectedParameter
}

// InspectedParameter is the value a draw or dispatch bound to one binding,
// a pass's, its material's or its own, by the binding's name, group and
// number: a texture as the view the binding saw of it, its counts,
// dimension and aspect filled in; a sampler, each field at its default
// spelt zero; a range of a buffer, its size filled in, at the offset it
// bound, a dynamic offset included; or the bytes of a uniform block set as
// a value, or bound to a temporary buffer, as the frame's submit found
// them. Default says the binding drew render's default, unbound, refused or
// naming an asset since freed, and holds nothing else.
type InspectedParameter struct {
	Name           string
	Group, Binding uint32
	Default        bool
	TextureView    TextureView
	Sampler        SamplerDescriptor
	Buffer         BufferRange
	Uniform        []byte
}

// InspectedAsset is a texture, a buffer or a shader whose gfx object an
// inspected frame made, named by its handle, with its label: a loaded
// asset's path, or the label its maker gave.
type InspectedAsset struct {
	Texture Texture
	Buffer  Buffer
	Shader  Shader
	Label   string
}

// InspectedWrite is a write into a texture or a buffer of an inspected
// frame: a range of a buffer, or a region of one mip level of a texture,
// over LayerCount layers from the region's.
type InspectedWrite struct {
	Buffer     BufferRange
	Texture    Texture
	Region     TextureRegion
	LayerCount uint32
}

// InspectedPipeline is a pipeline an inspected frame made: its shader, the
// zero Shader for render's own, such as its mip shader; a render pipeline's
// vertex and fragment functions and its render state, as an
// InspectedMaterial's; or a compute pipeline's compute function.
type InspectedPipeline struct {
	Shader                               Shader
	VertexEntryPoint, FragmentEntryPoint string
	State                                State
	ComputeEntryPoint                    string
}

// InspectedBindGroup is a bind group an inspected frame made, by what it
// binds.
type InspectedBindGroup struct {
	Kind BindGroupKind
}
