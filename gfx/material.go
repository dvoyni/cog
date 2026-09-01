package gfx

// MaterialDescr describes how to shade a mesh: a shader plus named parameters.
// Build it with Material and the *Param constructors. OpQueue.Draw remaps its
// texture and buffer parameters to baked resource IDs before recording the draw.
type MaterialDescr struct {
	shader ShaderDescr
	params []ParameterDescr
	state  MaterialState
}

// MaterialState controls fixed render-pipeline state.
type MaterialState struct {
	Blend     BlendMode
	DepthTest bool
}

// Material describes a material from a shader and its named parameters.
func Material(shader ShaderDescr, params ...ParameterDescr) MaterialDescr {
	return MaterialWithState(shader, MaterialState{Blend: BlendAlpha, DepthTest: true}, params...)
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
