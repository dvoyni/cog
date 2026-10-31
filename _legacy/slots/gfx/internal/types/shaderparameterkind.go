package types

// ShaderParameterKind tags the variant stored in a ShaderParameterDescr. The
// value kinds - raw, float, vec4, mat4 and color - are a uniform binding's
// bytes, named by what built them; the rest are one per kind of binding a
// shader attaches to a bind group.
type ShaderParameterKind uint8

const (
	shaderParameterKindNone ShaderParameterKind = iota
	ShaderParameterKindRaw
	ShaderParameterKindFloat
	ShaderParameterKindVec4
	ShaderParameterKindMat4
	ShaderParameterKindColor
	ShaderParameterKindTexture
	ShaderParameterKindSampler
	ShaderParameterKindBuffer
)

// IsValue reports whether the parameter carries bytes a shader reads out of
// a uniform, as opposed to a binding it attaches to a bind group. It is the
// question a recorder asks to decide whether a parameter can vary per item at
// all: a value can, and a bind group cannot, because there is one per draw.
func (k ShaderParameterKind) IsValue() bool {
	switch k {
	case ShaderParameterKindRaw, ShaderParameterKindFloat, ShaderParameterKindVec4,
		ShaderParameterKindMat4, ShaderParameterKindColor:
		return true
	}
	return false
}

// String names the kind the way an error message and an inspection view want
// it: what a caller wrote, not what the enum is called.
func (k ShaderParameterKind) String() string {
	switch k {
	case ShaderParameterKindRaw:
		return "raw"
	case ShaderParameterKindFloat:
		return "float"
	case ShaderParameterKindVec4:
		return "vec4"
	case ShaderParameterKindMat4:
		return "mat4"
	case ShaderParameterKindColor:
		return "color"
	case ShaderParameterKindTexture:
		return "texture"
	case ShaderParameterKindSampler:
		return "sampler"
	case ShaderParameterKindBuffer:
		return "buffer"
	}
	return "none"
}

// MarshalText names the value in a JSON document, so a snapshot spells it
// rather than numbering it.
func (k ShaderParameterKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }
