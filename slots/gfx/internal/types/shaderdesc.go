package types

// ShaderDesc describes a shader module to create from opaque, backend-specific
// source bytes (WGSL for the wgpu backend). gfx flattens a shader's sources
// before it hands Code over, so a backend never sees a preprocessor directive.
type ShaderDesc struct {
	Code  []byte
	Label string
}

// ShaderLayout describes a shader's reflected bindings: the uniform parameter
// block (members + byte offsets, and its group/binding) plus texture and sampler
// resources. The translator packs params at their declared offsets and binds
// each resource by matching its name to a material parameter.
type ShaderLayout struct {
	UniformSize    int
	UniformGroup   int
	UniformBinding int
	Uniforms       []UniformMember
	Resources      []ShaderResource
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

// UniformMember is one member of the shader-parameter uniform block: its name and
// byte offset within the block.
type UniformMember struct {
	Name   string
	Offset int
}

// StorageMember is one top-level member of a reflected storage struct. Stride
// and Count are set for an array member - `lights: array<SceneLight, 16>` - and
// zero otherwise.
type StorageMember struct {
	Name   string
	Offset int
	Stride int
	Count  int
}

// ShaderResource is a reflected texture, sampler, or storage-buffer binding.
type ShaderResource struct {
	Name           string
	Sampler        bool
	StorageBuffer  bool
	WritableBuffer bool
	// Depth marks a depth texture and Comparison a comparison sampler: WebGPU
	// types those bindings differently from a colour texture and its filtering
	// sampler, and binding one where the other is declared is an error.
	Depth       bool
	Comparison  bool
	TextureView TextureViewDimension
	Group       int
	Binding     int
	// Members is the reflected layout of a storage struct's top-level members,
	// which is how a recorder that declares no uniform block at all - scene -
	// packs its records.
	Members []StorageMember
}
