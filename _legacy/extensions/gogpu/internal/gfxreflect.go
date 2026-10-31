package internal

import (
	"github.com/dvoyni/cog/slots/gfx"
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
func reflectShaderLayout(source string) (gfx.ShaderLayout, error) {
	mod, err := lowerWGSL(source)
	if err != nil {
		return gfx.ShaderLayout{}, err
	}
	return shaderLayoutFrom(mod)
}

// shaderLayoutFrom extracts every uniform and storage binding, whatever type it
// is declared at - a struct's with its member layout - and every texture and
// sampler binding from a lowered module.
//
// A buffer binding is told by its address space rather than its type. A
// uniform may be a bare vec4f or mat4x4f and a storage binding a bare
// array<T>, and each is a binding the pipeline layout has to declare and a
// param has to be able to name; reading only the struct-typed ones would leave
// them out of both.
func shaderLayoutFrom(mod *ir.Module) (gfx.ShaderLayout, error) {
	var layout gfx.ShaderLayout
	for _, gv := range mod.GlobalVariables {
		if gv.Binding == nil {
			continue
		}
		group, binding := int(gv.Binding.Group), int(gv.Binding.Binding)
		switch gv.Space {
		case ir.SpaceStorage:
			kind := gfx.ResourceStorageBuffer
			if gv.Access == ir.StorageReadWrite {
				kind |= gfx.ResourceWritable
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind,
				Group: group, Binding: binding, Members: bufferMembers(mod, gv.Type),
			})
			continue
		case ir.SpaceUniform:
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: gfx.ResourceUniformBuffer, Group: group, Binding: binding,
				Size: int(ir.TypeSize(mod, gv.Type)), Members: bufferMembers(mod, gv.Type),
			})
			continue
		}
		switch inner := mod.Types[gv.Type].Inner.(type) {
		case ir.ImageType:
			view := gfx.TextureView2D
			if inner.Dim == ir.Dim2D && inner.Arrayed {
				view = gfx.TextureView2DArray
			}
			kind := gfx.ResourceTexture
			if inner.Class == ir.ImageClassDepth {
				kind |= gfx.ResourceDepth
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind, TextureView: view,
				Group: group, Binding: binding,
			})
		case ir.SamplerType:
			kind := gfx.ResourceSampler
			if inner.Comparison {
				kind |= gfx.ResourceComparison
			}
			layout.Resources = append(layout.Resources, gfx.ShaderResource{
				Name: gv.Name, Kind: kind,
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
func vertexInputs(mod *ir.Module) []gfx.ShaderVertexInput {
	var inputs []gfx.ShaderVertexInput
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
) (gfx.ShaderVertexInput, bool) {
	location, ok := binding.(ir.LocationBinding)
	if !ok {
		return gfx.ShaderVertexInput{}, false
	}
	input := gfx.ShaderVertexInput{Name: name, Location: int(location.Location)}
	switch inner := mod.Types[typ].Inner.(type) {
	case ir.ScalarType:
		input.Kind, input.Count = vertexScalar(inner.Kind), 1
	case ir.VectorType:
		input.Kind, input.Count = vertexScalar(inner.Scalar.Kind), int(inner.Size)
	default:
		return gfx.ShaderVertexInput{}, false
	}
	if input.Kind == gfx.VertexScalarNone {
		return gfx.ShaderVertexInput{}, false
	}
	return input, true
}

// vertexScalar maps a WGSL scalar kind onto what a vertex format decodes to.
// Half-width floats are still floats: WebGPU's float16 formats present as f32,
// so the width is not part of the comparison.
func vertexScalar(kind ir.ScalarKind) gfx.VertexScalar {
	switch kind {
	case ir.ScalarFloat:
		return gfx.VertexScalarFloat
	case ir.ScalarUint:
		return gfx.VertexScalarUint
	case ir.ScalarSint:
		return gfx.VertexScalarSint
	}
	return gfx.VertexScalarNone
}

// bufferMembers is the member layout of a buffer binding declared at a struct,
// and nothing for one declared at any other type.
func bufferMembers(mod *ir.Module, typ ir.TypeHandle) []gfx.StorageMember {
	if structure, ok := mod.Types[typ].Inner.(ir.StructType); ok {
		return storageMembers(mod, structure)
	}
	return nil
}

// storageMembers walks one level of a storage struct. An array member carries
// its element stride and count, because a reader of `lights: array<Light, 16>`
// needs both where the array starts and how far apart its elements sit.
func storageMembers(mod *ir.Module, structure ir.StructType) []gfx.StorageMember {
	members := make([]gfx.StorageMember, 0, len(structure.Members))
	for _, member := range structure.Members {
		reflected := gfx.StorageMember{Name: member.Name, Offset: int(member.Offset)}
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
