package wgpu

import (
	"fmt"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// vertexEntryPoint is the vertex function every gfx pipeline is built against.
// Reflection and pipeline creation read the same constant so that what is
// checked cannot drift from what is built.
const vertexEntryPoint = "vs_main"

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
	layout.VertexInputs = vertexInputs(mod)
	return layout, nil
}

// vertexInputs reflects every @location the vertex entry point declares, which
// is the half of the vertex interface only the shader knows. It walks the
// module the global-variable loop above already has: no new parse, no second
// lowering, no new dependency.
//
// The entry point is the one named vs_main, because that is the one
// gfxBackend.NewPipeline names. A module carrying a second vertex function
// nothing is built against has no say in what a draw through this shader reads.
func vertexInputs(mod *ir.Module) []cgfx.ShaderVertexInput {
	var inputs []cgfx.ShaderVertexInput
	for i := range mod.EntryPoints {
		entry := &mod.EntryPoints[i]
		if entry.Stage != ir.StageVertex || entry.Name != vertexEntryPoint {
			continue
		}
		for _, arg := range entry.Function.Arguments {
			// An argument is either one @location itself or a struct whose
			// members carry the bindings - SceneVertexIn is the second shape and
			// canvas's vs_main the first. A @builtin argument carries a binding
			// that is not a location and is skipped by the type switch below.
			if arg.Binding != nil {
				if input, ok := vertexInput(mod, arg.Name, arg.Type, *arg.Binding); ok {
					inputs = append(inputs, input)
				}
				continue
			}
			structure, ok := mod.Types[arg.Type].Inner.(ir.StructType)
			if !ok {
				continue
			}
			for _, member := range structure.Members {
				if member.Binding == nil {
					continue
				}
				if input, ok := vertexInput(mod, member.Name, member.Type, *member.Binding); ok {
					inputs = append(inputs, input)
				}
			}
		}
	}
	return inputs
}

// vertexInput reduces one @location declaration to the pair a vertex format is
// compared against: the scalar kind it decodes to and how many components it
// has. Anything else - a @builtin, or a declared type no vertex format can
// present - is not a vertex input and is reported as not one.
func vertexInput(
	mod *ir.Module, name string, typ ir.TypeHandle, binding ir.Binding,
) (cgfx.ShaderVertexInput, bool) {
	location, ok := binding.(ir.LocationBinding)
	if !ok {
		return cgfx.ShaderVertexInput{}, false
	}
	input := cgfx.ShaderVertexInput{Name: name, Location: int(location.Location)}
	switch inner := mod.Types[typ].Inner.(type) {
	case ir.ScalarType:
		input.Kind, input.Count = vertexScalar(inner.Kind), 1
	case ir.VectorType:
		input.Kind, input.Count = vertexScalar(inner.Scalar.Kind), int(inner.Size)
	default:
		return cgfx.ShaderVertexInput{}, false
	}
	if input.Kind == cgfx.VertexScalarNone {
		return cgfx.ShaderVertexInput{}, false
	}
	return input, true
}

// vertexScalar maps a WGSL scalar kind onto what a vertex format decodes to.
// Half-width floats are still floats: WebGPU's float16 formats present as f32,
// so the width is not part of the comparison.
func vertexScalar(kind ir.ScalarKind) cgfx.VertexScalar {
	switch kind {
	case ir.ScalarFloat:
		return cgfx.VertexScalarFloat
	case ir.ScalarUint:
		return cgfx.VertexScalarUint
	case ir.ScalarSint:
		return cgfx.VertexScalarSint
	}
	return cgfx.VertexScalarNone
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
