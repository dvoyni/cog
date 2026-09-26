package descriptors

import (
	"encoding/binary"
	"hash/maphash"
	"math"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// ParameterDescr is one declarative shader parameter: a whole binding's value,
// named by the binding's WGSL global name. It is one of four kinds - bytes for
// a uniform, a texture, a sampler, or a buffer and the range of it bound -
// built with the *Param constructors and handed to ResourceQueue.NewDrawParams,
// OpQueue.SetDrawParams, Material or OpQueue.Draw.
type ParameterDescr struct {
	name    string
	kind    ParamKind
	texture TextureDescr
	buffer  BufferDescr
	// bufferOffset and bufferSize bind a range of buffer. A zero size means the
	// whole buffer from the offset.
	bufferOffset int
	bufferSize   int
	sampler      types.SamplerDesc
	// form is what a bytes parameter's bytes were built from. It changes
	// nothing about how they bind - a uniform takes bytes of its own size
	// whatever made them - and is kept only so a reader can still ask for the
	// value back as the type it was given as, and an agent be shown it so.
	form ParamForm
	// small holds a bytes parameter's bytes when they fit, which every typed
	// constructor's do: a parameter built per draw then carries its value
	// without an allocation behind it. size is how many of them are used.
	small [smallBytes]byte
	size  uint8
	// raw holds the bytes that do not fit in small, already laid out the way
	// the shader reads them. It is static, for the reason BufferDescr.bytes
	// gives.
	raw assets.Blob
}

// smallBytes is the largest value a parameter carries inline: a mat4, the
// largest typed constructor's value.
const smallBytes = 64

// ParamKind tags the variant stored in a ParameterDescr. There are four, one
// per kind of binding a shader declares.
type ParamKind uint8

const (
	paramNone ParamKind = iota
	// ParamBytes is a uniform binding's value: bytes of the binding's own
	// size and WGSL layout.
	ParamBytes
	ParamTexture
	ParamSampler
	ParamBuffer
)

// ValueKind reports whether the parameter carries bytes a shader reads out of
// a uniform, as opposed to a binding it attaches to a bind group.
func (k ParamKind) ValueKind() bool { return k == ParamBytes }

// String names the kind the way an error message wants it: what a caller
// wrote, not what the enum is called.
func (k ParamKind) String() string {
	switch k {
	case ParamBytes:
		return "bytes"
	case ParamTexture:
		return "texture"
	case ParamSampler:
		return "sampler"
	case ParamBuffer:
		return "buffer"
	}
	return "none"
}

// ParamForm is what a bytes parameter's bytes were built from: one of the typed
// constructors, or RawParameter.
type ParamForm uint8

const (
	FormRaw ParamForm = iota
	FormFloat
	FormVec4
	FormMat4
	FormColor
)

// String names the form the way an inspection view spells a parameter's kind.
func (f ParamForm) String() string {
	switch f {
	case FormFloat:
		return "float"
	case FormVec4:
		return "vec4"
	case FormMat4:
		return "mat4"
	case FormColor:
		return "color"
	}
	return "raw"
}

// FloatParam creates a parameter carrying the four bytes of one f32.
func FloatParam(name string, v float32) ParameterDescr {
	p := ParameterDescr{name: name, kind: ParamBytes, form: FormFloat, size: 4}
	binary.LittleEndian.PutUint32(p.small[:], math.Float32bits(v))
	return p
}

// VecParam creates a parameter carrying the sixteen bytes of one vec4f.
func VecParam(name string, v m.Vec4) ParameterDescr {
	p := ParameterDescr{name: name, kind: ParamBytes, form: FormVec4, size: 16}
	WriteVec4(p.small[:16], v)
	return p
}

// MatParam creates a parameter carrying the sixty-four bytes of one mat4x4f.
func MatParam(name string, m m.Mat4) ParameterDescr {
	p := ParameterDescr{name: name, kind: ParamBytes, form: FormMat4, size: 64}
	WriteMat4(p.small[:64], m)
	return p
}

// ColorParam creates a parameter carrying a color as the sixteen bytes of one
// vec4f, r, g, b, a.
func ColorParam(name string, c m.Color) ParameterDescr {
	p := ParameterDescr{name: name, kind: ParamBytes, form: FormColor, size: 16}
	WriteColor(p.small[:16], c)
	return p
}

// TextureParam creates a texture parameter from a texture descriptor.
func TextureParam(name string, tex TextureDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamTexture, texture: tex}
}

// SamplerParam creates a sampler parameter. The zero SamplerDesc clamps and
// filters linearly.
func SamplerParam(name string, desc types.SamplerDesc) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamSampler, sampler: desc}
}

// BufferParam creates a buffer parameter from a buffer descriptor, binding the
// whole buffer.
func BufferParam(name string, buf BufferDescr) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamBuffer, buffer: buf}
}

// BufferRangeParam binds one slice of a buffer, which is how a draw addresses
// its own record in a shared arena: the binding is the addressing, so no index
// has to be agreed on across the record/translate thread boundary. offset must
// be a multiple of StorageAlignment.
func BufferRangeParam(name string, buf BufferDescr, offset, size int) ParameterDescr {
	return ParameterDescr{name: name, kind: ParamBuffer, buffer: buf, bufferOffset: offset, bufferSize: size}
}

// Name reports the parameter's declared shader name.
func (p ParameterDescr) Name() string { return p.name }

// ColorValue returns the parameter's color and true when ColorParam built it.
func (p ParameterDescr) ColorValue() (m.Color, bool) {
	if p.kind != ParamBytes || p.form != FormColor {
		return m.Color{}, false
	}
	return m.Color{R: p.float(0), G: p.float(1), B: p.float(2), A: p.float(3)}, true
}

// FloatValue returns the parameter's scalar and true when FloatParam built it.
func (p ParameterDescr) FloatValue() (float32, bool) {
	if p.kind != ParamBytes || p.form != FormFloat {
		return 0, false
	}
	return p.float(0), true
}

// TextureValue returns the parameter's texture and true when it is a texture parameter.
func (p ParameterDescr) TextureValue() (TextureDescr, bool) { return p.texture, p.kind == ParamTexture }

// SamplerValue returns the parameter's sampler and true when it is a sampler parameter.
func (p ParameterDescr) SamplerValue() (types.SamplerDesc, bool) {
	return p.sampler, p.kind == ParamSampler
}

// VecValue returns the parameter's vec4 and true when VecParam built it.
func (p ParameterDescr) VecValue() (m.Vec4, bool) {
	if p.kind != ParamBytes || p.form != FormVec4 {
		return m.Vec4{}, false
	}
	return m.Vec4{X: p.float(0), Y: p.float(1), Z: p.float(2), W: p.float(3)}, true
}

// MatValue returns the parameter's mat4 and true when MatParam built it.
func (p ParameterDescr) MatValue() (m.Mat4, bool) {
	if p.kind != ParamBytes || p.form != FormMat4 {
		return m.Mat4{}, false
	}
	var value m.Mat4
	for i := range value {
		value[i] = p.float(i)
	}
	return value, true
}

// float reads the i-th f32 of the parameter's inline bytes.
func (p *ParameterDescr) float(i int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(p.small[i*4:]))
}

// BufferValue returns the parameter's buffer and true when it is a buffer
// parameter. The range it binds is BufferRange, not part of the descriptor:
// two draws addressing their own slices of one arena share the buffer and
// differ only in the range.
func (p ParameterDescr) BufferValue() (BufferDescr, bool) {
	return p.buffer, p.kind == ParamBuffer
}

// BufferRange returns the slice of the buffer this parameter binds, and true
// when it is a buffer parameter. A zero size means the whole buffer from the
// offset.
func (p ParameterDescr) BufferRange() (offset, size int, ok bool) {
	return p.bufferOffset, p.bufferSize, p.kind == ParamBuffer
}

// RawLen returns the byte length of a raw parameter's data and true when
// RawParameter built it. The bytes themselves stay inside the descriptor: they
// are already laid out the way one shader reads them, so nothing outside can do
// anything with them but forward them, and a reader that only wants to know how
// much is travelling gets the number without the copy.
func (p ParameterDescr) RawLen() (int, bool) {
	return p.ValueSize(), p.kind == ParamBytes && p.form == FormRaw
}

// HasValue reports whether the parameter carries bytes a shader reads out of a
// uniform, as opposed to a binding it attaches to a bind group. It is the
// question a recorder asks to decide whether a parameter can vary per item at
// all: a value can, and a bind group cannot, because there is one per draw.
func (p ParameterDescr) HasValue() bool { return p.kind.ValueKind() }

// ValueSize reports how many bytes AppendValue would append, and zero for a
// binding. A recorder collecting one name's values across many items needs it to
// tell that the items agree on the element size: one name at two kinds would
// otherwise pack an array the shader strides through wrongly, which is a wrong
// picture with nothing reported.
func (p ParameterDescr) ValueSize() int {
	if p.kind != ParamBytes {
		return 0
	}
	if p.raw.Len() > 0 {
		return p.raw.Len()
	}
	return int(p.size)
}

// AppendValue appends the parameter's value to dst in the byte layout a shader
// reads it at - four bytes for a float, sixteen for a color or vec4, sixty-four
// for a mat4, and a raw parameter's own bytes - and reports whether it had a
// value at all.
//
// A texture, sampler or buffer parameter is a binding rather than a value, so it
// appends nothing and reports false. This is what lets a recorder pack the same
// parameter into an array without a type switch of its own, and the growth of
// dst is the value's size.
func (p ParameterDescr) AppendValue(dst []byte) ([]byte, bool) {
	if p.kind != ParamBytes {
		return dst, false
	}
	return append(dst, p.bytes()...), true
}

// bytes is a bytes parameter's value, wherever it is held. It aliases the
// descriptor and must not be written to.
func (p *ParameterDescr) bytes() []byte {
	if p.raw.Len() > 0 {
		return p.raw.Data()
	}
	return p.small[:p.size]
}

// WriteMat4, WriteColor and WriteVec4 pack a value in the little-endian layout
// WGSL reads.
func WriteMat4(dst []byte, m m.Mat4) {
	for i := 0; i < 16; i++ {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(m[i]))
	}
}

func WriteColor(dst []byte, c m.Color) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(c.R))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(c.G))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(c.B))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(c.A))
}

func WriteVec4(dst []byte, v m.Vec4) {
	binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(v.X))
	binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(v.Y))
	binary.LittleEndian.PutUint32(dst[8:], math.Float32bits(v.Z))
	binary.LittleEndian.PutUint32(dst[12:], math.Float32bits(v.W))
}

func (p *ParameterDescr) fingerprint(h *maphash.Hash) {
	h.WriteString(p.name)
	writeUint(h, uint64(p.kind))
	switch p.kind {
	case ParamTexture:
		// The three cases are disjoint, so the id, the path and the blob say
		// between them which one this is: there is no source term to fold in.
		t := &p.texture
		writeUint(h, uint64(t.Params.id))
		h.WriteString(t.Name)
		writeUint(h, uint64(t.Params.width)|uint64(t.Params.height)<<32)
		writeUint(h, uint64(t.Params.format)|boolBit(t.Params.mipmaps)<<8)
		maphash.WriteComparable(h, t.Blob)
	case ParamBuffer:
		b := &p.buffer
		writeUint(h, uint64(b.source)|uint64(b.id)<<8)
		writeUint(h, uint64(b.size))
		maphash.WriteComparable(h, b.bytes)
		writeUint(h, uint64(p.bufferOffset)|uint64(p.bufferSize)<<32)
	case ParamBytes:
		// The form goes in beside the bytes: a color and a vec4 of the same
		// components are the same bytes, and a fingerprint that merged them
		// would merge two batches that inspect differently.
		writeUint(h, uint64(p.form))
		if p.raw.Len() > 0 {
			// Bytes held out of line hash by identity, as every inline run
			// does.
			maphash.WriteComparable(h, p.raw)
		} else {
			h.Write(p.small[:p.size])
		}
	case ParamSampler:
		s := &p.sampler
		writeUint(h, uint64(s.AddressU)|uint64(s.AddressV)<<8|uint64(s.Mag)<<16|
			uint64(s.Min)<<24|uint64(s.Mip)<<32|uint64(s.Anisotropy)<<40|
			boolBit(s.Comparison)<<48|uint64(s.Compare)<<56)
	}
}

func writeUint(h *maphash.Hash, value uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	h.Write(buf[:])
}

func boolBit(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
