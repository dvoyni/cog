package wgpu

import (
	"fmt"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// lowerWGSL parses and lowers WGSL to naga's typed IR (pure, no GPU).
func lowerWGSL(source string) (*ir.Module, error) {
	ast, err := naga.Parse(source)
	if err != nil {
		return nil, err
	}
	return wgsl.Lower(ast)
}

// reflectShaderLayout reflects the uniform + resource bindings of WGSL source.
func reflectShaderLayout(source string) (cgfx.ShaderLayout, error) {
	mod, err := lowerWGSL(source)
	if err != nil {
		return cgfx.ShaderLayout{}, err
	}
	return shaderLayoutFrom(mod)
}

// shaderLayoutFrom extracts the uniform block's member layout, every storage
// struct's member layout, and every texture and sampler binding from a lowered
// module.
func shaderLayoutFrom(mod *ir.Module) (cgfx.ShaderLayout, error) {
	var layout cgfx.ShaderLayout
	uniform := ""
	for _, gv := range mod.GlobalVariables {
		if gv.Binding == nil {
			continue
		}
		group, binding := int(gv.Binding.Group), int(gv.Binding.Binding)
		switch inner := mod.Types[gv.Type].Inner.(type) {
		case ir.StructType:
			if gv.Space == ir.SpaceStorage {
				layout.Resources = append(layout.Resources, cgfx.ShaderResource{
					Name: gv.Name, StorageBuffer: true, WritableBuffer: gv.Access == ir.StorageReadWrite,
					Group: group, Binding: binding, Members: storageMembers(mod, inner),
				})
				continue
			}
			if gv.Space != ir.SpaceUniform {
				continue
			}
			// A second uniform block used to overwrite the first, which moves
			// every parameter to the wrong offset with nothing to point at.
			if uniform != "" {
				return cgfx.ShaderLayout{}, fmt.Errorf(
					"wgpu: shader declares two uniform blocks, %q and %q; gfx supports one", uniform, gv.Name)
			}
			uniform = gv.Name
			layout.UniformSize = int(inner.Span)
			layout.UniformGroup = group
			layout.UniformBinding = binding
			for _, mem := range inner.Members {
				layout.Uniforms = append(layout.Uniforms, cgfx.UniformMember{Name: mem.Name, Offset: int(mem.Offset)})
			}
		case ir.ImageType:
			view := cgfx.TextureView2D
			if inner.Dim == ir.Dim2D && inner.Arrayed {
				view = cgfx.TextureView2DArray
			}
			layout.Resources = append(layout.Resources, cgfx.ShaderResource{
				Name: gv.Name, TextureView: view, Depth: inner.Class == ir.ImageClassDepth,
				Group: group, Binding: binding,
			})
		case ir.SamplerType:
			layout.Resources = append(layout.Resources, cgfx.ShaderResource{
				Name: gv.Name, Sampler: true, Comparison: inner.Comparison,
				Group: group, Binding: binding,
			})
		}
	}
	return layout, nil
}

// storageMembers walks one level of a storage struct. An array member carries
// its element stride and count, because a reader of `lights: array<Light, 16>`
// needs both where the array starts and how far apart its elements sit.
func storageMembers(mod *ir.Module, structure ir.StructType) []cgfx.StorageMember {
	members := make([]cgfx.StorageMember, 0, len(structure.Members))
	for _, member := range structure.Members {
		reflected := cgfx.StorageMember{Name: member.Name, Offset: int(member.Offset)}
		if array, ok := mod.Types[member.Type].Inner.(ir.ArrayType); ok {
			reflected.Stride = int(array.Stride)
			if array.Size.Constant != nil {
				reflected.Count = int(*array.Size.Constant)
			}
		}
		members = append(members, reflected)
	}
	return members
}
