package render

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dvoyni/cog/plugins/gfx"
)

// uniformLayoutKey is what a Go type's check against a uniform binding is
// kept by: the type, and the shader and name of the binding.
type uniformLayoutKey struct {
	goType reflect.Type
	shader Shader
	name   string
}

// checkTypeLayout answers why a value of goType cannot be shaderType, which
// holder names: a size other than shaderType's, or, for a struct, fields
// other than its members, at other offsets, in order, names ignored and
// fields named _ left out as padding. A type that is no struct is one of no
// members, such as a vec4<f32>, of its size. A uniform block's value is
// checked by it, as Inventory.CheckLayout checks a type at a path.
func checkTypeLayout(goType reflect.Type, shaderType gfx.ShaderType, holder string) error {
	members := shaderType.Members
	if size := uint64(goType.Size()); size != shaderType.Size {
		return fmt.Errorf("is a %v of %d bytes, where %s holds %d", goType, size, holder, shaderType.Size)
	}
	if goType.Kind() != reflect.Struct {
		if len(members) > 0 {
			return fmt.Errorf("is a %v, no struct, where %s is a struct of %d members", goType, holder, len(members))
		}
		return nil
	}
	memberIndex := 0
	for field := range goType.Fields() {
		if field.Name == "_" {
			continue
		}
		if memberIndex >= len(members) {
			return fmt.Errorf("is a %v whose field %s is past %s's %d members", goType, field.Name, holder, len(members))
		}
		if member := members[memberIndex]; uint64(field.Offset) != member.Offset {
			return fmt.Errorf("is a %v whose field %s is at offset %d, where %s's member %s is at %d", goType, field.Name, field.Offset, holder, member.Name, member.Offset)
		}
		memberIndex++
	}
	if memberIndex < len(members) {
		return fmt.Errorf("is a %v of %d fields, where %s has %d members", goType, memberIndex, holder, len(members))
	}
	return nil
}

// findPathType answers the type at path within shaderType: its dotted
// members in turn, each [] after one, or at the path's start, stepping into
// an array's element, so "[].argument" is the member argument of an
// element, and an empty path is shaderType itself. A step naming no member,
// or stepping into a type that is no array, answers an error naming it.
func findPathType(shaderType gfx.ShaderType, path string) (gfx.ShaderType, error) {
	if path == "" {
		return shaderType, nil
	}
	// walkedLength is how many bytes of path the walk has taken, the dot
	// after a step included, which an error names.
	walkedLength := 0
	describeWalked := func() string {
		if walkedLength == 0 {
			return "the binding's type"
		}
		return fmt.Sprintf("the type at %q", strings.TrimSuffix(path[:walkedLength], "."))
	}
	for name := range strings.SplitSeq(path, ".") {
		elementCount := 0
		for strings.HasSuffix(name, "[]") {
			name = strings.TrimSuffix(name, "[]")
			elementCount++
		}
		if name != "" {
			index := slices.IndexFunc(shaderType.Members, func(member gfx.ShaderStructMember) bool { return member.Name == name })
			if index < 0 {
				return gfx.ShaderType{}, fmt.Errorf("names %s, which is no member of %s", name, describeWalked())
			}
			shaderType = shaderType.Members[index].Type
			walkedLength += len(name)
		} else if walkedLength > 0 || elementCount == 0 {
			return gfx.ShaderType{}, fmt.Errorf("has a step naming no member of %s", describeWalked())
		}
		for range elementCount {
			if shaderType.Element == nil {
				return gfx.ShaderType{}, fmt.Errorf("steps into an element of %s, which is no array", describeWalked())
			}
			shaderType = *shaderType.Element
			walkedLength += len("[]")
		}
		walkedLength += len(".")
	}
	return shaderType, nil
}
