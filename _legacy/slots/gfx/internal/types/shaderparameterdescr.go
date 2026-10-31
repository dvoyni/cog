package types

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// ShaderParameterDescr is one declarative shader parameter: a whole binding's
// value, named by the binding's WGSL global name. It is one of four kinds -
// bytes for a uniform, a texture, a sampler, or a buffer and the range of it
// bound - built with the *Param constructors and handed to
// ResourceQueue.NewDrawParams, ResourceQueue.UpdateDrawParams or
// OpQueue.SetDrawParams. Kind says which of the fields below carry the value.
//
// The fields are ordered widest alignment first so the byte-sized ones share
// the struct's tail instead of each padding out a word.
type ShaderParameterDescr struct {
	// Name is the binding's declared shader name.
	Name    string
	Texture TextureDescr
	Buffer  BufferDescr
	Sampler SamplerDesc
	// Raw holds the bytes that do not fit in Small, already laid out the way
	// the shader reads them. It is static, for the reason BufferDescr.Bytes
	// gives.
	Raw assets.Blob
	// BufferOffset and BufferSize bind a range of Buffer, which is how a draw
	// addresses its own record in a shared arena. A zero size means the whole
	// buffer from the offset. They are 32 bits wide because an arena outgrows
	// 16 in a few hundred records, and 32 is what the bind group carries.
	BufferOffset uint32
	BufferSize   uint32
	// Small holds a bytes parameter's bytes when they fit, which every typed
	// constructor's do: a parameter built per draw then carries its value
	// without an allocation behind it. SmallLen is how many of them are used.
	Small    [ParameterDescriptorInlineBytesCount]byte
	SmallLen uint8
	// Kind says which fields carry the value, and for a uniform value what
	// built its bytes, so a reader can ask for it back as the type it was
	// given as and an agent be shown it so.
	Kind ShaderParameterKind
}

// ShaderParameterFloat creates a parameter carrying the four bytes of one f32.
func ShaderParameterFloat(name string, v float32) ShaderParameterDescr {
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindFloat, SmallLen: 4}
	binary.LittleEndian.PutUint32(p.Small[:], math.Float32bits(v))
	return p
}

// ShaderParameterVec4 creates a parameter carrying the sixteen bytes of one
// vec4f.
func ShaderParameterVec4(name string, v m.Vec4) ShaderParameterDescr {
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindVec4, SmallLen: 16}
	binary.LittleEndian.PutUint32(p.Small[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(p.Small[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(p.Small[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(p.Small[12:], math.Float32bits(v.W))
	return p
}

// ShaderParameterMat4 creates a parameter carrying the sixty-four bytes of one
// mat4x4f.
func ShaderParameterMat4(name string, m m.Mat4) ShaderParameterDescr {
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindMat4, SmallLen: 64}
	for i := range m {
		binary.LittleEndian.PutUint32(p.Small[i*4:], math.Float32bits(m[i]))
	}
	return p
}

// ShaderParameterColor creates a parameter carrying a color as the sixteen
// bytes of one vec4f, r, g, b, a.
func ShaderParameterColor(name string, c m.Color) ShaderParameterDescr {
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindColor, SmallLen: 16}
	binary.LittleEndian.PutUint32(p.Small[0:], math.Float32bits(c.R))
	binary.LittleEndian.PutUint32(p.Small[4:], math.Float32bits(c.G))
	binary.LittleEndian.PutUint32(p.Small[8:], math.Float32bits(c.B))
	binary.LittleEndian.PutUint32(p.Small[12:], math.Float32bits(c.A))
	return p
}

// ShaderParameterTexture creates a texture parameter from a texture descriptor.
func ShaderParameterTexture(name string, tex TextureDescr) ShaderParameterDescr {
	return ShaderParameterDescr{Name: name, Kind: ShaderParameterKindTexture, Texture: tex}
}

// ShaderParameterSampler creates a sampler parameter. The zero SamplerDesc
// clamps and filters linearly.
func ShaderParameterSampler(name string, desc SamplerDesc) ShaderParameterDescr {
	return ShaderParameterDescr{Name: name, Kind: ShaderParameterKindSampler, Sampler: desc}
}

// ShaderParameterBuffer creates a buffer parameter from a buffer descriptor,
// binding the whole buffer.
func ShaderParameterBuffer(name string, buf BufferDescr) ShaderParameterDescr {
	return ShaderParameterDescr{Name: name, Kind: ShaderParameterKindBuffer, Buffer: buf}
}

// ShaderParameterBufferRange binds one slice of a buffer, which is how a draw
// addresses its own record in a shared arena: the binding is the addressing, so
// no index has to be agreed on across the record/translate thread boundary.
// offset must be a multiple of StorageAlignment.
func ShaderParameterBufferRange(name string, buf BufferDescr, offset, size int) ShaderParameterDescr {
	return ShaderParameterDescr{Name: name, Kind: ShaderParameterKindBuffer, Buffer: buf, BufferOffset: uint32(offset), BufferSize: uint32(size)}
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
//
// A value of up to sixty-four bytes is carried inside the descriptor, as the
// typed constructors' are, so a small record set per draw costs no allocation;
// a larger one is copied out once, here.
func ShaderParameterRaw[T any](name string, value T) ShaderParameterDescr {
	validateRawLayout(reflect.TypeFor[T]())
	size := int(unsafe.Sizeof(value))
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&value)), size)
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindRaw}
	if size <= ParameterDescriptorInlineBytesCount {
		p.SmallLen = uint8(copy(p.Small[:], bytes))
		return p
	}
	p.Raw = assets.NewBlob(append([]byte(nil), bytes...))
	return p
}

// ShaderParameterRawRef is ShaderParameterRaw over a value the caller keeps,
// for a record larger than sixty-four bytes set every frame: it is validated
// the same way, and a value that fits is carried inside the descriptor as
// ShaderParameterRaw's is, but a larger one is not copied out. The descriptor
// borrows *value, so the call it is handed to must read it before the caller
// changes it. ResourceQueue.NewDrawParams, UpdateDrawParams and
// OpQueue.SetDrawParams all copy a param's bytes before they return, so a
// caller refilling one record per batch and handing each fill to SetDrawParams
// allocates nothing.
//
// value must point at memory that outlives the call: taking the address of a
// local moves it to the heap, which is the allocation this exists to avoid.
func ShaderParameterRawRef[T any](name string, value *T) ShaderParameterDescr {
	validateRawLayout(reflect.TypeFor[T]())
	size := int(unsafe.Sizeof(*value))
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(value)), size)
	p := ShaderParameterDescr{Name: name, Kind: ShaderParameterKindRaw}
	if size <= ParameterDescriptorInlineBytesCount {
		p.SmallLen = uint8(copy(p.Small[:], bytes))
		return p
	}
	p.Raw = assets.NewBlob(bytes)
	return p
}

// ColorValue returns the parameter's color and true when ShaderParameterColor
// built it.
func (p ShaderParameterDescr) ColorValue() (m.Color, bool) {
	if p.Kind != ShaderParameterKindColor {
		return m.Color{}, false
	}
	return m.Color{R: p.float(0), G: p.float(1), B: p.float(2), A: p.float(3)}, true
}

// FloatValue returns the parameter's scalar and true when ShaderParameterFloat
// built it.
func (p ShaderParameterDescr) FloatValue() (float32, bool) {
	if p.Kind != ShaderParameterKindFloat {
		return 0, false
	}
	return p.float(0), true
}

// VecValue returns the parameter's vec4 and true when ShaderParameterVec4 built
// it.
func (p ShaderParameterDescr) VecValue() (m.Vec4, bool) {
	if p.Kind != ShaderParameterKindVec4 {
		return m.Vec4{}, false
	}
	return m.Vec4{X: p.float(0), Y: p.float(1), Z: p.float(2), W: p.float(3)}, true
}

// MatValue returns the parameter's mat4 and true when ShaderParameterMat4 built
// it.
func (p ShaderParameterDescr) MatValue() (m.Mat4, bool) {
	if p.Kind != ShaderParameterKindMat4 {
		return m.Mat4{}, false
	}
	var value m.Mat4
	for i := range value {
		value[i] = p.float(i)
	}
	return value, true
}

// float reads the i-th f32 of the parameter's inline bytes.
func (p *ShaderParameterDescr) float(i int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(p.Small[i*4:]))
}

// ValueSize reports how many bytes AppendValueTo would append, and zero for a
// binding. A recorder collecting one name's values across many items needs it to
// tell that the items agree on the element size: one name at two kinds would
// otherwise pack an array the shader strides through wrongly, which is a wrong
// picture with nothing reported.
func (p ShaderParameterDescr) ValueSize() int {
	if !p.Kind.IsValue() {
		return 0
	}
	if p.Raw.Len() > 0 {
		return p.Raw.Len()
	}
	return int(p.SmallLen)
}

// AppendValueTo appends the parameter's value to dst in the byte layout a shader
// reads it at - four bytes for a float, sixteen for a color or vec4, sixty-four
// for a mat4, and a raw parameter's own bytes - and reports whether it had a
// value at all.
//
// A texture, sampler or buffer parameter is a binding rather than a value, so it
// appends nothing and reports false. This is what lets a recorder pack the same
// parameter into an array without a type switch of its own, and the growth of
// dst is the value's size.
func (p ShaderParameterDescr) AppendValueTo(dst []byte) ([]byte, bool) {
	if !p.Kind.IsValue() {
		return dst, false
	}
	return append(dst, p.Bytes()...), true
}

// Bytes is a bytes parameter's value, wherever it is held. It aliases the
// descriptor and must not be written to.
func (p *ShaderParameterDescr) Bytes() []byte {
	if p.Raw.Len() > 0 {
		return p.Raw.Data()
	}
	return p.Small[:p.SmallLen]
}

// MarshalJSON reports the parameter's name, its kind, and the value that kind
// carries alone: a tagged union serializes to exactly one value, not the
// fields another kind would use. A numeric value is its components in shader order - one for a
// float, four for a color (r, g, b, a) or a vec4 (x, y, z, w), sixteen for a
// mat4 - and a raw one only its length, since its bytes are laid out for one
// shader and an agent can do nothing with them but carry them.
func (p ShaderParameterDescr) MarshalJSON() ([]byte, error) {
	document := struct {
		Name         string              `json:"name"`
		Kind         ShaderParameterKind `json:"kind"`
		Value        []float32           `json:"value,omitempty"`
		Texture      *TextureDescr       `json:"texture,omitempty"`
		Sampler      *SamplerDesc        `json:"sampler,omitempty"`
		Buffer       *BufferDescr        `json:"buffer,omitempty"`
		BufferOffset uint32              `json:"bufferOffset,omitempty"`
		BufferSize   uint32              `json:"bufferSize,omitempty"`
		Bytes        int                 `json:"bytes,omitempty"`
	}{Name: p.Name, Kind: p.Kind}
	switch p.Kind {
	case ShaderParameterKindFloat:
		value, _ := p.FloatValue()
		document.Value = []float32{value}
	case ShaderParameterKindVec4:
		value, _ := p.VecValue()
		document.Value = []float32{value.X, value.Y, value.Z, value.W}
	case ShaderParameterKindColor:
		value, _ := p.ColorValue()
		document.Value = []float32{value.R, value.G, value.B, value.A}
	case ShaderParameterKindMat4:
		value, _ := p.MatValue()
		document.Value = value[:]
	case ShaderParameterKindRaw:
		document.Bytes = p.ValueSize()
	case ShaderParameterKindTexture:
		document.Texture = &p.Texture
	case ShaderParameterKindSampler:
		document.Sampler = &p.Sampler
	case ShaderParameterKindBuffer:
		document.Buffer, document.BufferOffset, document.BufferSize = &p.Buffer, p.BufferOffset, p.BufferSize
	}
	return json.Marshal(document)
}
