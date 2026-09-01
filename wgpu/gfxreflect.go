package wgpu

import (
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
	return shaderLayoutFrom(mod), nil
}

// shaderLayoutFrom extracts the first uniform buffer's member layout plus every
// texture and sampler binding from a lowered module.
func shaderLayoutFrom(mod *ir.Module) cgfx.ShaderLayout {
	var layout cgfx.ShaderLayout
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
					Group: group, Binding: binding,
				})
				continue
			}
			if gv.Space != ir.SpaceUniform {
				continue
			}
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
				Name: gv.Name, TextureView: view, Group: group, Binding: binding,
			})
		case ir.SamplerType:
			layout.Resources = append(layout.Resources, cgfx.ShaderResource{Name: gv.Name, Sampler: true, Group: group, Binding: binding})
		}
	}
	return layout
}
