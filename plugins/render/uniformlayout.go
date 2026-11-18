package render

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/plugins/gfx"
)

// uniformLayoutKey is what a Go type's check against a uniform binding is
// kept by: the type, and the shader and name of the binding.
type uniformLayoutKey struct {
	goType reflect.Type
	shader Shader
	name   string
}

// checkUniformLayout answers why a value of goType cannot fill binding, a
// uniform block: a size other than the block's, or, for a struct, fields
// other than the block's members, at other offsets, in order, names ignored
// and fields named _ left out as padding. A type that is no struct fills a
// block of no members, such as a vec4<f32>, of its size.
func checkUniformLayout(goType reflect.Type, binding gfx.ShaderBinding) error {
	members := binding.Type.Members
	if size := uint64(goType.Size()); size != binding.Buffer.MinBindingSize {
		return fmt.Errorf("is a %v of %d bytes, where its uniform block holds %d", goType, size, binding.Buffer.MinBindingSize)
	}
	if goType.Kind() != reflect.Struct {
		if len(members) > 0 {
			return fmt.Errorf("is a %v, no struct, where its uniform block is a struct of %d members", goType, len(members))
		}
		return nil
	}
	memberIndex := 0
	for field := range goType.Fields() {
		if field.Name == "_" {
			continue
		}
		if memberIndex >= len(members) {
			return fmt.Errorf("is a %v whose field %s is past its uniform block's %d members", goType, field.Name, len(members))
		}
		if member := members[memberIndex]; uint64(field.Offset) != member.Offset {
			return fmt.Errorf("is a %v whose field %s is at offset %d, where its uniform block's member %s is at %d", goType, field.Name, field.Offset, member.Name, member.Offset)
		}
		memberIndex++
	}
	if memberIndex < len(members) {
		return fmt.Errorf("is a %v of %d fields, where its uniform block has %d members", goType, memberIndex, len(members))
	}
	return nil
}
