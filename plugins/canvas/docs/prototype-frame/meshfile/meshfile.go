// Package meshfile stands in for libs/meshfile, the mesh file format the
// meshes ticket settled: a version, a vertex layout, a topology, optional
// indices, bounds, and the vertex and index bytes. It imports no render, so
// an exporter writing mesh files needs neither render nor gfx, and a 3D
// plugin can share it.
package meshfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// Format is one vertex attribute's type, mirroring WebGPU's vertex formats.
type Format uint32

const (
	Float32 Format = iota + 1
	Float32x2
	Float32x3
	Float32x4
	Uint32
	Unorm8x4
)

// Attribute is one value each vertex holds: in Format, Offset bytes into the
// vertex, fed to the vertex stage's input at Location.
type Attribute struct {
	Format   Format
	Offset   uint32
	Location uint32
}

// Layout is how a vertex's bytes are laid out.
type Layout struct {
	Stride     uint32
	Attributes []Attribute
}

// VertexType is a vertex type that knows its layout: canvas.Vertex, or a
// layout of a user's own.
type VertexType interface{ VertexLayout() Layout }

// Topology is how vertices make primitives, its zero a triangle list.
type Topology uint32

const (
	TriangleList Topology = iota
	TriangleStrip
	LineList
	LineStrip
)

// File is a decoded mesh file. Vertices and Indices are slices of the bytes
// decoded, not copies.
type File struct {
	Layout      Layout
	Topology    Topology
	Bounds      m.Box3
	VertexCount uint32
	IndexCount  uint32
	IndexBits   uint32
	Vertices    []byte
	Indices     []byte
}

const (
	magic   = "CMSH"
	version = 1
)

type header struct {
	Magic          [4]byte
	Version        uint32
	Topology       uint32
	Stride         uint32
	AttributeCount uint32
	VertexCount    uint32
	IndexCount     uint32
	IndexBits      uint32
	Bounds         m.Box3
}

// Encode answers the mesh file of vertices, indices, topology and bounds.
// indices may be nil; they are written in 16 bits while the vertices fit.
// It refuses zero bounds, and a vertex type whose layout's stride is not its
// size.
func Encode[V VertexType](vertices []V, indices []uint32, topology Topology, bounds m.Box3) ([]byte, error) {
	var zero V
	layout := zero.VertexLayout()
	if uintptr(layout.Stride) != unsafe.Sizeof(zero) {
		return nil, fmt.Errorf("meshfile: %T's layout has a stride of %d, where it is %d bytes", zero, layout.Stride, unsafe.Sizeof(zero))
	}
	if bounds == (m.Box3{}) {
		return nil, errors.New("meshfile: a mesh needs its bounds")
	}
	h := header{Version: version, Topology: uint32(topology), Stride: layout.Stride, AttributeCount: uint32(len(layout.Attributes)),
		VertexCount: uint32(len(vertices)), IndexCount: uint32(len(indices)), IndexBits: 32, Bounds: bounds}
	copy(h.Magic[:], magic)
	if len(vertices) <= 1<<16 {
		h.IndexBits = 16
	}
	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, h)
	binary.Write(&out, binary.LittleEndian, layout.Attributes)
	if len(vertices) > 0 {
		out.Write(unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*int(layout.Stride)))
	}
	for _, index := range indices {
		if h.IndexBits == 16 {
			binary.Write(&out, binary.LittleEndian, uint16(index))
		} else {
			binary.Write(&out, binary.LittleEndian, index)
		}
	}
	return out.Bytes(), nil
}

// Decode answers the mesh file in data, its bytes slices of data.
func Decode(data []byte) (File, error) {
	var h header
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &h); err != nil || string(h.Magic[:]) != magic {
		return File{}, errors.New("meshfile: not a mesh file")
	}
	if h.Version != version {
		return File{}, fmt.Errorf("meshfile: version %d is not known", h.Version)
	}
	attributes := make([]Attribute, h.AttributeCount)
	if err := binary.Read(reader, binary.LittleEndian, attributes); err != nil {
		return File{}, errors.New("meshfile: its layout is cut short")
	}
	start := len(data) - reader.Len()
	vertexBytes := int(h.VertexCount * h.Stride)
	indexBytes := int(h.IndexCount * h.IndexBits / 8)
	if reader.Len() != vertexBytes+indexBytes {
		return File{}, errors.New("meshfile: its vertices and indices are cut short")
	}
	return File{
		Layout: Layout{Stride: h.Stride, Attributes: attributes}, Topology: Topology(h.Topology), Bounds: h.Bounds,
		VertexCount: h.VertexCount, IndexCount: h.IndexCount, IndexBits: h.IndexBits,
		Vertices: data[start : start+vertexBytes], Indices: data[start+vertexBytes:],
	}, nil
}

// CalculateBounds answers the box holding every vertex's position, read by
// position, over any layout and dimension. An empty slice answers the zero
// box, which Encode and canvas refuse as missing bounds.
func CalculateBounds[T any](vertices []T, position func(T) m.Vec3) m.Box3 {
	if len(vertices) == 0 {
		return m.Box3{}
	}
	first := position(vertices[0])
	bounds := m.Box3{Min: first, Max: first}
	for _, vertex := range vertices[1:] {
		point := position(vertex)
		bounds = bounds.Union(m.Box3{Min: point, Max: point})
	}
	return bounds
}
