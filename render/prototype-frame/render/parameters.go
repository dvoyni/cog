package render

import "fmt"

// The calls in this file are variant B of the prototype: parameters as
// values built by constructors and passed as lists, as v1's
// gfx.ShaderParameter* were, instead of a setter per kind. The naming rules
// make each constructor a make*.

// Parameter is one value bound by name: a texture, a sampler, a buffer or a
// uniform block.
type Parameter struct {
	name, value string
}

// MakeTextureParameter makes a parameter binding texture to name.
func MakeTextureParameter(name string, texture Texture) Parameter {
	return Parameter{name, fmt.Sprintf("texture #%d", texture.index)}
}

// MakeSamplerParameter makes a parameter binding a sampler to name.
func MakeSamplerParameter(name string, sampler SamplerDescriptor) Parameter {
	return Parameter{name, fmt.Sprintf("sampler %+v", sampler)}
}

// MakeBufferParameter makes a parameter binding a storage buffer to name.
func MakeBufferParameter(name string, buffer BufferRange) Parameter {
	return Parameter{name, fmt.Sprintf("%d bytes at %d", buffer.Size, buffer.Offset)}
}

// MakeUniformParameter makes a parameter binding a uniform block to name.
func MakeUniformParameter[T any](name string, value T) Parameter {
	return Parameter{name, fmt.Sprintf("%+v", value)}
}

// SetMaterialParameters binds parameters to the material.
func (a *Assets) SetMaterialParameters(material Material, parameters ...Parameter) {
	for _, parameter := range parameters {
		a.setValue(material.index, parameter.name, parameter.value)
	}
}

// SetPassParameters binds pass parameters.
func (p *Pass) SetPassParameters(parameters ...Parameter) {
	for _, parameter := range parameters {
		p.line("pass parameter %s = %s", parameter.name, parameter.value)
	}
}

// SetParameters binds draw parameters for the draws after it.
func (p *Pass) SetParameters(parameters ...Parameter) {
	for _, parameter := range parameters {
		p.line("    draw parameter %s = %s", parameter.name, parameter.value)
	}
}
