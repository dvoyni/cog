package rendertest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/dvoyni/cog/gfx"
)

// bufferBindingTypes, samplerBindingTypes, textureSampleTypes,
// textureViewDimensions, storageTextureAccesses and storageTextureFormats
// are the values a declaration names, by WebGPU's spelling; scalarTypes are
// the scalar types of vertex inputs, and vectorComponentCounts the component
// counts of their vectors, by WGSL's.
var (
	bufferBindingTypes = map[string]gfx.BufferBindingType{
		"uniform":           gfx.BufferBindingTypeUniform,
		"storage":           gfx.BufferBindingTypeStorage,
		"read-only-storage": gfx.BufferBindingTypeReadOnlyStorage,
	}
	samplerBindingTypes = map[string]gfx.SamplerBindingType{
		"filtering":     gfx.SamplerBindingTypeFiltering,
		"non-filtering": gfx.SamplerBindingTypeNonFiltering,
		"comparison":    gfx.SamplerBindingTypeComparison,
	}
	textureSampleTypes = map[string]gfx.TextureSampleType{
		"float":              gfx.TextureSampleTypeFloat,
		"unfilterable-float": gfx.TextureSampleTypeUnfilterableFloat,
		"depth":              gfx.TextureSampleTypeDepth,
		"sint":               gfx.TextureSampleTypeSint,
		"uint":               gfx.TextureSampleTypeUint,
	}
	textureViewDimensions = map[string]gfx.TextureViewDimension{
		"1d":         gfx.TextureViewDimension1D,
		"2d":         gfx.TextureViewDimension2D,
		"2d-array":   gfx.TextureViewDimension2DArray,
		"cube":       gfx.TextureViewDimensionCube,
		"cube-array": gfx.TextureViewDimensionCubeArray,
		"3d":         gfx.TextureViewDimension3D,
	}
	storageTextureAccesses = map[string]gfx.StorageTextureAccess{
		"write-only": gfx.StorageTextureAccessWriteOnly,
		"read-only":  gfx.StorageTextureAccessReadOnly,
		"read-write": gfx.StorageTextureAccessReadWrite,
	}
	storageTextureFormats = map[string]gfx.TextureFormat{
		"r32float":    gfx.TextureFormatR32Float,
		"r32uint":     gfx.TextureFormatR32Uint,
		"r32sint":     gfx.TextureFormatR32Sint,
		"rg32float":   gfx.TextureFormatRG32Float,
		"rg32uint":    gfx.TextureFormatRG32Uint,
		"rg32sint":    gfx.TextureFormatRG32Sint,
		"rgba8unorm":  gfx.TextureFormatRGBA8Unorm,
		"rgba8snorm":  gfx.TextureFormatRGBA8Snorm,
		"rgba8uint":   gfx.TextureFormatRGBA8Uint,
		"rgba8sint":   gfx.TextureFormatRGBA8Sint,
		"bgra8unorm":  gfx.TextureFormatBGRA8Unorm,
		"rgba16uint":  gfx.TextureFormatRGBA16Uint,
		"rgba16sint":  gfx.TextureFormatRGBA16Sint,
		"rgba16float": gfx.TextureFormatRGBA16Float,
		"rgba32float": gfx.TextureFormatRGBA32Float,
		"rgba32uint":  gfx.TextureFormatRGBA32Uint,
		"rgba32sint":  gfx.TextureFormatRGBA32Sint,
	}
	scalarTypes = map[string]gfx.ScalarType{
		"f32": gfx.ScalarTypeFloat32,
		"f16": gfx.ScalarTypeFloat16,
		"i32": gfx.ScalarTypeSint32,
		"u32": gfx.ScalarTypeUint32,
	}
	vectorComponentCounts = map[string]uint32{
		"vec2": 2,
		"vec3": 3,
		"vec4": 4,
	}
)

// bindingDeclaration is the JSON of a binding declaration, the file beside a
// test shader, <shader>.wgsl.json, that the harness answers the shader's
// reflection from: what the shader declares, and a variant for each define
// set whose bindings differ. The package's documentation describes the
// format.
type bindingDeclaration struct {
	declaredShader
	Variants []declaredVariant `json:"variants"`
}

// declaredVariant is the declaration of the variant compiled with Defines,
// each define's name with its value, empty for none, in place of the base
// declaration's, whole.
type declaredVariant struct {
	Defines map[string]string `json:"defines"`
	declaredShader
}

// declaredShader is what a declaration says one variant of a shader
// declares: its bindings, and the inputs of each vertex entry point, by name.
type declaredShader struct {
	Bindings     []declaredBinding                `json:"bindings"`
	VertexInputs map[string][]declaredVertexInput `json:"vertexInputs"`
}

// makeShaderDeclaration answers the gfx values of s's bindings and vertex
// inputs, or the error naming the first value no gfx value spells.
func (s declaredShader) makeShaderDeclaration() (shaderDeclaration, error) {
	declaration := shaderDeclaration{vertexInputs: map[string][]gfx.VertexInput{}}
	for _, binding := range s.Bindings {
		shaderBinding, err := binding.makeShaderBinding()
		if err != nil {
			return shaderDeclaration{}, fmt.Errorf("binding %q: %w", binding.Name, err)
		}
		declaration.bindings = append(declaration.bindings, shaderBinding)
	}
	for entryPoint, declaredInputs := range s.VertexInputs {
		inputs := make([]gfx.VertexInput, 0, len(declaredInputs))
		for _, input := range declaredInputs {
			vertexInput, err := input.makeVertexInput()
			if err != nil {
				return shaderDeclaration{}, fmt.Errorf("vertex entry point %q: %w", entryPoint, err)
			}
			inputs = append(inputs, vertexInput)
		}
		declaration.vertexInputs[entryPoint] = inputs
	}
	return declaration, nil
}

// declaredBinding is one binding a shader declares, as gfx.ShaderBinding
// holds it: exactly one of Buffer, Sampler, Texture and StorageTexture is
// set.
type declaredBinding struct {
	Group          uint32                  `json:"group"`
	Binding        uint32                  `json:"binding"`
	Name           string                  `json:"name"`
	Buffer         *declaredBuffer         `json:"buffer"`
	Sampler        *declaredSampler        `json:"sampler"`
	Texture        *declaredTexture        `json:"texture"`
	StorageTexture *declaredStorageTexture `json:"storageTexture"`
	EntryPoints    []string                `json:"entryPoints"`
}

// makeShaderBinding answers b as gfx reflects it, or the error naming what
// spells no gfx value: a binding of no kind or of several among them.
func (b declaredBinding) makeShaderBinding() (gfx.ShaderBinding, error) {
	kindCount := 0
	for _, isSet := range []bool{b.Buffer != nil, b.Sampler != nil, b.Texture != nil, b.StorageTexture != nil} {
		if isSet {
			kindCount++
		}
	}
	if kindCount != 1 {
		return gfx.ShaderBinding{}, fmt.Errorf("declares %d of buffer, sampler, texture and storageTexture, want one", kindCount)
	}
	binding := gfx.ShaderBinding{Name: b.Name, Group: b.Group, Binding: b.Binding, EntryPoints: b.EntryPoints}
	var err error
	switch {
	case b.Buffer != nil:
		binding.Buffer.MinBindingSize = b.Buffer.Size
		for _, member := range b.Buffer.Members {
			binding.Members = append(binding.Members, gfx.ShaderStructMember(member))
		}
		binding.Buffer.Type, err = findNamed(bufferBindingTypes, "buffer type", b.Buffer.Type)
	case b.Sampler != nil:
		binding.Sampler.Type, err = findNamed(samplerBindingTypes, "sampler type", b.Sampler.Type)
	case b.Texture != nil:
		binding.Texture.Multisampled = b.Texture.Multisampled
		if binding.Texture.SampleType, err = findNamed(textureSampleTypes, "sample type", b.Texture.SampleType); err == nil {
			binding.Texture.ViewDimension, err = findNamed(textureViewDimensions, "view dimension", b.Texture.ViewDimension)
		}
	default:
		if binding.StorageTexture.Access, err = findNamed(storageTextureAccesses, "storage access", b.StorageTexture.Access); err != nil {
			break
		}
		if binding.StorageTexture.Format, err = findNamed(storageTextureFormats, "storage format", b.StorageTexture.Format); err != nil {
			break
		}
		binding.StorageTexture.ViewDimension, err = findNamed(textureViewDimensions, "view dimension", b.StorageTexture.ViewDimension)
	}
	return binding, err
}

// declaredBuffer is a buffer binding: its type, uniform, storage or
// read-only-storage, its type's size as WGSL lays it out, and its struct's
// members.
type declaredBuffer struct {
	Type    string           `json:"type"`
	Size    uint64           `json:"size"`
	Members []declaredMember `json:"members"`
}

// declaredMember is one member of a buffer's struct.
type declaredMember struct {
	Name   string `json:"name"`
	Offset uint64 `json:"offset"`
	Size   uint64 `json:"size"`
}

// declaredSampler is a sampler binding: its type, filtering or comparison.
type declaredSampler struct {
	Type string `json:"type"`
}

// declaredTexture is a sampled-texture binding.
type declaredTexture struct {
	SampleType    string `json:"sampleType"`
	ViewDimension string `json:"viewDimension"`
	Multisampled  bool   `json:"multisampled"`
}

// declaredStorageTexture is a storage-texture binding.
type declaredStorageTexture struct {
	Access        string `json:"access"`
	Format        string `json:"format"`
	ViewDimension string `json:"viewDimension"`
}

// declaredVertexInput is one input a vertex entry point reads: its location
// and its type as WGSL spells it, f32 or vec3<f32>.
type declaredVertexInput struct {
	Location uint32 `json:"location"`
	Type     string `json:"type"`
}

// makeVertexInput answers i as gfx reflects it: its type a scalar, f32, or
// a vector of two to four, vec3<f32>.
func (i declaredVertexInput) makeVertexInput() (gfx.VertexInput, error) {
	input := gfx.VertexInput{Location: i.Location, Components: 1}
	scalar := i.Type
	if vectorName, afterBracket, isVector := strings.Cut(i.Type, "<"); isVector {
		components, isCounted := vectorComponentCounts[vectorName]
		componentType, isClosed := strings.CutSuffix(afterBracket, ">")
		if !isCounted || !isClosed {
			return gfx.VertexInput{}, fmt.Errorf("location %d: %q is no vector type", i.Location, i.Type)
		}
		input.Components = components
		scalar = componentType
	}
	scalarType, err := findNamed(scalarTypes, "scalar type", scalar)
	if err != nil {
		return gfx.VertexInput{}, fmt.Errorf("location %d: %w", i.Location, err)
	}
	input.Type = scalarType
	return input, nil
}

// shaderDeclaration is what a declaration says of one variant of a shader,
// as gfx reflects it: its bindings, and the inputs of each vertex entry
// point, by name.
type shaderDeclaration struct {
	bindings     []gfx.ShaderBinding
	vertexInputs map[string][]gfx.VertexInput
}

// readShaderDeclaration answers what the declaration beside the shader at
// path in files says of its variant with defines: the variant whose defines
// are the same names with the same values, or the base declaration when
// none is. A missing declaration answers fs.ErrNotExist; one that fails to
// parse, its error.
func readShaderDeclaration(files fs.FS, path string, defines []gfx.ShaderDefine) (shaderDeclaration, error) {
	data, err := fs.ReadFile(files, path+".json")
	if err != nil {
		return shaderDeclaration{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var declaration bindingDeclaration
	if err := decoder.Decode(&declaration); err != nil {
		return shaderDeclaration{}, err
	}
	definedValues := map[string]string{}
	for _, define := range defines {
		definedValues[define.Name] = define.Value
	}
	chosenShader := declaration.declaredShader
	for _, variant := range declaration.Variants {
		if maps.Equal(variant.Defines, definedValues) {
			chosenShader = variant.declaredShader
			break
		}
	}
	return chosenShader.makeShaderDeclaration()
}

// findNamed answers the value of values named name, or an error naming it as
// a what and the names there are.
func findNamed[T any](values map[string]T, what, name string) (T, error) {
	value, ok := values[name]
	if !ok {
		return value, fmt.Errorf("%q is no %s, want one of %q", name, what, slices.Sorted(maps.Keys(values)))
	}
	return value, nil
}
