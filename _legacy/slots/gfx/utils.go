package gfx

import (
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx/internal"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// ShaderWithText describes a shader from inline source bytes (e.g. WGSL).
func ShaderWithText(text string, opts ...ShaderOption) ShaderDescr {
	return shader.ShaderWithText(text, opts...)
}

// ShaderWithResource describes a shader loaded from storage.FileSystem.
func ShaderWithResource(path string, opts ...ShaderOption) ShaderDescr {
	return shader.ShaderWithResource(path, opts...)
}

// ShaderDefine supplies a valueless flag, readable by the source's #if
// conditionals and never reaching WGSL. Supplying a define a source does not
// mention is harmless; nothing can unset one.
func ShaderDefine(name string) ShaderOption {
	return shader.ShaderDefine(name)
}

// ShaderConst supplies a value for a #const the source declares, overriding
// that declaration's default. The value is WGSL text the preprocessor never
// interprets - the type rides in the value, so "16" and "16u" differ - and a
// name no source declares is silently ignored, which keeps one const map usable
// across a family of shaders.
func ShaderConst(name, value string) ShaderOption {
	return shader.ShaderConst(name, value)
}

// TextureWithResource describes a texture loaded from storage.FileSystem. It is
// always sRGB: the loader decodes PNG and JPEG, both of which are gamma-encoded
// by definition, so there is nothing for a caller to choose and a caller that
// chose wrong would be a silently wrong picture. A data map - normals,
// metallic-roughness, occlusion - is not a picture and does not come through
// here; it comes through TextureWithBytes, which does take a format.
func TextureWithResource(path string) TextureDescr {
	return types.TextureWithResource(path)
}

// TextureWithBytes describes a texture from inline pixel bytes. copyData
// snapshots pixels when true; when false, the caller must keep them unchanged
// until the recorded frame is consumed or dropped. mipmaps generates a full mip
// chain at bake time for smoother minification.
func TextureWithBytes(width, height int, format TextureFormat, pixels []byte, copyData, mipmaps bool) TextureDescr {
	return types.TextureWithBytes(width, height, format, pixels, copyData, mipmaps)
}

// BufferDescrWithBlob describes a buffer from inline bytes. copyData snapshots
// the bytes when recorded if true; when false, the caller must keep them
// unchanged until the recorded frame is consumed or dropped.
func BufferDescrWithBlob(data assets.Blob, copyData bool) BufferDescr {
	return types.BufferDescrWithBlob(data, copyData)
}

// ShaderParameterFloat creates a parameter carrying the four bytes of one f32,
// which fills a uniform declared at f32.
func ShaderParameterFloat(name string, v float32) ShaderParameterDescr {
	return types.ShaderParameterFloat(name, v)
}

// ShaderParameterVec4 creates a parameter carrying the sixteen bytes of one
// vec4f.
func ShaderParameterVec4(name string, v m.Vec4) ShaderParameterDescr {
	return types.ShaderParameterVec4(name, v)
}

// ShaderParameterMat4 creates a parameter carrying the sixty-four bytes of one
// mat4x4f.
func ShaderParameterMat4(name string, m m.Mat4) ShaderParameterDescr {
	return types.ShaderParameterMat4(name, m)
}

// ShaderParameterColor creates a parameter carrying a color as the sixteen
// bytes of one vec4f, r, g, b, a.
func ShaderParameterColor(name string, c m.Color) ShaderParameterDescr {
	return types.ShaderParameterColor(name, c)
}

// ShaderParameterTexture creates a texture parameter from a texture descriptor.
func ShaderParameterTexture(name string, tex TextureDescr) ShaderParameterDescr {
	return types.ShaderParameterTexture(name, tex)
}

// ShaderParameterSampler creates a sampler parameter. The zero SamplerDesc
// clamps and filters linearly.
func ShaderParameterSampler(name string, desc SamplerDesc) ShaderParameterDescr {
	return types.ShaderParameterSampler(name, desc)
}

// ShaderParameterBuffer creates a buffer parameter from a buffer descriptor,
// binding the whole buffer.
func ShaderParameterBuffer(name string, buf BufferDescr) ShaderParameterDescr {
	return types.ShaderParameterBuffer(name, buf)
}

// ShaderParameterBufferRange binds one slice of a buffer, which is how a draw
// addresses its own record in a shared arena: the binding is the addressing, so
// no index has to be agreed on across the record/translate thread boundary.
// offset must be a multiple of StorageAlignment.
func ShaderParameterBufferRange(name string, buf BufferDescr, offset, size int) ShaderParameterDescr {
	return types.ShaderParameterBufferRange(name, buf, offset, size)
}

// ShaderParameterRaw creates a parameter carrying an arbitrary plain-data
// struct by copying its bytes, so a shader value that is a record rather than a
// scalar or a vector can be named like any other parameter.
//
// T's memory layout is validated against WGSL's alignment rules once per type,
// and a mismatch panics. That check is the whole point of the constructor. Go
// aligns a struct to the widest alignment of its fields, which for float32 data
// is 4; WGSL aligns vec2 to 8 and vec3, vec4 and matrices to 16. So
//
//	type bad struct { A float32; B m.Vec3 }
//
// is 16 bytes in Go and 32 in WGSL: it passes any size-based check, and every
// element after the first reads the wrong memory with no error anywhere. The
// panic names the field, both offsets, and the padding that would fix it.
//
// Only plain-data members are accepted - float32, int32, uint32, m.Vec2,
// m.Vec3, m.Vec4, m.Color, m.Quat, m.Mat4, arrays of those, and structs of
// those. Any other member type panics, which is what keeps a pointer out of a
// byte copy.
func ShaderParameterRaw[T any](name string, value T) ShaderParameterDescr {
	return types.ShaderParameterRaw[T](name, value)
}

// ShaderParameterRawRef is ShaderParameterRaw over a value the caller keeps:
// validated the same way, but a record larger than sixty-four bytes is borrowed
// rather than copied, so the call it is handed to must read it before the
// caller changes it. NewDrawParams, UpdateDrawParams and SetDrawParams copy a
// param's bytes before they return, which is what lets a record refilled per
// batch and set through SetDrawParams cost no allocation. value must outlive
// the call: the address of a local escapes to the heap.
func ShaderParameterRawRef[T any](name string, value *T) ShaderParameterDescr {
	return types.ShaderParameterRawRef[T](name, value)
}

// FingerprintParams hashes a parameter slice in order by name, kind and value:
// one seed, one encoding per kind, and inline texture and buffer bytes by
// pointer identity rather than by content.
//
// It exists because a recorder that keys a batch on a draw's parameters would
// otherwise write the comparison itself, and a hand-written type switch would
// silently mis-key every kind or field it forgot - and mis-keying merges two
// draws that differ, which draws the wrong picture rather than costing a batch.
func FingerprintParams(params []ShaderParameterDescr) uint64 {
	return internal.FingerprintParams(params)
}

// MeshDescrWithVertices builds non-indexed geometry from an interleaved vertex
// buffer, a topology, and the vertex layout.
func MeshDescrWithVertices(vertices BufferDescr, topology PrimitiveTopology, layout ...VertexAttribute) MeshDescr {
	return types.MeshDescrWithVertices(vertices, topology, layout...)
}

// MeshDescrWithIndices builds indexed geometry from vertex and index buffers,
// the width one index of that buffer is written at, a topology, and the vertex
// layout. A zero index buffer (as passed by MeshDescrWithVertices) yields a
// non-indexed mesh.
//
// The width describes the bytes rather than constraining them: gfx has no way
// to know how a caller wrote its indices, so declaring uint16 over uint32 bytes
// reads pairs of indices as one. What it can check - that the buffer's length
// divides by the width - it checks where the draw is translated, since this is
// a pure value constructor with no error return.
func MeshDescrWithIndices(
	vertices, indices BufferDescr, width IndexWidth,
	topology PrimitiveTopology, layout ...VertexAttribute,
) MeshDescr {
	return types.MeshDescrWithIndices(vertices, indices, width, topology, layout...)
}

// TargetDescrScreen is the frame's screen attachment. It stays a sentinel the
// recorder cannot resolve: the swapchain view is per-frame and known only on
// the render thread.
func TargetDescrScreen() TargetDescr {
	return types.TargetDescrScreen()
}

// TargetDescrTexture renders into one mip level of one layer of a texture, which
// must have been allocated Renderable.
func TargetDescrTexture(texture TextureDescr, mip, layer int) TargetDescr {
	return types.TargetDescrTexture(texture, mip, layer)
}

// TargetDescrNone declares a pass with no colour attachment, such as a depth-only
// prepass.
func TargetDescrNone() TargetDescr {
	return types.TargetDescrNone()
}

// DepthDescrAuto uses the backend's own depth texture for the target's size. Every
// DepthDescrAuto pass at a given size shares one texture, so a pass that means to
// start from a clean depth buffer must clear depth or it inherits whatever the
// previous pass at that size left behind.
func DepthDescrAuto() DepthDescr {
	return types.DepthDescrAuto()
}

// DepthDescrNone declares a pass with no depth attachment.
func DepthDescrNone() DepthDescr {
	return types.DepthDescrNone()
}

// DepthDescrTarget renders depth into a texture, which must be FormatDepth32F and
// Renderable.
func DepthDescrTarget(texture TextureDescr) DepthDescr {
	return types.DepthDescrTarget(texture)
}

// StateOpaque3D returns the state of opaque geometry, the first of the three
// states the engine's passes are made of: opaque geometry, then transparent
// geometry over it, then 2D on top of everything.
func StateOpaque3D() DrawState {
	return internal.StateOpaque3D()
}

// StateTransparent3D returns the state of transparent geometry drawn over
// opaque geometry; see StateOpaque3D.
func StateTransparent3D() DrawState {
	return internal.StateTransparent3D()
}

// StateOverlay2D returns the state of 2D drawn on top of everything; see
// StateOpaque3D.
func StateOverlay2D() DrawState {
	return internal.StateOverlay2D()
}

// DefaultLimits returns the WebGPU spec floor every browser guarantees. It is
// the comparison target on purpose: a desktop adapter reports its hardware
// limits, where 200 storage buffers is ordinary, so checking a shader against
// the device it happens to run on passes builds that cannot run in a browser.
func DefaultLimits() PipelineLimits {
	return internal.DefaultLimits()
}

// CheckVertexInterface reports the first way a vertex layout fails the shader
// about to be drawn with it, and nil when the pair is legal. Three things can
// be wrong: an @location the layout does not supply at all, one it supplies at
// a different type, and a stride WebGPU refuses outright.
//
// **The type rule is equality of (kind, count), deliberately stricter than
// WebGPU.** WebGPU fills a shader's missing components with (0, 0, 0, 1) and
// drops the extra ones, so `vec3<f32>` over a two-component unorm yields
// (x, y, 0) - a plausible unit-ish direction lying in the XY plane. That is not
// a black screen and not garbage triangles; it is wrong shading that looks like
// art, in an engine with no pixel readback anywhere. Presence-only and
// base-type-compatible were both considered and both let exactly that case
// through, because a two-component unorm decodes to f32 and `vec3<f32>` is f32.
// The cost of forbidding WebGPU's legal widening and narrowing is zero against
// the tree: every @location in bundles/model/internal/builtin and bundles/canvas/internal/builtin is an exact
// match already.
//
// **The check is one-directional.** A layout supplying an attribute the shader
// does not read is legal and common - scene's bundled vertex struct declares six
// of eight under the no-skin variant. The direction that fails is a shader input
// no attribute supplies, never the other way round.
//
// It is an exported plain function for the reason FlattenShader is: it is the
// test surface for a rule that otherwise could only be exercised through a
// backend, and a package that owns both halves of a pair - scene holds its
// layout and its shader - can ask the same question gfx will ask at draw time,
// through the same call.
func CheckVertexInterface(shader string, layout ShaderLayout, attrs []VertexAttribute) error {
	return internal.CheckVertexInterface(shader, layout, attrs)
}

// SnapshotViewOf fills in the three coordinate sizes from a viewport. The
// step fields belong to the capability body, which is the only place that
// knows whether one was performed, and Tick to the snapshot, which is the
// only thing produced inside the tick it names.
func SnapshotViewOf(viewport Viewport) SnapshotView {
	return internal.SnapshotViewOf(viewport)
}
