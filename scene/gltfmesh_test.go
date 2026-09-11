package scene

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// testDoc is an empty document with the one buffer the modeler writers append
// into. Tests build documents rather than reading files, because the vendored
// asset set lives in cog-examples and this package has no testdata.
func testDoc() *gltf.Document {
	return &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
}

// triangleAttributes writes one flat triangle's positions and returns the
// primitive attributes naming them.
func triangleAttributes(doc *gltf.Document, positions [][3]float32) gltf.PrimitiveAttributes {
	return gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)}
}

func TestConvertPrimitiveReadsPositionsAndDeclaredBounds(t *testing.T) {
	doc := testDoc()
	primitive := &gltf.Primitive{Attributes: triangleAttributes(doc, [][3]float32{
		{0, 0, 0}, {2, 0, 0}, {0, 3, 0},
	})}
	geometry, err := convertPrimitive(doc, primitive, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(geometry.vertices) != 3 {
		t.Fatalf("vertices = %d, want 3", len(geometry.vertices))
	}
	if got := geometry.vertices[2].Position; got != (m.Vec3{Y: 3}) {
		t.Errorf("position[2] = %v, want {0 3 0}", got)
	}
	if !geometry.hasBox {
		t.Fatal("WritePosition emits min/max, so the box should be declared")
	}
	if geometry.box.Max != (m.Vec3{X: 2, Y: 3}) {
		t.Errorf("box.Max = %v, want {2 3 0}", geometry.box.Max)
	}
	if geometry.topology != gfx.TopologyTriangleList {
		t.Errorf("topology = %v, want triangle list", geometry.topology)
	}
}

// A primitive with no COLOR_0 has to carry white, because the shader multiplies
// the vertex colour into base colour with no branch: the Go zero value would
// render every untinted model transparent black.
func TestConvertPrimitiveDefaultsColorToWhite(t *testing.T) {
	doc := testDoc()
	primitive := &gltf.Primitive{Attributes: triangleAttributes(doc, [][3]float32{
		{0, 0, 0}, {1, 0, 0}, {0, 1, 0},
	})}
	geometry, err := convertPrimitive(doc, primitive, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	for i, vertex := range geometry.vertices {
		if vertex.Color != m.White {
			t.Fatalf("colour[%d] = %v, want opaque white", i, vertex.Color)
		}
	}
}

// A missing POSITION min/max is what makes the whole model never-cull, so the
// flag has to survive rather than being defaulted to a zero box - a zero box
// culls the model away from every camera that is not at the origin.
func TestConvertPrimitiveReportsMissingBounds(t *testing.T) {
	doc := testDoc()
	index := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Accessors[index].Min, doc.Accessors[index].Max = nil, nil
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: index},
	}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if geometry.hasBox {
		t.Error("an accessor with no min/max must not report a box")
	}
}

// Quantised positions are an ordinary short accessor, so the dequantise path is
// the normalised component scale and nothing else.
func TestConvertPrimitiveDequantisesNormalisedShorts(t *testing.T) {
	doc := testDoc()
	index := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, [][3]int16{
		{0, 0, 0}, {32767, 0, 0}, {-32768, 16383, 0},
	})
	doc.Accessors[index].Normalized = true
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: index},
	}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := geometry.vertices[1].Position.X; got != 1 {
		t.Errorf("32767 dequantised to %v, want 1", got)
	}
	// -32768/32767 is -1.00003, and an unclamped normal of that length is a
	// visible shading error, so the read clamps.
	if got := geometry.vertices[2].Position.X; got != -1 {
		t.Errorf("-32768 dequantised to %v, want exactly -1", got)
	}
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

// glTF requires flat normals when NORMAL is absent, and a flat normal belongs
// to a face, so shared vertices have to come apart first.
func TestConvertPrimitiveUnweldsForFlatNormals(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}})
	indices := modeler.WriteIndices(doc, []uint32{0, 1, 2, 2, 1, 3})
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: positions},
		Indices:    gltf.Index(indices),
	}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(geometry.vertices) != 6 {
		t.Fatalf("vertices = %d, want six unwelded", len(geometry.vertices))
	}
	for i, vertex := range geometry.vertices {
		if vertex.Normal != (m.Vec3{Z: 1}) {
			t.Fatalf("normal[%d] = %v, want +Z", i, vertex.Normal)
		}
	}
}

// An authored NORMAL is kept as authored, welding included: unwelding every
// model would double the vertex count of every asset in the set.
func TestConvertPrimitiveKeepsAuthoredNormalsWelded(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}})
	normals := modeler.WriteNormal(doc, [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1}})
	indices := modeler.WriteIndices(doc, []uint32{0, 1, 2, 2, 1, 3})
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: positions, gltf.NORMAL: normals},
		Indices:    gltf.Index(indices),
	}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(geometry.vertices) != 4 {
		t.Fatalf("vertices = %d, want the authored four", len(geometry.vertices))
	}
}

// Tangents are generated only when a normal map will read them, and the frame
// has to follow the UV gradient rather than an arbitrary axis.
func TestGenerateTangentsFollowsTheUVGradient(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	normals := modeler.WriteNormal(doc, [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}})
	uvs := modeler.WriteTextureCoord(doc, [][2]float32{{0, 1}, {1, 1}, {0, 0}})
	primitive := &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: positions, gltf.NORMAL: normals, gltf.TEXCOORD_0: uvs,
	}}
	geometry, err := convertPrimitive(doc, primitive, true)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// U runs with +X across this triangle, so the tangent does too.
	tangent := geometry.vertices[0].Tangent
	if abs32(tangent.X-1) > 1e-5 || abs32(tangent.Y) > 1e-5 || abs32(tangent.Z) > 1e-5 {
		t.Errorf("tangent = %v, want +X", tangent)
	}
	if tangent.W != 1 && tangent.W != -1 {
		t.Errorf("tangent.w = %v, want a handedness sign", tangent.W)
	}
}

// With no UV gradient there is no tangent direction to recover, only one that
// will not produce a black or NaN frame.
func TestGenerateTangentsFallsBackToAnOrthonormalBasis(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	normals := modeler.WriteNormal(doc, [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}})
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: gltf.PrimitiveAttributes{gltf.POSITION: positions, gltf.NORMAL: normals},
	}, true)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	for i, vertex := range geometry.vertices {
		tangent := m.Vec3{X: vertex.Tangent.X, Y: vertex.Tangent.Y, Z: vertex.Tangent.Z}
		if abs32(tangent.Length()-1) > 1e-5 {
			t.Fatalf("tangent[%d] = %v, want unit length", i, tangent)
		}
		if abs32(tangent.Dot(vertex.Normal)) > 1e-5 {
			t.Fatalf("tangent[%d] = %v is not perpendicular to %v", i, tangent, vertex.Normal)
		}
	}
}

// Nothing generates tangents for a material with no normal map: the shader
// never reads the frame, so the pass would be per-vertex work multiplied by
// nothing.
func TestConvertPrimitiveSkipsTangentsWithoutANormalMap(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	normals := modeler.WriteNormal(doc, [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}})
	uvs := modeler.WriteTextureCoord(doc, [][2]float32{{0, 1}, {1, 1}, {0, 0}})
	geometry, err := convertPrimitive(doc, &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: positions, gltf.NORMAL: normals, gltf.TEXCOORD_0: uvs,
	}}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if geometry.vertices[0].Tangent != (m.Vec4{}) {
		t.Errorf("tangent = %v, want none generated", geometry.vertices[0].Tangent)
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

// The Fox is the vendored set's only non-indexed draw, and it is also the
// skinned model, so anything assuming a model mesh has indices meets it there.
func TestConvertPrimitiveKeepsNonIndexedGeometryNonIndexed(t *testing.T) {
	doc := testDoc()
	geometry, err := convertPrimitive(doc, &gltf.Primitive{
		Attributes: triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}),
		// No NORMAL, so the flat-normal path runs and would be the one place
		// tempted to index it anyway.
	}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(geometry.indices) != 3 {
		t.Fatalf("indices = %v; unwelding indexes what it expands", geometry.indices)
	}
}

func TestConvertPrimitiveReadsSkinningAttributes(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	joints := modeler.WriteJoints(doc, [][4]uint8{{0, 1, 0, 0}, {2, 0, 0, 0}, {3, 0, 0, 0}})
	weights := modeler.WriteWeights(doc, [][4]float32{{1, 0, 0, 0}, {0.5, 0.5, 0, 0}, {1, 0, 0, 0}})
	geometry, err := convertPrimitive(doc, &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: positions, gltf.JOINTS_0: joints, gltf.WEIGHTS_0: weights,
	}}, false)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// Unwelding for flat normals reorders nothing here: the geometry is one
	// triangle, so vertex i is still index i.
	if got := geometry.vertices[1].Joints; got != [4]uint16{2, 0, 0, 0} {
		t.Errorf("joints[1] = %v, want the ubyte joints widened", got)
	}
	if got := geometry.vertices[1].Weights.Y; got != 0.5 {
		t.Errorf("weights[1].y = %v, want 0.5", got)
	}
}
