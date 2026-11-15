package render

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// materialGroup is the bind group of a material's parameters.
const materialGroup = 1

// reservedGroup is the first bind group render reserves: a shader declaring a
// binding in it is refused.
const reservedGroup = 3

// shaderKey is what an acquired shader is shared by: the path of its file and
// its defines, sorted by name, each name given once, joined into one string.
type shaderKey struct {
	path    string
	defines string
}

// makeShaderKey answers the key of path's variant with defines, and defines
// sorted by name, each name once with its last value, as gfx preprocesses
// it: each define is its name, and then = and its value quoted when it has
// one, joined by spaces.
func makeShaderKey(path string, defines []ShaderDefine) (shaderKey, []gfx.ShaderDefine) {
	sortedDefines := make([]gfx.ShaderDefine, 0, len(defines))
	for _, define := range defines {
		index := slices.IndexFunc(sortedDefines, func(other gfx.ShaderDefine) bool { return other.Name == define.Name })
		if index < 0 {
			sortedDefines = append(sortedDefines, gfx.ShaderDefine(define))
		} else {
			sortedDefines[index].Value = define.Value
		}
	}
	slices.SortFunc(sortedDefines, func(a, b gfx.ShaderDefine) int { return cmp.Compare(a.Name, b.Name) })
	var joinedDefines strings.Builder
	for index, define := range sortedDefines {
		joinedDefines.Grow(len(define.Name) + len(define.Value) + 4)
		if index > 0 {
			joinedDefines.WriteByte(' ')
		}
		joinedDefines.WriteString(define.Name)
		if define.Value != "" {
			joinedDefines.WriteByte('=')
			joinedDefines.WriteString(strconv.Quote(define.Value))
		}
	}
	return shaderKey{path: path, defines: joinedDefines.String()}, sortedDefines
}

// compile preprocesses k's file with defines, its own sorted, makes its module,
// labelled with shader, reflects its entry points and bindings, and acquires from layouts
// the layout of the module's bindings, answering them in a slot. A failure
// answers the slot with nothing, what it made released, and its error, with
// the kind it is reported as: ReportKindLoadFailed for a file that fails to preprocess or
// compile, ReportKindShaderRefused for one refused for its bindings, and none
// for an error of gfx's to log.
func (k shaderKey) compile(device gfx.Device, layouts *layoutInterner, files fs.FS, shader Shader, defines []gfx.ShaderDefine) (slot shaderSlot, reportKind ReportKind, err error) {
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
		entry.Buffer.HasDynamicOffset = (entry.Buffer.Type == gfx.BufferBindingTypeUniform && binding.Group != 1) || (storageBuffer && binding.Group == 2)
		groups[binding.Group].Entries = append(groups[binding.Group].Entries, entry)
	}
	for _, group := range groups {
		slices.SortFunc(group.Entries, func(a, b gfx.BindGroupLayoutEntry) int { return cmp.Compare(a.Binding, b.Binding) })
	}
	return groups, nil
}
