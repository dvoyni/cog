package bindingdeclaration

import "github.com/dvoyni/cog/plugins/gfx"

// declaredType is a buffer's type, or a member's or an element's within it,
// as gfx.ShaderType describes it: a size, and the members of a struct or the
// element, count and stride of an array, a count left out for a
// runtime-sized one.
type declaredType struct {
	Size          uint64           `json:"size"`
	Members       []declaredMember `json:"members,omitempty"`
	Element       *declaredType    `json:"element,omitempty"`
	ElementCount  uint32           `json:"elementCount,omitempty"`
	ElementStride uint64           `json:"elementStride,omitempty"`
}

// makeDeclaredType answers shaderType as a declaration spells it.
func makeDeclaredType(shaderType gfx.ShaderType) declaredType {
	declared := declaredType{Size: shaderType.Size, ElementCount: shaderType.ElementCount, ElementStride: shaderType.ElementStride}
	for _, member := range shaderType.Members {
		declared.Members = append(declared.Members, declaredMember{Name: member.Name, Offset: member.Offset, declaredType: makeDeclaredType(member.Type)})
	}
	if shaderType.Element != nil {
		element := makeDeclaredType(*shaderType.Element)
		declared.Element = &element
	}
	return declared
}

// makeShaderType answers declared as gfx describes it.
func (declared declaredType) makeShaderType() gfx.ShaderType {
	shaderType := gfx.ShaderType{Size: declared.Size, ElementCount: declared.ElementCount, ElementStride: declared.ElementStride}
	for _, member := range declared.Members {
		shaderType.Members = append(shaderType.Members, gfx.ShaderStructMember{Name: member.Name, Offset: member.Offset, Type: member.makeShaderType()})
	}
	if declared.Element != nil {
		element := declared.Element.makeShaderType()
		shaderType.Element = &element
	}
	return shaderType
}

// declaredMember is one member of a struct: its name, its offset, and its
// type.
type declaredMember struct {
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
	declaredType
}
