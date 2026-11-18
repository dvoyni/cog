package meshfile_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/libs/meshfile"
)

func TestDecodeAnswersWhatEncodeWroteForEachIndexWidthAndTopology(t *testing.T) {
	widths := []struct {
		name        string
		vertexCount int
		indices     []uint32
		want        meshfile.IndexFormat
	}{
		{"no indices", 3, nil, 0},
		{"16 bits at 65,535 vertices", 1<<16 - 1, []uint32{0, 1<<16 - 2, 1}, meshfile.IndexFormatUint16},
		{"32 bits at 65,536 vertices", 1 << 16, []uint32{0, 1<<16 - 1, 1}, meshfile.IndexFormatUint32},
	}
	topologies := []meshfile.Topology{meshfile.TopologyTriangleList, meshfile.TopologyPointList, meshfile.TopologyLineList, meshfile.TopologyLineStrip, meshfile.TopologyTriangleStrip}
	bounds := m.Box3{Min: m.Vec3{X: -1, Y: -2}, Max: m.Vec3{X: 3, Y: 4}}
	layout := colorVertex{}.VertexLayout()
	for _, width := range widths {
		vertices := makeColorVertices(width.vertexCount)
		wantVertices := writeLittleEndian(t, vertices)
		var wantIndices []byte
		switch width.want {
		case meshfile.IndexFormatUint16:
			narrowed := make([]uint16, len(width.indices))
			for index, value := range width.indices {
				narrowed[index] = uint16(value)
			}
			wantIndices = writeLittleEndian(t, narrowed)
		case meshfile.IndexFormatUint32:
			wantIndices = writeLittleEndian(t, width.indices)
		}
		for _, topology := range topologies {
			t.Run(fmt.Sprintf("%s, topology %d", width.name, topology), func(t *testing.T) {
				data, err := meshfile.Encode(vertices, width.indices, topology, bounds)
				if err != nil {
					t.Fatalf("Encode: %v", err)
				}
				file, err := meshfile.Decode(data)
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}

				if file.Layout.Stride != layout.Stride || !slices.Equal(file.Layout.Attributes, layout.Attributes) {
					t.Errorf("Layout is %+v, want %+v", file.Layout, layout)
				}
				if file.Topology != topology || file.IndexFormat != width.want || file.Bounds != bounds {
					t.Errorf("Topology, IndexFormat and Bounds are %d, %d and %+v, want %d, %d and %+v", file.Topology, file.IndexFormat, file.Bounds, topology, width.want, bounds)
				}
				if file.VertexCount != uint32(len(vertices)) || file.IndexCount != uint32(len(width.indices)) {
					t.Errorf("VertexCount and IndexCount are %d and %d, want %d and %d", file.VertexCount, file.IndexCount, len(vertices), len(width.indices))
				}
				if !bytes.Equal(file.Vertices, wantVertices) {
					t.Error("Vertices are not the vertices' bytes")
				}
				if !bytes.Equal(file.Indices, wantIndices) {
					t.Errorf("Indices are % x, want % x", file.Indices, wantIndices)
				}
			})
		}
	}
}

func TestDecodeAnswersSlicesOfItsDataWithoutCopying(t *testing.T) {
	data := encodeQuad(t)
	file, err := meshfile.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	clear(data)

	if slices.ContainsFunc(file.Vertices, func(value byte) bool { return value != 0 }) || slices.ContainsFunc(file.Indices, func(value byte) bool { return value != 0 }) {
		t.Error("Vertices and Indices kept their bytes after the data was cleared, so they were copied")
	}
}

func TestEncodeWritesTheHeaderAtTheOffsetsThePackageDocumentationGives(t *testing.T) {
	bounds := m.Box3{Min: m.Vec3{X: -1, Y: -2, Z: -3}, Max: m.Vec3{X: 4, Y: 5, Z: 6}}
	data, err := meshfile.Encode(makeColorVertices(4), []uint32{0, 1, 2, 2, 1, 3}, meshfile.TopologyTriangleStrip, bounds)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if string(data[:4]) != "CMSH" {
		t.Errorf("the magic is %q, want CMSH", data[:4])
	}
	fields := []struct {
		name   string
		offset int
		want   uint32
	}{
		{"version", 4, 1},
		{"topology", 8, 4},
		{"index format", 12, 1},
		{"stride", 16, 12},
		{"attribute count", 20, 2},
		{"vertex count", 24, 4},
		{"index count", 28, 6},
		{"minimum x", 32, math.Float32bits(-1)},
		{"minimum z", 40, math.Float32bits(-3)},
		{"maximum x", 44, math.Float32bits(4)},
		{"maximum z", 52, math.Float32bits(6)},
		{"the second attribute's format", 68, uint32(meshfile.FormatUnorm8x4)},
		{"the second attribute's offset", 72, 8},
		{"the second attribute's location", 76, 1},
	}
	for _, want := range fields {
		if got := binary.LittleEndian.Uint32(data[want.offset:]); got != want.want {
			t.Errorf("the %s, at offset %d, is %#x, want %#x", want.name, want.offset, got, want.want)
		}
	}
	if len(data) != 56+2*12+4*12+6*2 {
		t.Errorf("the file is %d bytes, want %d", len(data), 56+2*12+4*12+6*2)
	}
}

func TestEncodeRefusesZeroBoundsAMismatchedStrideAndAnIndexPastTheVertices(t *testing.T) {
	bounds := m.Box3{Max: m.Vec3{X: 1, Y: 1}}
	encodings := []struct {
		name string
		err  error
		want string
	}{
		{"zero bounds", encodeError(meshfile.Encode(makeColorVertices(3), nil, meshfile.TopologyTriangleList, m.Box3{})), "bounds"},
		{"a mismatched stride", encodeError(meshfile.Encode([]misstatedVertex{{}, {}, {}}, nil, meshfile.TopologyTriangleList, bounds)), "stride"},
		{"an index past the vertices", encodeError(meshfile.Encode(makeColorVertices(3), []uint32{0, 1, 3}, meshfile.TopologyTriangleList, bounds)), "index"},
	}
	for _, encoding := range encodings {
		if encoding.err == nil || !strings.Contains(encoding.err.Error(), encoding.want) {
			t.Errorf("Encode of %s answered %v, want an error naming its %s", encoding.name, encoding.err, encoding.want)
		}
	}
}

func TestDecodeRefusesATruncatedFileAnUnknownVersionAndOtherBytes(t *testing.T) {
	data := encodeQuad(t)
	for length := range len(data) {
		if _, err := meshfile.Decode(data[:length]); err == nil || !strings.Contains(err.Error(), "cut short") {
			t.Errorf("Decode of the file's first %d of %d bytes answered %v, want it cut short", length, len(data), err)
		}
	}

	newer := slices.Clone(data)
	binary.LittleEndian.PutUint32(newer[4:], 2)
	if _, err := meshfile.Decode(newer); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Errorf("Decode of a file of version 2 answered %v, want its version refused", err)
	}

	other := slices.Clone(data)
	copy(other, "PNG!")
	if _, err := meshfile.Decode(other); err == nil || !strings.Contains(err.Error(), "not a mesh file") {
		t.Errorf("Decode of a file of other magic answered %v, want it not a mesh file", err)
	}

	if _, err := meshfile.Decode(append(slices.Clone(data), 0)); err == nil || !strings.Contains(err.Error(), "past") {
		t.Errorf("Decode of a file with a byte after its indices answered %v, want the byte refused", err)
	}
}

func TestCalculateBoundsAnswersTheBoxOfEveryPositionAndZeroForNone(t *testing.T) {
	vertices := []m.Vec2{{X: -1, Y: 2}, {X: 3, Y: -4}, {X: 0, Y: 5}}
	position := func(vertex m.Vec2) m.Vec3 { return m.Vec3{X: vertex.X, Y: vertex.Y} }

	got := meshfile.CalculateBounds(vertices, position)
	empty := meshfile.CalculateBounds(nil, position)

	want := m.Box3{Min: m.Vec3{X: -1, Y: -4}, Max: m.Vec3{X: 3, Y: 5}}
	if got != want {
		t.Errorf("CalculateBounds answered %+v, want %+v", got, want)
	}
	if empty != (m.Box3{}) {
		t.Errorf("CalculateBounds of no vertices answered %+v, want the zero box", empty)
	}
}

// colorVertex is a vertex of a position and a colour of four bytes, laid
// out as its VertexLayout says.
type colorVertex struct {
	Position m.Vec2
	Color    [4]uint8
}

// VertexLayout answers colorVertex's layout: 12 bytes, the position at
// location 0 and the colour at location 1.
func (colorVertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 12, Attributes: []meshfile.Attribute{
		{Format: meshfile.FormatFloat32x2, Offset: 0, Location: 0},
		{Format: meshfile.FormatUnorm8x4, Offset: 8, Location: 1},
	}}
}

// misstatedVertex is a vertex of 12 bytes whose layout says 16.
type misstatedVertex struct{ Position m.Vec3 }

// VertexLayout answers a stride of 16, past misstatedVertex's size.
func (misstatedVertex) VertexLayout() meshfile.Layout {
	return meshfile.Layout{Stride: 16, Attributes: []meshfile.Attribute{{Format: meshfile.FormatFloat32x3}}}
}

// makeColorVertices answers count vertices, each its own position and
// colour.
func makeColorVertices(count int) []colorVertex {
	vertices := make([]colorVertex, count)
	for index := range vertices {
		vertices[index] = colorVertex{Position: m.Vec2{X: float32(index), Y: -float32(index)}, Color: [4]uint8{uint8(index), uint8(index >> 8), 7, 255}}
	}
	return vertices
}

// encodeQuad answers the mesh file of a quad of four vertices and six 16-bit
// indices.
func encodeQuad(t *testing.T) []byte {
	t.Helper()
	data, err := meshfile.Encode(makeColorVertices(4), []uint32{0, 1, 2, 2, 1, 3}, meshfile.TopologyTriangleList, m.Box3{Max: m.Vec3{X: 3, Y: 0}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

// encodeError answers the error of an Encode call.
func encodeError(_ []byte, err error) error { return err }

// writeLittleEndian answers value's little-endian bytes, as encoding/binary
// writes them.
func writeLittleEndian(t *testing.T, value any) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("binary.Write: %v", err)
	}
	return buffer.Bytes()
}
