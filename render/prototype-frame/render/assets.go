// Package render is a PROTOTYPE of the API of cog's render plugin, for
// https://github.com/dvoyni/cog/issues/815. It is throwaway: every body only
// records what render would do, so that a frame can be printed. Nothing here
// talks to a GPU, and the kernel is left out: where a user would reach a
// render resource through kernel.Read or kernel.Write, it gets a pointer.
package render

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Shader is a loaded variant: a WGSL file with one set of defines.
type Shader struct{ index uint32 }

// Texture is a loaded, made or temporary texture.
type Texture struct{ index uint32 }

// Buffer is a loaded or made buffer, with every usage.
type Buffer struct{ index uint32 }

// Mesh is made from buffers the user loaded or made.
type Mesh struct{ index uint32 }

// Material is a shader, its render state and the values bound to its
// parameters, made by the user.
type Material struct{ index uint32 }

// BufferRange is part of a buffer; a temporary buffer is always one.
type BufferRange struct {
	Buffer       Buffer
	Offset, Size uint64
}

// Define is one preprocessor define of a shader variant.
type Define struct{ Name, Value string }

// ColorSpace is how a texture's file encodes colour.
type ColorSpace uint8

const (
	ColorSpaceSRGB ColorSpace = iota
	ColorSpaceLinear
)

// TextureOptions are the load options that make one file several textures.
type TextureOptions struct {
	ColorSpace ColorSpace
	Mipmaps    bool
}

// VertexLayout is one vertex buffer's layout, abridged.
type VertexLayout struct {
	Stride     uint32
	Attributes []string
}

// VertexBuffer is a buffer range with its layout.
type VertexBuffer struct {
	Range  BufferRange
	Layout VertexLayout
}

// MeshDescriptor describes a mesh: vertex buffers, an optional index buffer
// and a topology (abridged to its name).
type MeshDescriptor struct {
	Label         string
	VertexBuffers []VertexBuffer
	Indices       BufferRange
	IndexCount    uint32
	Topology      string
}

// RenderState is abridged; its zero value draws with WebGPU's defaults.
type RenderState struct {
	Blend      string
	DepthTest  string
	DepthWrite bool
	Cull       string
}

// SamplerDescriptor is a sampler by value, abridged.
type SamplerDescriptor struct {
	Filter, Address string
}

// Assets is render's table of shaders, textures, buffers, meshes and
// materials. In the plugin it would be a kernel resource: loads and material
// changes under its write lock, a guess for the kernel ticket.
type Assets struct {
	mutex   sync.Mutex
	counts  map[string]int
	names   []string
	indices map[string]uint32
	values  map[uint32][]string
}

// NewAssets is the prototype's stand-in for the plugin installing Assets.
func NewAssets() *Assets {
	return &Assets{counts: map[string]int{}, indices: map[string]uint32{}, values: map[uint32][]string{}}
}

// AcquireShader loads path's variant with defines, or counts one more holder
// of it. The GPU object exists when it returns.
func (a *Assets) AcquireShader(path string, defines ...Define) Shader {
	parts := make([]string, len(defines))
	for index, define := range defines {
		parts[index] = define.Name + "=" + define.Value
	}
	sort.Strings(parts)
	return Shader{a.acquire(fmt.Sprintf("shader %s [%s]", path, strings.Join(parts, " ")))}
}

// AcquireTexture loads path as a texture, or counts one more holder of it.
// The texture exists when it returns; its contents land before the frame is
// submitted.
func (a *Assets) AcquireTexture(path string, options TextureOptions) Texture {
	return Texture{a.acquire(fmt.Sprintf("texture %s (colour space %d, mipmaps %v)", path, options.ColorSpace, options.Mipmaps))}
}

// AcquireBuffer loads path's bytes as a buffer, or counts one more holder.
func (a *Assets) AcquireBuffer(path string) Buffer {
	return Buffer{a.acquire("buffer " + path)}
}

// ReleaseShader gives up one hold; the last frees it at the frame's end.
func (a *Assets) ReleaseShader(shader Shader) { a.release(shader.index) }

// ReleaseTexture gives up one hold; the last frees it at the frame's end.
func (a *Assets) ReleaseTexture(texture Texture) { a.release(texture.index) }

// ReleaseBuffer gives up one hold; the last frees it at the frame's end.
func (a *Assets) ReleaseBuffer(buffer Buffer) { a.release(buffer.index) }

// MakeMesh makes a mesh, which owns none of the buffers it names.
func (a *Assets) MakeMesh(descriptor MeshDescriptor) Mesh {
	return Mesh{a.make("mesh " + descriptor.Label)}
}

// ReleaseMesh frees a mesh at the frame's end.
func (a *Assets) ReleaseMesh(mesh Mesh) { a.release(mesh.index) }

// MakeMaterial makes a material, which owns nothing it names.
func (a *Assets) MakeMaterial(label string, shader Shader, state RenderState) Material {
	return Material{a.make(fmt.Sprintf("material %s (%s)", label, a.name(shader.index)))}
}

// ReleaseMaterial frees a material at the frame's end.
func (a *Assets) ReleaseMaterial(material Material) { a.release(material.index) }

// SetMaterialTexture binds texture to the material's binding name.
func (a *Assets) SetMaterialTexture(material Material, name string, texture Texture) {
	a.setValue(material.index, name, a.name(texture.index))
}

// SetMaterialSampler binds a sampler, by value, to the binding name.
func (a *Assets) SetMaterialSampler(material Material, name string, sampler SamplerDescriptor) {
	a.setValue(material.index, name, fmt.Sprintf("sampler %+v", sampler))
}

// SetMaterialUniform binds value, a Go struct mirroring the WGSL struct, to
// the uniform block name. A frame draws the material with the values it
// holds when the frame is submitted, even if they change between its draws.
func (a *Assets) SetMaterialUniform[T any](material Material, name string, value T) {
	a.setValue(material.index, name, fmt.Sprintf("%+v", value))
}

// WriteBuffer fills buffer at offset for the whole frame: it lands before the
// frame's first pass.
func (a *Assets) WriteBuffer(buffer Buffer, offset uint64, data []byte) {}

func (a *Assets) acquire(key string) uint32 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.counts[key]++
	if index, found := a.indices[key]; found {
		return index
	}
	a.names = append(a.names, key)
	a.indices[key] = uint32(len(a.names))
	return uint32(len(a.names))
}

func (a *Assets) make(name string) uint32 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.names = append(a.names, name)
	return uint32(len(a.names))
}

func (a *Assets) release(index uint32) {}

func (a *Assets) name(index uint32) string {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if index == 0 || int(index) > len(a.names) {
		return "none"
	}
	return a.names[index-1]
}

func (a *Assets) setValue(index uint32, name, value string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	values := a.values[index]
	for at, entry := range values {
		if strings.HasPrefix(entry, name+" = ") {
			values[at] = name + " = " + value
			return
		}
	}
	a.values[index] = append(values, name+" = "+value)
}
