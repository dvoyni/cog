package gfx

import (
	"fmt"
	"strings"
)

// VertexInput is one input a vertex entry point reads at a location, as the
// backend reflects it: its scalar type, and its component count, 1 for a
// scalar.
type VertexInput struct {
	Location   uint32
	Type       ScalarType
	Components uint32
}

// name answers input's type as WGSL spells it: f32, vec3<f32>.
func (input VertexInput) name() string {
	if input.Components <= 1 {
		return input.Type.name()
	}
	return fmt.Sprintf("vec%d<%s>", input.Components, input.Type.name())
}

// ScalarType is the scalar type of a shader value, or of a vector's
// components.
type ScalarType uint32

const (
	ScalarTypeUndefined ScalarType = 0x0
	ScalarTypeFloat32   ScalarType = 0x1
	ScalarTypeFloat16   ScalarType = 0x2
	ScalarTypeSint32    ScalarType = 0x3
	ScalarTypeUint32    ScalarType = 0x4
)

// name answers scalarType as WGSL spells it.
func (scalarType ScalarType) name() string {
	switch scalarType {
	case ScalarTypeFloat32:
		return "f32"
	case ScalarTypeFloat16:
		return "f16"
	case ScalarTypeSint32:
		return "i32"
	case ScalarTypeUint32:
		return "u32"
	}
	return fmt.Sprintf("ScalarType(%d)", scalarType)
}

// checkVertexInputs answers ErrVertexMismatch, naming the input and the
// attribute, for an input of inputs that no attribute of state's buffers
// feeds: none at its location, or one whose format is of another type. An
// attribute no input reads is fine, as is a component count differing from
// the input's.
func checkVertexInputs(state VertexState, inputs []VertexInput) error {
	for _, input := range inputs {
		inputType := input.Type
		if inputType == ScalarTypeFloat16 {
			inputType = ScalarTypeFloat32
		}
		fed := false
		for bufferIndex, buffer := range state.Buffers {
			for _, attribute := range buffer.Attributes {
				if attribute.ShaderLocation != input.Location {
					continue
				}
				if vertexFormatShaderType(attribute.Format) != inputType {
					return fmt.Errorf("%w: vertex buffer %d's attribute %s at location %d cannot feed the shader's input %s",
						ErrVertexMismatch, bufferIndex, vertexFormatName(attribute.Format), input.Location, input.name())
				}
				fed = true
			}
		}
		if !fed {
			return fmt.Errorf("%w: no vertex attribute at location %d feeds the shader's input %s",
				ErrVertexMismatch, input.Location, input.name())
		}
	}
	return nil
}

// vertexFormatName answers format's name, as its constant spells it after
// VertexFormat.
func vertexFormatName(format VertexFormat) string {
	names := [...]string{
		"Undefined", "Uint8x2", "Uint8x4", "Sint8x2", "Sint8x4", "Unorm8x2", "Unorm8x4", "Snorm8x2", "Snorm8x4",
		"Uint16x2", "Uint16x4", "Sint16x2", "Sint16x4", "Unorm16x2", "Unorm16x4", "Snorm16x2", "Snorm16x4",
		"Float16x2", "Float16x4", "Float32", "Float32x2", "Float32x3", "Float32x4",
		"Uint32", "Uint32x2", "Uint32x3", "Uint32x4", "Sint32", "Sint32x2", "Sint32x3", "Sint32x4", "Unorm1010102",
	}
	if int(format) < len(names) {
		return names[format]
	}
	return fmt.Sprintf("VertexFormat(%d)", format)
}

// vertexFormatShaderType answers the scalar type of the shader inputs format
// feeds: u32 for an unsigned integer format, i32 for a signed one, and f32
// for the rest, normalized ones included, which an f16 input takes too.
func vertexFormatShaderType(format VertexFormat) ScalarType {
	switch name := vertexFormatName(format); {
	case strings.HasPrefix(name, "Uint"):
		return ScalarTypeUint32
	case strings.HasPrefix(name, "Sint"):
		return ScalarTypeSint32
	}
	return ScalarTypeFloat32
}
