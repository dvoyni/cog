package bindingdeclaration

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// declaredShader is what a declaration says one variant of a shader
// declares: its bindings, and the inputs of each vertex entry point, by name.
type declaredShader struct {
	Bindings     []declaredBinding                `json:"bindings"`
	VertexInputs map[string][]declaredVertexInput `json:"vertexInputs"`
}

// makeDeclaredShader answers reflection as a declaration spells it, or the
// error naming the first value it cannot spell.
func makeDeclaredShader(reflection Reflection) (declaredShader, error) {
	shader := declaredShader{
		Bindings:     make([]declaredBinding, 0, len(reflection.Bindings)),
		VertexInputs: make(map[string][]declaredVertexInput, len(reflection.VertexInputs)),
	}
	for _, shaderBinding := range reflection.Bindings {
		binding, err := makeDeclaredBinding(shaderBinding)
		if err != nil {
			return declaredShader{}, fmt.Errorf("binding %q: %w", shaderBinding.Name, err)
		}
		shader.Bindings = append(shader.Bindings, binding)
	}
	for entryPoint, vertexInputs := range reflection.VertexInputs {
		inputs := make([]declaredVertexInput, 0, len(vertexInputs))
		for _, vertexInput := range vertexInputs {
			input, err := makeDeclaredVertexInput(vertexInput)
			if err != nil {
				return declaredShader{}, fmt.Errorf("vertex entry point %q: %w", entryPoint, err)
			}
			inputs = append(inputs, input)
		}
		shader.VertexInputs[entryPoint] = inputs
	}
	return shader, nil
}

// makeReflection answers the gfx values of s's bindings and vertex inputs,
// or the error naming the first value no gfx value spells.
func (s declaredShader) makeReflection() (Reflection, error) {
	reflection := Reflection{VertexInputs: map[string][]gfx.VertexInput{}}
	for _, binding := range s.Bindings {
		shaderBinding, err := binding.makeShaderBinding()
		if err != nil {
			return Reflection{}, fmt.Errorf("binding %q: %w", binding.Name, err)
		}
		reflection.Bindings = append(reflection.Bindings, shaderBinding)
	}
	for entryPoint, declaredInputs := range s.VertexInputs {
		inputs := make([]gfx.VertexInput, 0, len(declaredInputs))
		for _, input := range declaredInputs {
			vertexInput, err := input.makeVertexInput()
			if err != nil {
				return Reflection{}, fmt.Errorf("vertex entry point %q: %w", entryPoint, err)
			}
			inputs = append(inputs, vertexInput)
		}
		reflection.VertexInputs[entryPoint] = inputs
	}
	return reflection, nil
}
