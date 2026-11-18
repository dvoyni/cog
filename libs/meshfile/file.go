package meshfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// File is a decoded mesh file: its header, and its vertex and index bytes,
// slices of the bytes it was decoded from.
type File struct {
	Layout   Layout
	Topology Topology

	// IndexFormat is the type of Indices, zero when the mesh has none.
	IndexFormat IndexFormat
	Bounds      m.Box3
	VertexCount uint32
	IndexCount  uint32
	// Vertices is VertexCount vertices, Layout.Stride bytes apart.
	Vertices []byte
	// Indices is IndexCount indices of IndexFormat, little-endian.
	Indices []byte
}

// header is a mesh file's header, its fields in the file's order, as the
// package's documentation gives them; its attributes follow it.
type header struct {
	Magic          [4]byte
	Version        uint32
	Topology       Topology
	IndexFormat    IndexFormat
	Stride         uint32
	AttributeCount uint32
	VertexCount    uint32
	IndexCount     uint32
	Bounds         m.Box3
}

// The magic and the version a mesh file's header opens with.
const (
	magic   = "CMSH"
	version = 1
)

// Encode answers the mesh file of vertices, laid out as V's VertexLayout
// says, assembled into primitives by topology, within bounds. indices, which
// may be nil, are written in 16 bits while the mesh has at most 65,535
// vertices, and in 32 beyond. It refuses a vertex type whose layout's stride
// is not its size, zero bounds, which CalculateBounds answers of no
// vertices, and an index past the vertices.
func Encode[V VertexType](vertices []V, indices []uint32, topology Topology, bounds m.Box3) ([]byte, error) {
	var vertex V
	layout := vertex.VertexLayout()
	if uintptr(layout.Stride) != unsafe.Sizeof(vertex) {
		return nil, fmt.Errorf("meshfile: %T's layout has a stride of %d bytes, not its size of %d", vertex, layout.Stride, unsafe.Sizeof(vertex))
	}
	if bounds == (m.Box3{}) {
		return nil, errors.New("meshfile: the mesh's bounds are zero, and a mesh file needs them given")
	}
	for position, index := range indices {
		if uint64(index) >= uint64(len(vertices)) {
			return nil, fmt.Errorf("meshfile: index %d, at %d, is past the mesh's %d vertices", index, position, len(vertices))
		}
	}
	fileHeader := header{Version: version, Topology: topology, Stride: layout.Stride, AttributeCount: uint32(len(layout.Attributes)),
		VertexCount: uint32(len(vertices)), IndexCount: uint32(len(indices)), Bounds: bounds}
	copy(fileHeader.Magic[:], magic)
	switch {
	case len(indices) == 0:
	case len(vertices) < 1<<16:
		fileHeader.IndexFormat = IndexFormatUint16
	default:
		fileHeader.IndexFormat = IndexFormatUint32
	}
	vertexBytes := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(vertices))), len(vertices)*int(layout.Stride))

	data := make([]byte, 0, binary.Size(fileHeader)+binary.Size(layout.Attributes)+len(vertexBytes)+len(indices)*int(fileHeader.IndexFormat.size()))
	data, err := binary.Append(data, binary.LittleEndian, fileHeader)
	if err == nil {
		data, err = binary.Append(data, binary.LittleEndian, layout.Attributes)
	}
	if err != nil {
		panic(fmt.Sprintf("meshfile: a header of fixed size failed to encode: %v", err))
	}
	data = append(data, vertexBytes...)
	if fileHeader.IndexFormat == IndexFormatUint16 {
		for _, index := range indices {
			data = binary.LittleEndian.AppendUint16(data, uint16(index))
		}
	} else {
		for _, index := range indices {
			data = binary.LittleEndian.AppendUint32(data, index)
		}
	}
	return data, nil
}

// Decode answers the mesh file in data, its Vertices and Indices slices of
// data rather than copies, so data must not change while they are read. It
// refuses data that is not a mesh file, one of a version it does not know,
// and one cut short or running past the end its header gives.
func Decode(data []byte) (File, error) {
	var fileHeader header
	headerSize := binary.Size(fileHeader)
	if len(data) >= len(magic) && string(data[:len(magic)]) != magic {
		return File{}, fmt.Errorf("meshfile: not a mesh file, its first bytes are not %q", magic)
	}
	if len(data) < len(magic)+4 {
		return File{}, fmt.Errorf("meshfile: its header is cut short, at %d of %d bytes", len(data), headerSize)
	}
	if fileVersion := binary.LittleEndian.Uint32(data[len(magic):]); fileVersion != version {
		return File{}, fmt.Errorf("meshfile: it is of version %d, and only version %d is known", fileVersion, version)
	}
	if _, err := binary.Decode(data, binary.LittleEndian, &fileHeader); err != nil {
		return File{}, fmt.Errorf("meshfile: its header is cut short, at %d of %d bytes", len(data), headerSize)
	}
	if fileHeader.IndexFormat > IndexFormatUint32 || (fileHeader.IndexFormat == 0) != (fileHeader.IndexCount == 0) {
		return File{}, fmt.Errorf("meshfile: its index format is %d, for %d indices", fileHeader.IndexFormat, fileHeader.IndexCount)
	}

	// Each part is measured against what is left of data, so no size a
	// header gives overflows.
	size := uint64(len(data))
	attributesEnd := uint64(headerSize) + uint64(fileHeader.AttributeCount)*uint64(binary.Size(Attribute{}))
	verticesSize := uint64(fileHeader.Stride) * uint64(fileHeader.VertexCount)
	indicesSize := uint64(fileHeader.IndexCount) * fileHeader.IndexFormat.size()
	if attributesEnd > size || verticesSize > size-attributesEnd || indicesSize > size-attributesEnd-verticesSize {
		return File{}, fmt.Errorf("meshfile: it is cut short, at %d bytes, before the %d attributes, %d vertices and %d indices its header gives", size, fileHeader.AttributeCount, fileHeader.VertexCount, fileHeader.IndexCount)
	}
	verticesEnd := attributesEnd + verticesSize
	end := verticesEnd + indicesSize
	if size > end {
		return File{}, fmt.Errorf("meshfile: it holds %d bytes past the end its header gives", size-end)
	}
	attributes := make([]Attribute, fileHeader.AttributeCount)
	if _, err := binary.Decode(data[headerSize:attributesEnd], binary.LittleEndian, attributes); err != nil {
		panic(fmt.Sprintf("meshfile: attributes measured whole failed to decode: %v", err))
	}
	return File{
		Layout:      Layout{Stride: fileHeader.Stride, Attributes: attributes},
		Topology:    fileHeader.Topology,
		IndexFormat: fileHeader.IndexFormat,
		Bounds:      fileHeader.Bounds,
		VertexCount: fileHeader.VertexCount,
		IndexCount:  fileHeader.IndexCount,
		Vertices:    data[attributesEnd:verticesEnd:verticesEnd],
		Indices:     data[verticesEnd:end:end],
	}, nil
}

// CalculateBounds answers the box holding the position of every one of
// vertices, which position reads from a vertex, over any layout and
// dimension: a 2D layout answers its positions with a zero z. No vertices
// answer the zero box, which Encode refuses as bounds not given.
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

// Topology is how a mesh's vertices are assembled into primitives, each
// WebGPU's primitive topology of the same name, and its value that of gfx's
// and render's. Its zero value is a triangle list.
type Topology uint32

const (
	TopologyTriangleList  Topology = 0x0
	TopologyPointList     Topology = 0x1
	TopologyLineList      Topology = 0x2
	TopologyLineStrip     Topology = 0x3
	TopologyTriangleStrip Topology = 0x4
)
