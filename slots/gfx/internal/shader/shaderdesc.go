package shader

import "strconv"

// ShaderDesc describes a shader module to create from opaque, backend-specific
// source bytes (WGSL for the gogpu backend). gfx flattens a shader's sources
// before it hands Code over, so a backend never sees a preprocessor directive.
type ShaderDesc struct {
	Code  []byte
	Label string
}

// ShaderLayout describes a shader's reflected bindings: uniform blocks, storage
// buffers, textures and samplers, each one a ShaderResource. The translator packs
// params into a uniform block at its members' offsets and binds every other
// resource by matching its name to a material parameter.
type ShaderLayout struct {
	Resources []ShaderResource
	// VertexInputs is every @location the vertex stage declares, which is the
	// half of the vertex interface only the shader knows. The other half is the
	// mesh's vertex layout, and gfx.CheckVertexInterface is where they meet.
	VertexInputs []ShaderVertexInput
}

// ShaderVertexInput is one @location input of a shader's vertex stage: where it
// binds and what it declares, reduced to the pair a vertex format can be
// compared against.
//
// The declared type is carried as (Kind, Count) rather than as source text
// because that is what the comparison is over: a format decodes to a scalar
// kind and a component count, and nothing in a spelling like "vec3<f32>"
// survives into the hardware beyond those two numbers.
type ShaderVertexInput struct {
	Name     string
	Location int
	Kind     VertexScalar
	Count    int
}

// UniformBlock is the shader's uniform block, or nil when it declares none. gfx
// packs one block per draw, and reflection refuses a shader declaring a second,
// so the first is the only one.
func (l ShaderLayout) UniformBlock() *ShaderResource {
	for i := range l.Resources {
		if l.Resources[i].Kind.Base() == ResourceUniformBuffer {
			return &l.Resources[i]
		}
	}
	return nil
}

// StorageMember is one top-level member of a reflected uniform block or storage
// struct. Stride and Count are set for an array member - `lights:
// array<SceneLight, 16>` - and zero otherwise.
type StorageMember struct {
	Name   string
	Offset int
	Stride int
	Count  int
}

// ResourceKind is what a reflected binding is - one of the kinds below, in the
// low bits - plus the flags that refine it within its kind. Base separates the
// kind from its flags, so a switch over kinds names every one of them rather
// than leaving the texture as whatever matched no flag.
type ResourceKind uint8

const (
	// ResourceTexture is a sampled texture. It is the zero kind, so a resource
	// declared with no kind is one.
	ResourceTexture ResourceKind = iota
	ResourceSampler
	ResourceUniformBuffer
	ResourceStorageBuffer
)

// The flags refine a kind. WebGPU types a depth texture and a comparison sampler
// differently from a colour texture and a filtering sampler, and binding one
// where the other is declared is an error; a writable storage buffer is
// read_write rather than read.
const (
	// ResourceDepth marks a depth texture.
	ResourceDepth ResourceKind = 1 << (4 + iota)
	// ResourceComparison marks a comparison sampler.
	ResourceComparison
	// ResourceWritable marks a read_write storage buffer.
	ResourceWritable
)

// resourceKindMask covers the kind bits, below the flags.
const resourceKindMask ResourceKind = 1<<4 - 1

// Base is the kind without its flags.
func (k ResourceKind) Base() ResourceKind { return k & resourceKindMask }

// Has reports whether every bit of flag is set.
func (k ResourceKind) Has(flag ResourceKind) bool { return k&flag == flag }

// ShaderResource is a reflected uniform-block, storage-buffer, texture, or
// sampler binding.
type ShaderResource struct {
	Name string
	// Kind is what the binding is, with the flags that refine it. Size is a
	// uniform block's byte size and Members its layout, which is what gfx packs
	// a draw's params by.
	Kind        ResourceKind
	TextureView TextureViewDimension
	Group       int
	Binding     int
	Size        int
	// Members is the reflected layout of a uniform block's or storage struct's
	// top-level members. A storage struct's is how a recorder that declares no
	// uniform block at all - scene - packs its records.
	Members []StorageMember
}

// TextureViewDimension selects the texture view expected by a shader binding.
type TextureViewDimension uint8

const (
	TextureView2D TextureViewDimension = iota
	TextureView2DArray
)

// VertexScalar is the scalar type an attribute presents to the shader once the
// hardware has decoded it, which is not the same thing as the type its bytes
// are stored in: every normalized format arrives as float however many bits it
// occupies, and only the integer formats arrive as integers.
type VertexScalar uint8

const (
	// VertexScalarNone is the zero value: a type that decodes to nothing a
	// shader can read. No legal vertex format has it.
	VertexScalarNone VertexScalar = iota
	VertexScalarFloat
	VertexScalarUint
	VertexScalarSint
)

// WGSL renders the type a shader would declare for this kind at this many
// components, which is the spelling an author has to change to fix a mismatch.
func (s VertexScalar) WGSL(count int) string {
	scalar := "?"
	switch s {
	case VertexScalarFloat:
		scalar = "f32"
	case VertexScalarUint:
		scalar = "u32"
	case VertexScalarSint:
		scalar = "i32"
	}
	if count <= 1 {
		return scalar
	}
	return "vec" + strconv.Itoa(count) + "<" + scalar + ">"
}
