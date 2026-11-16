package render

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render/internal/handle"
)

// passGroup is the bind group of a pass's parameters.
const passGroup = 0

// materialGroup is the bind group of a material's parameters.
const materialGroup = 1

// drawGroup is the bind group of draw parameters.
const drawGroup = 2

// reservedGroup is the first bind group render reserves: a shader declaring a
// binding in it is refused.
const reservedGroup = 3

// shaderKey is what an acquired shader is shared by: the path of its file and
// its defines, sorted by name, each name given once, joined into one string.
type shaderKey struct {
	path    string
	defines string
}

// sortDefines answers defines sorted by name, each name once with its last
// value, as gfx preprocesses them, in sortedDefines' array, grown when they
// outnumber its capacity.
func sortDefines(defines []ShaderDefine, sortedDefines []gfx.ShaderDefine) []gfx.ShaderDefine {
	sortedDefines = sortedDefines[:0]
	for _, define := range defines {
		index := slices.IndexFunc(sortedDefines, func(other gfx.ShaderDefine) bool { return other.Name == define.Name })
		if index < 0 {
			sortedDefines = append(sortedDefines, gfx.ShaderDefine(define))
		} else {
			sortedDefines[index].Value = define.Value
		}
	}
	slices.SortFunc(sortedDefines, func(a, b gfx.ShaderDefine) int { return cmp.Compare(a.Name, b.Name) })
	return sortedDefines
}

// appendJoinedDefines appends to joinedDefines sortedDefines as a shader's
// key joins them: each define is its name, and then = and its value quoted
// when it has one, joined by spaces. It answers the extended slice.
func appendJoinedDefines(joinedDefines []byte, sortedDefines []gfx.ShaderDefine) []byte {
	for index, define := range sortedDefines {
		if index > 0 {
			joinedDefines = append(joinedDefines, ' ')
		}
		joinedDefines = append(joinedDefines, define.Name...)
		if define.Value != "" {
			joinedDefines = append(joinedDefines, '=')
			joinedDefines = strconv.AppendQuote(joinedDefines, define.Value)
		}
	}
	return joinedDefines
}

// compile preprocesses k's file with defines, its own sorted, makes its module,
// labelled with shader, reflects its entry points and bindings, and acquires from layouts
// the layout of the module's bindings, answering them in a slot. A failure
// answers the slot with nothing, what it made released, and its error, with
// the kind it is reported as: ReportKindLoadFailed for a file that fails to preprocess or
// compile, ReportKindShaderRefused for one refused for its bindings, and none
// for an error of gfx's to log. A module made goes into inspector's
// inspection.
func (k shaderKey) compile(device gfx.Device, layouts *layoutInterner, inspector *inspector, files fs.FS, shader Shader, defines []gfx.ShaderDefine) (slot shaderSlot, reportKind ReportKind, err error) {
	source, err := device.PreprocessShader(files, k.path, defines)
	if err != nil {
		return shaderSlot{}, ReportKindLoadFailed, fmt.Errorf("failed to load: %w", err)
	}
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: handle.MakeLabel(shader, k.path), WGSL: source})
	if errors.Is(err, gfx.ErrDeviceLost) {
		return shaderSlot{}, 0, err
	}
	if err != nil {
		return shaderSlot{}, ReportKindLoadFailed, fmt.Errorf("failed to compile: %w", err)
	}
	inspector.add(func(inspection *Inspection) {
		inspection.MadeAssets = append(inspection.MadeAssets, InspectedAsset{Shader: shader, Label: k.path})
	})
	defer func() {
		if err != nil {
			device.ReleaseShaderModule(module)
		}
	}()
	bindings, err := device.ShaderModuleBindings(module)
	if err != nil {
		return shaderSlot{}, 0, err
	}
	entryPoints, err := device.ShaderModuleEntryPoints(module)
	if err != nil {
		return shaderSlot{}, 0, err
	}
	groups, err := makeGroupLayouts(bindings)
	if err != nil {
		return shaderSlot{}, ReportKindShaderRefused, err
	}
	layout, err := layouts.acquire(device, groups)
	if err != nil {
		if errors.Is(err, gfx.ErrLimitExceeded) {
			return shaderSlot{}, ReportKindShaderRefused, fmt.Errorf("refused: %w", err)
		}
		return shaderSlot{}, 0, err
	}
	return makeShaderSlot(module, layout, entryPoints, bindings), 0, nil
}

// makeGroupLayouts answers the layout of each group up to the highest
// bindings declare, one with no bindings empty, each of every binding of its
// group, sorted by number, which every stage sees but the vertex stage, for
// a storage buffer or storage texture a shader may write, which WebGPU keeps
// from it. Uniform blocks of @group(0) and @group(2), and storage buffers of
// @group(2), bind at dynamic offsets. A binding in @group(3) or above, which
// render reserves, is refused.
func makeGroupLayouts(bindings []gfx.ShaderBinding) ([]gfx.BindGroupLayoutDescriptor, error) {
	var groups []gfx.BindGroupLayoutDescriptor
	for _, binding := range bindings {
		if binding.Group >= reservedGroup {
			return nil, fmt.Errorf("refused: %q is in @group(%d), and render reserves @group(%d)", binding.Name, binding.Group, reservedGroup)
		}
		for uint32(len(groups)) <= binding.Group {
			groups = append(groups, gfx.BindGroupLayoutDescriptor{Label: "render: a shader's group"})
		}
		entry := gfx.BindGroupLayoutEntry{
			Binding:        binding.Binding,
			Visibility:     gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment | gfx.ShaderStageFlagCompute,
			Buffer:         binding.Buffer,
			Sampler:        binding.Sampler,
			Texture:        binding.Texture,
			StorageTexture: binding.StorageTexture,
		}
		storageBuffer := entry.Buffer.Type == gfx.BufferBindingTypeStorage || entry.Buffer.Type == gfx.BufferBindingTypeReadOnlyStorage
		if entry.Buffer.Type == gfx.BufferBindingTypeStorage || (entry.StorageTexture.Access != gfx.StorageTextureAccessUndefined && entry.StorageTexture.Access != gfx.StorageTextureAccessReadOnly) {
			entry.Visibility &^= gfx.ShaderStageFlagVertex
		}
		entry.Buffer.HasDynamicOffset = (entry.Buffer.Type == gfx.BufferBindingTypeUniform && binding.Group != materialGroup) || (storageBuffer && binding.Group == drawGroup)
		groups[binding.Group].Entries = append(groups[binding.Group].Entries, entry)
	}
	for _, group := range groups {
		slices.SortFunc(group.Entries, func(a, b gfx.BindGroupLayoutEntry) int { return cmp.Compare(a.Binding, b.Binding) })
	}
	return groups, nil
}
