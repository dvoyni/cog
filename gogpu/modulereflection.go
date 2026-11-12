package gogpu

import (
	"fmt"
	"slices"

	"github.com/dvoyni/cog/gfx"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/valid"
	"github.com/gogpu/naga/wgsl"
)

// moduleReflection is what naga reflects of a shader module once, at its
// creation: each entry point, the bindings it uses and, a vertex entry
// point's, its inputs.
type moduleReflection []entryPointReflection

// reflectModule parses and validates source with naga, every capability
// allowed, as wgpu has already checked the device's, and answers its
// reflection.
func reflectModule(source string) (moduleReflection, error) {
	module, _, err := wgsl.Parse(source, wgsl.ParseOptions{})
	if err != nil {
		return nil, err
	}
	info, err := valid.Validate(module, valid.Options{Capabilities: valid.CapAll})
	if err != nil {
		return nil, err
	}
	var layouter ir.Layouter
	if err := layouter.Update(module); err != nil {
		return nil, err
	}
	reflection := make(moduleReflection, len(module.EntryPoints))
	for index := range module.EntryPoints {
		entryPoint := &module.EntryPoints[index]
		sampled := make([]bool, len(module.GlobalVariables))
		markSampled(module, &entryPoint.Function, nil, sampled)
		reflected := entryPointReflection{name: entryPoint.Name, stage: entryPoint.Stage}
		if entryPoint.Stage == ir.StageVertex {
			reflected.vertexInputs = reflectVertexInputs(module, &entryPoint.Function)
		}
		for global, use := range info.EntryPoints[index].GlobalUses {
			variable := &module.GlobalVariables[global]
			if use != 0 && variable.Binding != nil {
				entry := layoutEntry(module, &layouter, variable, sampled[global])
				entry.Visibility = stageVisibilities[entryPoint.Stage]
				reflected.bindings = append(reflected.bindings, reflectedBinding{group: variable.Binding.Group, entry: entry})
			}
		}
		reflection[index] = reflected
	}
	return reflection, nil
}

// entryPoint answers the reflection of the entry point of stage named name,
// or of the module's one entry point of stage when name is empty.
func (r moduleReflection) entryPoint(stage ir.ShaderStage, name string) (*entryPointReflection, error) {
	var found *entryPointReflection
	for index := range r {
		entryPoint := &r[index]
		if entryPoint.stage != stage || (name != "" && entryPoint.name != name) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("gogpu: the module has more than one %v entry point, and none is named", stage)
		}
		found = entryPoint
	}
	if found == nil {
		return nil, fmt.Errorf("gogpu: the module has no %v entry point %q", stage, name)
	}
	return found, nil
}

// entryPointReflection is an entry point's name and stage, the bindings it
// uses statically, its callees' included, and, a vertex entry point's, the
// inputs it reads at a location.
type entryPointReflection struct {
	name         string
	stage        ir.ShaderStage
	bindings     []reflectedBinding
	vertexInputs []gfx.VertexInput
}

// reflectedBinding is a binding's group, and its entry as WebGPU's "auto"
// layout holds it.
type reflectedBinding struct {
	group uint32
	entry gfx.BindGroupLayoutEntry
}

// stageVisibilities is the visibility of each stage gfx draws or computes
// with.
var stageVisibilities = map[ir.ShaderStage]gfx.ShaderStageFlags{
	ir.StageVertex:   gfx.ShaderStageFlagVertex,
	ir.StageFragment: gfx.ShaderStageFlagFragment,
	ir.StageCompute:  gfx.ShaderStageFlagCompute,
}

// reflectedGroupLayouts answers the bind group layouts WebGPU's "auto" builds
// of entryPoints' bindings: one for each group from 0 to the last they use,
// a group they skip empty. A binding two of them use is visible to both, at
// the larger minimum size, and filterable when either samples it.
func reflectedGroupLayouts(entryPoints ...*entryPointReflection) []gfx.BindGroupLayoutDescriptor {
	var groups []gfx.BindGroupLayoutDescriptor
	for _, entryPoint := range entryPoints {
		for _, binding := range entryPoint.bindings {
			for uint32(len(groups)) <= binding.group {
				groups = append(groups, gfx.BindGroupLayoutDescriptor{})
			}
			group := &groups[binding.group]
			index := slices.IndexFunc(group.Entries, func(entry gfx.BindGroupLayoutEntry) bool { return entry.Binding == binding.entry.Binding })
			if index < 0 {
				group.Entries = append(group.Entries, binding.entry)
				continue
			}
			merged := &group.Entries[index]
			merged.Visibility |= binding.entry.Visibility
			merged.Buffer.MinBindingSize = max(merged.Buffer.MinBindingSize, binding.entry.Buffer.MinBindingSize)
			if binding.entry.Texture.SampleType == gfx.TextureSampleTypeFloat && merged.Texture.SampleType == gfx.TextureSampleTypeUnfilterableFloat {
				merged.Texture.SampleType = gfx.TextureSampleTypeFloat
			}
		}
	}
	return groups
}

// layoutEntry answers the entry WebGPU's "auto" layout holds for global, of
// no visibility: a buffer at its type's size, a filtering sampler unless it
// compares, and an f32 texture filterable only when sampled, as the entry
// point samples it with a sampler.
func layoutEntry(module *ir.Module, layouter *ir.Layouter, global *ir.GlobalVariable, sampled bool) gfx.BindGroupLayoutEntry {
	entry := gfx.BindGroupLayoutEntry{Binding: global.Binding.Binding}
	switch inner := module.Types[global.Type].Inner.(type) {
	case ir.SamplerType:
		entry.Sampler.Type = gfx.SamplerBindingTypeFiltering
		if inner.Comparison {
			entry.Sampler.Type = gfx.SamplerBindingTypeComparison
		}
	case ir.ImageType:
		if inner.Class == ir.ImageClassStorage {
			entry.StorageTexture = gfx.StorageTextureBindingLayout{
				Access:        storageTextureAccesses[inner.StorageAccess&(ir.StorageAccessLoad|ir.StorageAccessStore)],
				Format:        storageTextureFormats[inner.StorageFormat],
				ViewDimension: viewDimension(inner),
			}
			break
		}
		entry.Texture = gfx.TextureBindingLayout{SampleType: sampleTypes[inner.SampledKind], ViewDimension: viewDimension(inner), Multisampled: inner.Multisampled}
		switch {
		case inner.Class == ir.ImageClassDepth:
			entry.Texture.SampleType = gfx.TextureSampleTypeDepth
		case sampled && entry.Texture.SampleType == gfx.TextureSampleTypeUnfilterableFloat:
			entry.Texture.SampleType = gfx.TextureSampleTypeFloat
		}
	default:
		entry.Buffer = gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: uint64(layouter.Layout(global.Type).Size)}
		if global.Space == ir.SpaceStorage {
			entry.Buffer.Type = gfx.BufferBindingTypeReadOnlyStorage
			if global.Access.Contains(ir.StorageAccessStore) {
				entry.Buffer.Type = gfx.BufferBindingTypeStorage
			}
		}
	}
	return entry
}

// reflectVertexInputs answers the inputs function, a vertex entry point,
// reads at a location: its arguments' and its structure arguments' members',
// builtins aside, each of its scalar type and component count.
func reflectVertexInputs(module *ir.Module, function *ir.Function) []gfx.VertexInput {
	var inputs []gfx.VertexInput
	for _, argument := range function.Arguments {
		members := []ir.StructMember{{Type: argument.Type, Binding: argument.Binding}}
		if structure, ok := module.Types[argument.Type].Inner.(ir.StructType); ok {
			members = structure.Members
		}
		for _, member := range members {
			if member.Binding == nil {
				continue
			}
			location, ok := (*member.Binding).(ir.LocationBinding)
			if !ok {
				continue
			}
			input := gfx.VertexInput{Location: location.Location, Components: 1}
			var scalar ir.ScalarType
			switch inner := module.Types[member.Type].Inner.(type) {
			case ir.ScalarType:
				scalar = inner
			case ir.VectorType:
				scalar = inner.Scalar
				input.Components = uint32(inner.Size)
			}
			input.Type = vertexScalarTypes[scalar]
			inputs = append(inputs, input)
		}
	}
	return inputs
}

// viewDimension answers the view dimension image is bound as.
func viewDimension(image ir.ImageType) gfx.TextureViewDimension {
	switch {
	case image.Dim == ir.Dim1D:
		return gfx.TextureViewDimension1D
	case image.Dim == ir.Dim3D:
		return gfx.TextureViewDimension3D
	case image.Dim == ir.DimCube && image.Arrayed:
		return gfx.TextureViewDimensionCubeArray
	case image.Dim == ir.DimCube:
		return gfx.TextureViewDimensionCube
	case image.Arrayed:
		return gfx.TextureViewDimension2DArray
	}
	return gfx.TextureViewDimension2D
}

// markSampled marks in sampled the globals function samples a texture of
// with a sampler, its callees' included; arguments holds the global each of
// function's arguments names, -1 for none.
func markSampled(module *ir.Module, function *ir.Function, arguments []int, sampled []bool) {
	global := func(expression ir.ExpressionHandle) int {
		switch kind := function.Expressions[expression].Kind.(type) {
		case ir.ExprGlobalVariable:
			return int(kind.Variable)
		case ir.ExprFunctionArgument:
			if int(kind.Index) < len(arguments) {
				return arguments[kind.Index]
			}
		}
		return -1
	}
	for _, expression := range function.Expressions {
		if sample, ok := expression.Kind.(ir.ExprImageSample); ok {
			if image := global(sample.Image); image >= 0 {
				sampled[image] = true
			}
		}
	}
	forEachCall(function.Body, func(call ir.StmtCall) {
		callArguments := make([]int, len(call.Arguments))
		for index, argument := range call.Arguments {
			callArguments[index] = global(argument)
		}
		markSampled(module, &module.Functions[call.Function], callArguments, sampled)
	})
}

// forEachCall calls visit with each call of block, its nested blocks'
// included.
func forEachCall(block ir.Block, visit func(call ir.StmtCall)) {
	for _, statement := range block {
		switch kind := statement.Kind.(type) {
		case ir.StmtCall:
			visit(kind)
		case ir.StmtBlock:
			forEachCall(kind.Block, visit)
		case ir.StmtIf:
			forEachCall(kind.Accept, visit)
			forEachCall(kind.Reject, visit)
		case ir.StmtSwitch:
			for _, switchCase := range kind.Cases {
				forEachCall(switchCase.Body, visit)
			}
		case ir.StmtLoop:
			forEachCall(kind.Body, visit)
			forEachCall(kind.Continuing, visit)
		}
	}
}

// sampleTypes is the sample type of a texture of each scalar kind, before
// sampling makes an f32 one filterable.
var sampleTypes = map[ir.ScalarKind]gfx.TextureSampleType{
	ir.ScalarFloat: gfx.TextureSampleTypeUnfilterableFloat,
	ir.ScalarSint:  gfx.TextureSampleTypeSint,
	ir.ScalarUint:  gfx.TextureSampleTypeUint,
}

// vertexScalarTypes is gfx's scalar type of a vertex input of each of naga's
// scalars.
var vertexScalarTypes = map[ir.ScalarType]gfx.ScalarType{
	{Kind: ir.ScalarFloat, Width: 4}: gfx.ScalarTypeFloat32,
	{Kind: ir.ScalarFloat, Width: 2}: gfx.ScalarTypeFloat16,
	{Kind: ir.ScalarSint, Width: 4}:  gfx.ScalarTypeSint32,
	{Kind: ir.ScalarUint, Width: 4}:  gfx.ScalarTypeUint32,
}

// storageTextureAccesses is gfx's access of each of naga's, atomics aside.
var storageTextureAccesses = [...]gfx.StorageTextureAccess{
	ir.StorageAccessLoad:                         gfx.StorageTextureAccessReadOnly,
	ir.StorageAccessStore:                        gfx.StorageTextureAccessWriteOnly,
	ir.StorageAccessLoad | ir.StorageAccessStore: gfx.StorageTextureAccessReadWrite,
}

// storageTextureFormats is gfx's format of each of naga's storage formats;
// the 64-bit ones gfx lacks are undefined.
var storageTextureFormats = [...]gfx.TextureFormat{
	ir.StorageFormatR8Unorm:       gfx.TextureFormatR8Unorm,
	ir.StorageFormatR8Snorm:       gfx.TextureFormatR8Snorm,
	ir.StorageFormatR8Uint:        gfx.TextureFormatR8Uint,
	ir.StorageFormatR8Sint:        gfx.TextureFormatR8Sint,
	ir.StorageFormatR16Uint:       gfx.TextureFormatR16Uint,
	ir.StorageFormatR16Sint:       gfx.TextureFormatR16Sint,
	ir.StorageFormatR16Float:      gfx.TextureFormatR16Float,
	ir.StorageFormatRg8Unorm:      gfx.TextureFormatRG8Unorm,
	ir.StorageFormatRg8Snorm:      gfx.TextureFormatRG8Snorm,
	ir.StorageFormatRg8Uint:       gfx.TextureFormatRG8Uint,
	ir.StorageFormatRg8Sint:       gfx.TextureFormatRG8Sint,
	ir.StorageFormatR32Uint:       gfx.TextureFormatR32Uint,
	ir.StorageFormatR32Sint:       gfx.TextureFormatR32Sint,
	ir.StorageFormatR32Float:      gfx.TextureFormatR32Float,
	ir.StorageFormatRg16Uint:      gfx.TextureFormatRG16Uint,
	ir.StorageFormatRg16Sint:      gfx.TextureFormatRG16Sint,
	ir.StorageFormatRg16Float:     gfx.TextureFormatRG16Float,
	ir.StorageFormatRgba8Unorm:    gfx.TextureFormatRGBA8Unorm,
	ir.StorageFormatRgba8Snorm:    gfx.TextureFormatRGBA8Snorm,
	ir.StorageFormatRgba8Uint:     gfx.TextureFormatRGBA8Uint,
	ir.StorageFormatRgba8Sint:     gfx.TextureFormatRGBA8Sint,
	ir.StorageFormatBgra8Unorm:    gfx.TextureFormatBGRA8Unorm,
	ir.StorageFormatRgb10a2Uint:   gfx.TextureFormatRGB10A2Uint,
	ir.StorageFormatRgb10a2Unorm:  gfx.TextureFormatRGB10A2Unorm,
	ir.StorageFormatRg11b10Ufloat: gfx.TextureFormatRG11B10Ufloat,
	ir.StorageFormatRg32Uint:      gfx.TextureFormatRG32Uint,
	ir.StorageFormatRg32Sint:      gfx.TextureFormatRG32Sint,
	ir.StorageFormatRg32Float:     gfx.TextureFormatRG32Float,
	ir.StorageFormatRgba16Uint:    gfx.TextureFormatRGBA16Uint,
	ir.StorageFormatRgba16Sint:    gfx.TextureFormatRGBA16Sint,
	ir.StorageFormatRgba16Float:   gfx.TextureFormatRGBA16Float,
	ir.StorageFormatRgba32Uint:    gfx.TextureFormatRGBA32Uint,
	ir.StorageFormatRgba32Sint:    gfx.TextureFormatRGBA32Sint,
	ir.StorageFormatRgba32Float:   gfx.TextureFormatRGBA32Float,
	ir.StorageFormatR16Unorm:      gfx.TextureFormatR16Unorm,
	ir.StorageFormatR16Snorm:      gfx.TextureFormatR16Snorm,
	ir.StorageFormatRg16Unorm:     gfx.TextureFormatRG16Unorm,
	ir.StorageFormatRg16Snorm:     gfx.TextureFormatRG16Snorm,
	ir.StorageFormatRgba16Unorm:   gfx.TextureFormatRGBA16Unorm,
	ir.StorageFormatRgba16Snorm:   gfx.TextureFormatRGBA16Snorm,
	ir.StorageFormatR64Uint:       gfx.TextureFormatUndefined,
	ir.StorageFormatR64Sint:       gfx.TextureFormatUndefined,
}
