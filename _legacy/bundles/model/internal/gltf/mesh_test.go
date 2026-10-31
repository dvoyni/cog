package gltf

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// testDoc is an empty document with the one buffer the modeler writers append
// into. Tests build documents rather than reading files, because the vendored
// asset set lives in cog-examples and this package has no testdata.
func testDoc() *gltf.Document {
	return &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
}

func TestConvertTopologyExpandsStripsLoopsAndFans(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     gltf.PrimitiveMode
		indices  []uint32
		topology gfx.PrimitiveTopology
		want     []uint32
	}{
		{
			name: "triangle strip flips odd triangles", mode: gltf.PrimitiveTriangleStrip,
			indices: []uint32{0, 1, 2, 3}, topology: gfx.TopologyTriangleList,
			want: []uint32{0, 1, 2, 2, 1, 3},
		},
		{
			name: "triangle fan shares the first vertex", mode: gltf.PrimitiveTriangleFan,
			indices: []uint32{0, 1, 2, 3}, topology: gfx.TopologyTriangleList,
			want: []uint32{0, 1, 2, 0, 2, 3},
		},
		{
			name: "line strip", mode: gltf.PrimitiveLineStrip,
			indices: []uint32{0, 1, 2}, topology: gfx.TopologyLineList,
			want: []uint32{0, 1, 1, 2},
		},
		{
			name: "line loop closes", mode: gltf.PrimitiveLineLoop,
			indices: []uint32{0, 1, 2}, topology: gfx.TopologyLineList,
			want: []uint32{0, 1, 1, 2, 2, 0},
		},
		{
			name: "triangles pass through", mode: gltf.PrimitiveTriangles,
			indices: []uint32{0, 1, 2}, topology: gfx.TopologyTriangleList,
			want: []uint32{0, 1, 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology, indices, err := convertTopology(test.mode, test.indices, 4)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if topology != test.topology {
				t.Errorf("topology = %v, want %v", topology, test.topology)
			}
			if len(indices) != len(test.want) {
				t.Fatalf("indices = %v, want %v", indices, test.want)
			}
			for i := range indices {
				if indices[i] != test.want[i] {
					t.Fatalf("indices = %v, want %v", indices, test.want)
				}
			}
		})
	}
}

// A non-indexed strip gains an index buffer rather than duplicated vertices,
// which is the cheaper half of the same conversion.
func TestConvertTopologyIndexesANonIndexedStrip(t *testing.T) {
	_, indices, err := convertTopology(gltf.PrimitiveTriangleStrip, nil, 4)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(indices) != 6 {
		t.Fatalf("indices = %v, want six", indices)
	}
}

func TestConvertTopologyRejectsPoints(t *testing.T) {
	if _, _, err := convertTopology(gltf.PrimitivePoints, nil, 3); !errors.Is(err, errPointTopology) {
		t.Fatalf("err = %v, want errPointTopology", err)
	}
}

// u8 indices are the WebGPU gap the loader papers over, and two of the vendored
// assets carry one.
func TestReadIndicesWidensEightBitIndices(t *testing.T) {
	doc := testDoc()
	index := modeler.WriteAccessor(doc, gltf.TargetElementArrayBuffer, []uint8{0, 1, 2})
	indices, err := readIndices(doc, &gltf.Primitive{Indices: gltf.Index(index)})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(indices) != 3 || indices[2] != 2 {
		t.Fatalf("indices = %v, want [0 1 2] as uint32", indices)
	}
}
