package render

// MaterialDescriptor is what a material is made of: its shader, a variant
// acquired with its defines; its render state; and the functions of the
// shader its vertex and fragment stages run, an empty name meaning the
// shader's only one of that stage. A shader with no @fragment function and
// no fragment entry point named makes a material with no fragment stage. Its
// label names it in reports. A material owns nothing it names.
type MaterialDescriptor struct {
	Label              string
	Shader             Shader
	State              State
	VertexEntryPoint   string
	FragmentEntryPoint string
}
