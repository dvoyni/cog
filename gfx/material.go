package gfx

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr struct {
	shader ShaderDescr
	params []ParameterDescr
	state  MaterialState
}

// MaterialState controls fixed render-pipeline state. Depth compare and depth
// write are independent because the states 3D needs most - test but do not
// write, or test with another compare - are inexpressible as one flag. Every
// zero value is both the WebGPU default and what the backend did before the
// field existed, so MaterialState{} renders as it always has.
type MaterialState struct {
	Blend        BlendMode
	DepthCompare CompareFunc
	DepthWrite   bool
	Cull         CullMode
	FrontFace    FrontFace
}

// The states the engine's three passes are made of: opaque geometry, then
// transparent geometry over it, then 2D on top of everything.
var (
	StateOpaque3D      = MaterialState{Blend: BlendOpaque, DepthCompare: CompareLess, DepthWrite: true, Cull: CullBack}
	StateTransparent3D = MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess}
	StateOverlay2D     = MaterialState{}
)

// Material describes a material from a shader and its named parameters. It
// depth-tests and writes, which is what an opaque draw wants; a draw that wants
// anything else names its state through MaterialWithState.
func Material(shader ShaderDescr, params ...ParameterDescr) MaterialDescr {
	return MaterialWithState(shader, MaterialState{Blend: BlendAlpha, DepthCompare: CompareLess, DepthWrite: true}, params...)
}

// MaterialWithState describes a material with explicit fixed pipeline state.
func MaterialWithState(shader ShaderDescr, state MaterialState, params ...ParameterDescr) MaterialDescr {
	return MaterialDescr{shader: shader, params: params, state: state}
}

// Clone snapshots the material parameter descriptors while preserving shader
// and fixed pipeline state. Underlying buffer/texture byte ownership continues
// to follow each descriptor's copyData policy when the material is recorded.
func (m MaterialDescr) Clone() MaterialDescr {
	m.params = append([]ParameterDescr(nil), m.params...)
	return m
}

// CloneTo snapshots the material parameter descriptors into arena and returns
// both the clone and the extended arena.
func (m MaterialDescr) CloneTo(arena []ParameterDescr) (MaterialDescr, []ParameterDescr) {
	start := len(arena)
	arena = append(arena, m.params...)
	m.params = arena[start:]
	return m, arena
}
