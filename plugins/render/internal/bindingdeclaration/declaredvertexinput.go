package bindingdeclaration

import (
	"fmt"
	"strings"

	"github.com/dvoyni/cog/plugins/gfx"
)

// scalarTypes are the scalar types of vertex inputs, and
// vectorComponentCounts the component counts of their vectors, by WGSL's
// spelling.
var (
	scalarTypes = map[string]gfx.ScalarType{
		"f32": gfx.ScalarTypeFloat32,
		"f16": gfx.ScalarTypeFloat16,
		"i32": gfx.ScalarTypeSint32,
		"u32": gfx.ScalarTypeUint32,
	}
	vectorComponentCounts = map[string]uint32{
		"vec2": 2,
		"vec3": 3,
		"vec4": 4,
	}
)

// declaredVertexInput is one input a vertex entry point reads: its location
// and its type as WGSL spells it, f32 or vec3<f32>.
type declaredVertexInput struct {
	Location uint32 `json:"location"`
	Type     string `json:"type"`
}

// makeDeclaredVertexInput answers vertexInput as a declaration spells it, a
// scalar, f32, or a vector of two to four, vec3<f32>, or the error naming a
// type no name spells.
func makeDeclaredVertexInput(vertexInput gfx.VertexInput) (declaredVertexInput, error) {
	scalar, err := findName(scalarTypes, "scalar type", vertexInput.Type)
	if err != nil {
		return declaredVertexInput{}, fmt.Errorf("location %d: %w", vertexInput.Location, err)
	}
	input := declaredVertexInput{Location: vertexInput.Location, Type: scalar}
	if vertexInput.Components > 1 {
		vectorName, err := findName(vectorComponentCounts, "vector's component count", vertexInput.Components)
		if err != nil {
			return declaredVertexInput{}, fmt.Errorf("location %d: %w", vertexInput.Location, err)
		}
		input.Type = vectorName + "<" + scalar + ">"
	}
	return input, nil
}

// makeVertexInput answers i as gfx reflects it: its type a scalar, f32, or
// a vector of two to four, vec3<f32>.
func (i declaredVertexInput) makeVertexInput() (gfx.VertexInput, error) {
	input := gfx.VertexInput{Location: i.Location, Components: 1}
	scalar := i.Type
	if vectorName, afterBracket, isVector := strings.Cut(i.Type, "<"); isVector {
		components, isCounted := vectorComponentCounts[vectorName]
		componentType, isClosed := strings.CutSuffix(afterBracket, ">")
		if !isCounted || !isClosed {
			return gfx.VertexInput{}, fmt.Errorf("location %d: %q is no vector type", i.Location, i.Type)
		}
		input.Components = components
		scalar = componentType
	}
	scalarType, err := findNamed(scalarTypes, "scalar type", scalar)
	if err != nil {
		return gfx.VertexInput{}, fmt.Errorf("location %d: %w", i.Location, err)
	}
	input.Type = scalarType
	return input, nil
}
