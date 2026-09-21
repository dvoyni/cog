package gltf

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// The seam is thin because the decoder cannot reach a GPU layout at all: it
// imports libs/m, qmuntal/gltf and slots/gfx for its enums, and nothing else of
// cog's. In particular it does not import libs/assets - it names images and
// model keys them - and it does not import model's own internal/types, where
// the records live.
func TestTheDecoderImportsOnlyItsSeam(t *testing.T) {
	allowed := map[string]bool{
		"github.com/dvoyni/cog/libs/m":                 true,
		"github.com/dvoyni/cog/slots/gfx":              true,
		"github.com/qmuntal/gltf":                      true,
		"github.com/qmuntal/gltf/modeler":              true,
		"github.com/qmuntal/gltf/ext/lightspunctual":   true,
		"github.com/qmuntal/gltf/ext/texturetransform": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, source, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if !strings.Contains(path, ".") {
				continue // the standard library
			}
			if !allowed[path] {
				t.Errorf("%s imports %s, which is not the decoder's to reach", file, path)
			}
		}
	}
}

// A float accessor crosses the seam as the slice the glTF library decoded,
// untouched: the decoder allocates nothing of its own for it. A quantised one
// is widened into a float slice of the same shape.
func TestAFloatAttributeCrossesAsTheLibrarysOwnSlice(t *testing.T) {
	doc := testDoc()
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	index := modeler.WritePosition(doc, positions)
	quantised := modeler.WriteAccessor(doc, gltf.TargetArrayBuffer, [][2]uint16{{0, 65535}, {65535, 0}, {0, 0}})
	doc.Accessors[quantised].Normalized = true
	geometry, err := readGeometry(doc, &gltf.Primitive{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: index, gltf.TEXCOORD_0: quantised,
	}}, false)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(geometry.Positions) != 3 || geometry.Positions[1] != positions[1] {
		t.Fatalf("positions = %v, want %v", geometry.Positions, positions)
	}
	if geometry.UV0[0] != [2]float32{0, 1} || geometry.UV0[1] != [2]float32{1, 0} {
		t.Errorf("uv0 = %v, want the normalised shorts widened to 0 and 1", geometry.UV0)
	}
	if geometry.Normals != nil || geometry.NormalNamed {
		t.Error("a primitive naming no NORMAL hands over no normals and says so")
	}
}

// The decoder hands the joints over in the skin's own numbering and the
// weights as the file wrote them. Remapping and normalising are the
// conversion's: they are what the one-byte storage vertex needs.
func TestSkinningAttributesCrossUnremappedAndUnnormalised(t *testing.T) {
	doc := testDoc()
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	joints := modeler.WriteJoints(doc, [][4]uint8{{1, 0, 0, 0}, {1, 0, 0, 0}, {1, 0, 0, 0}})
	weights := modeler.WriteWeights(doc, [][4]float32{{3, 1, 0, 0}, {3, 1, 0, 0}, {3, 1, 0, 0}})
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{{Attributes: gltf.PrimitiveAttributes{
		gltf.POSITION: positions, gltf.JOINTS_0: joints, gltf.WEIGHTS_0: weights,
	}}}}}
	doc.Nodes = []*gltf.Node{{Name: "a"}, {Name: "b"}, {Mesh: gltf.Index(0), Skin: gltf.Index(0)}}
	doc.Skins = []*gltf.Skin{{Joints: []int{1, 0}}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0, 1, 2}}}
	model, err := DecodeDocument(doc, "m.glb")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	geometry := model.Geometries[0]
	if geometry.Joints[0] != [4]uint16{1, 0, 0, 0} {
		t.Errorf("joints = %v, want the skin's own slot 1", geometry.Joints[0])
	}
	if geometry.Weights[0] != [4]float32{3, 1, 0, 0} {
		t.Errorf("weights = %v, want the file's own 3 and 1", geometry.Weights[0])
	}
	if geometry.Skin != 0 || !geometry.Skinned || !geometry.SkinnedLayout {
		t.Errorf("skin %d skinned %v layout %v, want skin 0 drawn skinned",
			geometry.Skin, geometry.Skinned, geometry.SkinnedLayout)
	}
	// The skin's slot 1 is node 0, which the skin claimed as the model's
	// joint 1, so the conversion remaps slot 1 to joint 1.
	if got := model.Skins[0].Joints; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("skin joints = %v, want [0 1] in the model's numbering", got)
	}
	if model.Joints[1].Node != 0 {
		t.Errorf("joint 1 follows node %d, want 0", model.Joints[1].Node)
	}
}
