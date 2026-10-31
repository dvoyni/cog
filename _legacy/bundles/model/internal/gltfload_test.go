package internal

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/ext/lightspunctual"
	"github.com/qmuntal/gltf/ext/texturetransform"
	"github.com/qmuntal/gltf/modeler"
)

// triangleMesh adds one flat triangle as a mesh and returns its index.
func triangleMesh(doc *gltf.Document, material *int) int {
	attributes := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       "triangle",
		Primitives: []*gltf.Primitive{{Attributes: attributes, Material: material}},
	})
	return len(doc.Meshes) - 1
}

// sceneOf wires a node list into the document's one scene.
func sceneOf(doc *gltf.Document, roots ...int) {
	doc.Scenes = append(doc.Scenes, &gltf.Scene{Name: "scene", Nodes: roots})
}

func TestConvertDocumentFlattensDepthFirst(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "root", Children: []int{1, 3}, Translation: [3]float64{10, 0, 0}},
		{Name: "child", Children: []int{2}, Translation: [3]float64{0, 5, 0}, Mesh: gltf.Index(mesh)},
		{Name: "grandchild", Translation: [3]float64{0, 0, 2}, Mesh: gltf.Index(mesh)},
		{Name: "sibling", Mesh: gltf.Index(mesh)},
	}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.primitives) != 3 {
		t.Fatalf("primitives = %d, want three", len(model.primitives))
	}
	// Depth-first is what makes a subtree a contiguous slice rather than a
	// filter: child and its grandchild come before the sibling.
	want := []m.Vec3{{X: 10, Y: 5}, {X: 10, Y: 5, Z: 2}, {X: 10}}
	for i, expected := range want {
		if got := model.primitives[i].local.Translation(); got != expected {
			t.Errorf("primitive %d sits at %v, want %v", i, got, expected)
		}
	}
}

// A negative determinant mirrors the geometry, which reverses winding. Pipeline
// state is per material and the draw gets no say, so the winding has to be
// answered with a material variant.
func TestConvertDocumentMakesAFrontCWVariantForMirroredNodes(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "plain", Mesh: gltf.Index(mesh)},
		{Name: "mirrored", Mesh: gltf.Index(mesh), Scale: [3]float64{-1, 1, 1}},
	}
	sceneOf(doc, 0, 1)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.materials) != 2 {
		t.Fatalf("materials = %d; one glTF material under two windings is two variants",
			len(model.materials))
	}
	plain := model.materials[model.primitives[0].material]
	mirrored := model.materials[model.primitives[1].material]
	if plain.state.FrontFace != gfx.FrontCCW {
		t.Errorf("the unmirrored node is %v, want FrontCCW", plain.state.FrontFace)
	}
	if mirrored.state.FrontFace != gfx.FrontCW {
		t.Errorf("the mirrored node is %v, want FrontCW", mirrored.state.FrontFace)
	}
}

// Two nodes with the same winding share one converted material, so a model with
// one glTF material costs one record however many nodes reference it.
func TestConvertDocumentSharesOneMaterialAcrossNodes(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Mesh: gltf.Index(mesh)},
		{Mesh: gltf.Index(mesh), Translation: [3]float64{3, 0, 0}},
	}
	sceneOf(doc, 0, 1)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.materials) != 1 {
		t.Fatalf("materials = %d, want one shared", len(model.materials))
	}
}

// A skinned node's own transform is ignored per the glTF specification: its
// joints resolve against the scene root, so applying the node's matrix as well
// would apply it twice.
func TestConvertDocumentGivesSkinnedPrimitivesAnIdentityLocal(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Skins = []*gltf.Skin{{Joints: []int{1}}}
	doc.Nodes = []*gltf.Node{
		{Name: "skinned", Mesh: gltf.Index(mesh), Skin: gltf.Index(0),
			Translation: [3]float64{7, 8, 9}, Children: []int{1}},
		{Name: "joint", Translation: [3]float64{1, 0, 0}},
	}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := model.primitives[0].local; got != m.NewMat4() {
		t.Errorf("skinned local = %v, want the identity", got)
	}
}

// The hierarchy is a hierarchy whether or not a node is skinned, so descendants
// still inherit the skinned node's transform.
func TestConvertDocumentKeepsTheHierarchyThroughASkinnedNode(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Skins = []*gltf.Skin{{Joints: []int{1}}}
	doc.Nodes = []*gltf.Node{
		{Mesh: gltf.Index(mesh), Skin: gltf.Index(0),
			Translation: [3]float64{7, 0, 0}, Children: []int{1}},
		{Mesh: gltf.Index(mesh), Translation: [3]float64{0, 2, 0}},
	}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got := model.primitives[1].local.Translation(); got != (m.Vec3{X: 7, Y: 2}) {
		t.Errorf("child sits at %v, want {7 2 0}", got)
	}
}

// A required extension scene has no decoder for fails the model wholesale:
// there is no geometry to fall back to, and a wrongly drawn model is exactly
// what a report cannot make visible.
func TestConvertDocumentRejectsARequiredExtension(t *testing.T) {
	doc := testDoc()
	sceneOf(doc)
	doc.ExtensionsRequired = []string{"KHR_draco_mesh_compression"}
	if _, err := convertDocument(doc, "m.glb", testSampleRate); err == nil {
		t.Fatal("a required Draco extension must fail the model")
	}
}

// KHR_mesh_quantization is required by one of the vendored assets and supported
// here, so requiring it must not fail the model.
func TestConvertDocumentAcceptsASupportedRequiredExtension(t *testing.T) {
	doc := testDoc()
	triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.ExtensionsRequired = []string{"KHR_mesh_quantization"}
	if _, err := convertDocument(doc, "m.glb", testSampleRate); err != nil {
		t.Fatalf("convert: %v", err)
	}
}

// "Ignored" and "rejected" are one if apart, and one vendored asset uses
// KHR_materials_unlit without requiring it.
func TestConvertDocumentIgnoresAnUnknownExtensionInUse(t *testing.T) {
	doc := testDoc()
	triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.ExtensionsUsed = []string{"KHR_materials_unlit"}
	if _, err := convertDocument(doc, "m.glb", testSampleRate); err != nil {
		t.Fatalf("an extensionsUsed entry must be ignored, not rejected: %v", err)
	}
}

func TestConvertDocumentReadsTheMetallicRoughnessSet(t *testing.T) {
	doc := testDoc()
	doc.Materials = []*gltf.Material{{
		Name:        "painted",
		AlphaMode:   gltf.AlphaMask,
		AlphaCutoff: gltf.Float(0.25),
		DoubleSided: true,
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{0.2, 0.4, 0.6, 0.8},
			MetallicFactor:  gltf.Float(0.25),
			RoughnessFactor: gltf.Float(0.75),
		},
		EmissiveFactor: [3]float64{0.1, 0.2, 0.3},
	}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	material := model.materials[0]
	if material.values.baseColorFactor != (m.Vec4{X: 0.2, Y: 0.4, Z: 0.6, W: 0.8}) {
		t.Errorf("baseColorFactor = %v", material.values.baseColorFactor)
	}
	if material.values.metallicFactor != 0.25 || material.values.roughnessFactor != 0.75 {
		t.Errorf("metallic/roughness = %v/%v, want 0.25/0.75",
			material.values.metallicFactor, material.values.roughnessFactor)
	}
	// MASK is fixed-function-identical to OPAQUE - the cutoff is entirely a
	// fragment-shader concern - so it must land in the opaque sort class.
	if material.state.Blend != gfx.BlendOpaque {
		t.Errorf("alphaMode MASK blends %v, want opaque state plus a shader discard", material.state.Blend)
	}
	if material.values.alphaCutoff != 0.25 {
		t.Errorf("alphaCutoff = %v, want 0.25", material.values.alphaCutoff)
	}
	if material.state.Cull != gfx.CullNone {
		t.Errorf("doubleSided culls %v, want CullNone", material.state.Cull)
	}
	if material.values.emissiveFactor.Z != 0.3 {
		t.Errorf("emissiveFactor = %v", material.values.emissiveFactor)
	}
}

// An OPAQUE material's cutoff is zero, which makes the shader's unconditional
// discard a no-op: alpha is never below zero.
func TestConvertDocumentLeavesAnOpaqueCutoffAtZero(t *testing.T) {
	doc := testDoc()
	doc.Materials = []*gltf.Material{{AlphaCutoff: gltf.Float(0.5)}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if model.materials[0].values.alphaCutoff != 0 {
		t.Errorf("cutoff = %v; only MASK carries one", model.materials[0].values.alphaCutoff)
	}
}

func TestConvertDocumentBlendsATransparentMaterial(t *testing.T) {
	doc := testDoc()
	doc.Materials = []*gltf.Material{{AlphaMode: gltf.AlphaBlend}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if model.materials[0].state.Blend == gfx.BlendOpaque {
		t.Error("alphaMode BLEND must reach the blend sort class")
	}
}

// Folding the strength into emissiveFactor at load is what keeps the record's
// numbers glTF's own, and the clamp is the honest limit of an 8-bit target with
// no tonemapping anywhere.
func TestConvertDocumentFoldsAndClampsEmissiveStrength(t *testing.T) {
	doc := testDoc()
	payload, _ := json.Marshal(map[string]float64{"emissiveStrength": 4})
	doc.Materials = []*gltf.Material{{
		EmissiveFactor: [3]float64{0.1, 0.5, 1},
		Extensions:     gltf.Extensions{"KHR_materials_emissive_strength": json.RawMessage(payload)},
	}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	emissive := model.materials[0].values.emissiveFactor
	if emissive.X != 0.4 {
		t.Errorf("emissive.x = %v, want 0.1 * 4", emissive.X)
	}
	if emissive.Y != 1 || emissive.Z != 1 {
		t.Errorf("emissive = %v; 0.5*4 and 1*4 both clamp at 1", emissive)
	}
}

func TestConvertDocumentReadsTextureTransformAndUVSet(t *testing.T) {
	doc := testDoc()
	doc.Images = []*gltf.Image{{URI: "colour.png"}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0, Extensions: gltf.Extensions{
			texturetransform.ExtensionName: &texturetransform.TextureTranform{
				Offset: [2]float64{0.25, 0.5}, Scale: [2]float64{2, 4},
				Rotation: 1.5, TexCoord: gltf.Index(1),
			},
		}},
	}}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	record := model.materials[0].values
	if got := record.transforms[baseColorSlot]; got != (m.Vec4{X: 0.25, Y: 0.5, Z: 2, W: 4}) {
		t.Errorf("baseColorTransform = %v, want offset.xy then scale.xy", got)
	}
	if record.rotations[baseColorSlot] != 1.5 {
		t.Errorf("baseColorRotation = %v", record.rotations[baseColorSlot])
	}
	// The transform's own texCoord overrides the textureInfo's, and set 1 is
	// one bit in the packed selector.
	if record.uvSets&(1<<baseColorSlot) == 0 {
		t.Errorf("uvSets = %b, want baseColor on TEXCOORD_1", record.uvSets)
	}
}

// Scene carries two UV sets, glTF core's minimum. A slot naming a third falls
// back to set 0 and says so: silently ignoring texCoord: 2 would be a wrong
// picture on a core feature with nothing to explain it.
func TestConvertDocumentReportsAThirdUVSet(t *testing.T) {
	doc := testDoc()
	doc.Images = []*gltf.Image{{URI: "colour.png"}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0, TexCoord: 2},
	}}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	found := false
	for _, report := range model.reports {
		if _, ok := report.(ErrTextureUVSetUnsupported); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("reports = %v, want an unsupported-UV-set report", model.reports)
	}
	if model.materials[0].values.uvSets&(1<<baseColorSlot) != 0 {
		t.Error("the slot must fall back to TEXCOORD_0")
	}
}

// An external image is named here and read by the Library when the texture
// cache misses it, so the parse opens nothing and reports nothing - which is
// what stops two models sharing one picture from both decoding it.
func TestConvertDocumentNamesAnExternalImageAndReadsNothing(t *testing.T) {
	doc := testDoc()
	doc.Images = []*gltf.Image{{URI: "pictures/absent.png"}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "models/m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("a missing texture must not fail the model: %v", err)
	}
	if len(model.reports) != 0 {
		t.Fatalf("reports = %v, want the read and its failure left to the Library", model.reports)
	}
	slot := model.materials[0].slots[baseColorSlot]
	if slot < 0 || slot >= len(model.textures) {
		t.Fatalf("slot = %d, want the image named among the model's %d requests",
			slot, len(model.textures))
	}
	descr := model.textures[slot]
	// The URI is relative to the model's own directory, and the picture has a
	// path of its own, so it is named by that path with no bytes beside it.
	if descr.Name != "models/pictures/absent.png" {
		t.Errorf("name = %q, want the URI resolved against the model's directory", descr.Name)
	}
	if descr.Params.image != externalImage {
		t.Errorf("image = %d, want the path to be the whole identity", descr.Params.image)
	}
	if !descr.Params.srgb {
		t.Error("a base colour is a picture and is read in sRGB")
	}
	if descr.Blob.Len() != 0 {
		t.Error("a picture with a path of its own carries no payload; the Library opens it")
	}
}

// An image the file says it embeds and then holds no bytes for is the parse's
// own fault, because no descriptor can be formed for it at all: the slot falls
// back to its 1x1 default and the model still loads.
func TestConvertDocumentReportsAnUnreadableEmbeddedImageAndKeepsTheModel(t *testing.T) {
	doc := testDoc()
	doc.Images = []*gltf.Image{{BufferView: gltf.Index(99)}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("a missing texture must not fail the model: %v", err)
	}
	if len(model.primitives) != 1 {
		t.Fatalf("primitives = %d, want the model still resident", len(model.primitives))
	}
	if model.materials[0].slots[baseColorSlot] != missingTexture {
		t.Error("the slot must fall back to the default texel")
	}
	if len(model.reports) != 1 {
		t.Fatalf("reports = %v, want exactly one", model.reports)
	}
}

// A POINTS primitive is skipped and the rest of the model loads: a mesh that is
// mostly triangles should not be lost to one point cloud.
func TestConvertDocumentSkipsAPointsPrimitiveAndKeepsTheRest(t *testing.T) {
	doc := testDoc()
	points := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}})
	triangles := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Meshes = []*gltf.Mesh{{Name: "modes", Primitives: []*gltf.Primitive{
		{Attributes: points, Mode: gltf.PrimitivePoints},
		{Attributes: triangles},
	}}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.primitives) != 1 {
		t.Fatalf("primitives = %d, want the triangle alone", len(model.primitives))
	}
	if len(model.reports) != 1 {
		t.Fatalf("reports = %v, want one skipped primitive", model.reports)
	}
}

// A primitive whose accessor carries no min/max makes the whole model
// never-cull, reported once: a model with one unbounded primitive has no bound
// at all, and culling the rest would leave the unbounded piece drawn alone.
func TestConvertDocumentMakesAnUnboundedModelNeverCull(t *testing.T) {
	doc := testDoc()
	index := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Accessors[index].Min, doc.Accessors[index].Max = nil, nil
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{
		{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: index}},
		{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: index}},
	}}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !model.neverCull {
		t.Error("a primitive with no declared bounds must make the model never-cull")
	}
	if len(model.reports) != 1 {
		t.Fatalf("reports = %v, want one however many primitives are unbounded", model.reports)
	}
}

// Lights are parsed and exposed as data. Nothing converts one: a file's lights
// are authored for the file, not for the scene it is dropped into.
func TestConvertDocumentExposesPunctualLightsAsData(t *testing.T) {
	doc := testDoc()
	doc.Extensions = gltf.Extensions{lightspunctual.ExtensionName: lightspunctual.Lights{
		{Type: lightspunctual.TypePoint, Name: "bulb",
			Color: &[3]float64{1, 0.5, 0.25}, Intensity: gltf.Float(3), Range: gltf.Float(9)},
		{Type: lightspunctual.TypeSpot, Name: "lamp",
			Spot: &lightspunctual.Spot{InnerConeAngle: 0.1, OuterConeAngle: gltf.Float(0.4)}},
		{Type: lightspunctual.TypeDirectional, Name: "sun"},
	}}
	doc.Nodes = []*gltf.Node{
		{Translation: [3]float64{1, 2, 3}, Extensions: gltf.Extensions{
			lightspunctual.ExtensionName: lightspunctual.LightIndex(0)}},
		{Extensions: gltf.Extensions{
			lightspunctual.ExtensionName: lightspunctual.LightIndex(1)}},
		{Extensions: gltf.Extensions{
			lightspunctual.ExtensionName: lightspunctual.LightIndex(2)}},
	}
	sceneOf(doc, 0, 1, 2)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.lights) != 3 {
		t.Fatalf("lights = %d, want three", len(model.lights))
	}
	bulb := model.lights[0]
	if bulb.Name != "bulb" || bulb.Descr.Position != (m.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Errorf("point light = %+v, want it at its node", bulb)
	}
	if bulb.Descr.Intensity != 3 || bulb.Descr.Range != 9 {
		t.Errorf("point light = %+v, want intensity 3 and range 9", bulb.Descr)
	}
	lamp := model.lights[1]
	if lamp.Descr.Kind != LightSpot || lamp.Descr.OuterCone != 0.4 {
		t.Errorf("spot light = %+v, want a cone", lamp.Descr)
	}
	// glTF punctual lights point down their node's local -Z, which is also the
	// direction scene's spot takes.
	if got := lamp.Descr.Direction; got != (m.Vec3{Z: -1}) {
		t.Errorf("spot direction = %v, want -Z", got)
	}
	if !model.lights[2].Directional {
		t.Error("a directional light must be marked as one; scene has no call for it")
	}
}

// namedSceneOf appends one named scene, so a Scene selector has something to
// match. sceneOf is the single-scene shorthand every other test uses.
func namedSceneOf(doc *gltf.Document, name string, roots ...int) {
	doc.Scenes = append(doc.Scenes, &gltf.Scene{Name: name, Nodes: roots})
}

// animate adds a one-channel animation targeting a node's rotation, which is
// all the recording of animated ancestors reads: a node an animation steers has
// no fixed authored world transform, and neither has any descendant of it.
func animate(doc *gltf.Document, node int) {
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Channels: []*gltf.AnimationChannel{{
			Target: gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: gltf.TRSRotation},
		}},
	})
}

// Every scene in the file is flattened, not just the default one, because path
// is a model's only cache key: a draw naming a scene cannot trigger a second
// load of the same file.
func TestConvertDocumentFlattensEveryScene(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "a", Mesh: gltf.Index(mesh)},
		{Name: "b", Mesh: gltf.Index(mesh)},
		{Name: "c", Mesh: gltf.Index(mesh)},
	}
	namedSceneOf(doc, "first", 0)
	namedSceneOf(doc, "second", 1, 2)
	doc.Scene = gltf.Index(1)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.scenes) != 2 {
		t.Fatalf("scenes = %d, want the file's two", len(model.scenes))
	}
	if model.defaultScene != 1 {
		t.Errorf("default scene = %d, want the declared 1", model.defaultScene)
	}
	// A scene is a contiguous range for the same reason a subtree is: the
	// flatten walks one scene to completion before it starts the next.
	if got := model.scenes[0]; got.Name != "first" || got.Start != 0 || got.End != 1 {
		t.Errorf("scene 0 spans [%d,%d) as %q, want first over [0,1)", got.Start, got.End, got.Name)
	}
	if got := model.scenes[1]; got.Name != "second" || got.Start != 1 || got.End != 3 {
		t.Errorf("scene 1 spans [%d,%d) as %q, want second over [1,3)", got.Start, got.End, got.Name)
	}
}

// A node walked in two scenes is flattened in both. The cycle guard is per
// scene, not per file, or the second scene would come out empty.
func TestConvertDocumentFlattensASharedNodeInEverySceneThatRootsIt(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "shared", Mesh: gltf.Index(mesh)}}
	namedSceneOf(doc, "first", 0)
	namedSceneOf(doc, "second", 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(model.primitives) != 2 {
		t.Fatalf("primitives = %d, want one per scene", len(model.primitives))
	}
	for i, scene := range model.scenes {
		if _, ok := scene.Nodes["shared"]; !ok {
			t.Errorf("scene %d does not address the shared node", i)
		}
	}
}

// Depth-first order is what makes a subtree a slice rather than a filter, so a
// named node records the half-open range its own primitives and its
// descendants' occupy.
func TestConvertDocumentRecordsANamedNodesSubtreeAsASlice(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "root", Children: []int{1, 3}},
		{Name: "child", Children: []int{2}, Mesh: gltf.Index(mesh)},
		{Name: "grandchild", Mesh: gltf.Index(mesh)},
		{Name: "sibling", Mesh: gltf.Index(mesh)},
	}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	nodes := model.scenes[0].Nodes
	for name, want := range map[string][2]int{
		"root": {0, 3}, "child": {0, 2}, "grandchild": {1, 2}, "sibling": {2, 3},
	} {
		node, ok := nodes[name]
		if !ok {
			t.Fatalf("node %q is not addressable", name)
		}
		if node.Start != want[0] || node.End != want[1] {
			t.Errorf("node %q covers [%d,%d), want [%d,%d)",
				name, node.Start, node.End, want[0], want[1])
		}
	}
}

// Re-rooting discards the node's authored world transform, so the matrix
// recorded for it is that transform's inverse: composed with the world the
// flatten baked into the node's own primitives, it is the identity.
func TestConvertDocumentRecordsTheInverseOfANodesAuthoredWorld(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "root", Children: []int{1}, Translation: [3]float64{4, 0, 0}},
		{
			Name: "crate", Mesh: gltf.Index(mesh),
			Translation: [3]float64{0, 3, 0}, Scale: [3]float64{2, 2, 2},
		},
	}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	node, ok := model.scenes[0].Nodes["crate"]
	if !ok || !node.Rerootable {
		t.Fatalf("crate = %+v, want a rerootable node", node)
	}
	if rerooted := node.Reroot.Mul(model.primitives[node.Start].local); !nearlyIdentity(rerooted) {
		t.Errorf("re-rooting the crate leaves %v, want the identity", rerooted)
	}
}

// A node whose authored world transform collapses an axis cannot be inverted,
// so it is recorded as unrerootable rather than as a matrix that is quietly
// wrong. The draw reports it, because a whole-scene draw of the same file is
// unaffected.
func TestConvertDocumentMarksACollapsedNodeUnrerootable(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "flat", Mesh: gltf.Index(mesh), Scale: [3]float64{1, 0, 1}}}
	sceneOf(doc, 0)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if node := model.scenes[0].Nodes["flat"]; node.Rerootable {
		t.Errorf("flat covers [%d,%d) and claims to reroot; want it marked unrerootable",
			node.Start, node.End)
	}
}

// A duplicate name keeps the first match, which is what "first depth-first
// match" means, and says so once.
func TestConvertDocumentKeepsTheFirstOfADuplicateNodeNameAndReports(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "crate", Mesh: gltf.Index(mesh), Translation: [3]float64{1, 0, 0}},
		{Name: "crate", Mesh: gltf.Index(mesh), Translation: [3]float64{2, 0, 0}},
	}
	sceneOf(doc, 0, 1)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if node := model.scenes[0].Nodes["crate"]; node.Start != 0 {
		t.Errorf("crate covers [%d,%d), want the first match's [0,1)", node.Start, node.End)
	}
	duplicates := 0
	for _, err := range model.reports {
		var duplicate ErrModelNodeDuplicated
		if errors.As(err, &duplicate) {
			duplicates++
			if duplicate.Node != "crate" {
				t.Errorf("report names %q, want crate", duplicate.Node)
			}
		}
	}
	if duplicates != 1 {
		t.Errorf("a duplicated name reported %d times, want once", duplicates)
	}
}

// The chain recorded is of animated *ancestors*. A node animated in its own
// right keeps that animation - re-rooting replaces where a node sits, not what
// it does - so it is not its own ancestor, and an unanimated hierarchy records
// nothing at all.
func TestConvertDocumentRecordsAnimatedAncestorsAndNotTheNodeItself(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "turntable", Children: []int{1}},
		{Name: "arm", Children: []int{2}},
		{Name: "crate", Mesh: gltf.Index(mesh)},
		{Name: "still", Mesh: gltf.Index(mesh)},
	}
	sceneOf(doc, 0, 3)
	animate(doc, 0)
	animate(doc, 2)
	model, err := convertDocument(doc, "m.glb", testSampleRate)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	nodes := model.scenes[0].Nodes
	if got := nodes["crate"].Animated; len(got) != 1 || got[0] != 0 {
		t.Errorf("the crate's animated ancestors = %v, want the turntable alone", got)
	}
	if got := nodes["still"].Animated; len(got) != 0 {
		t.Errorf("an unanimated hierarchy recorded %v, want nothing", got)
	}
	if got := nodes["turntable"].Animated; len(got) != 0 {
		t.Errorf("the turntable is recorded as its own ancestor: %v", got)
	}
}

// nearlyIdentity reports whether a matrix is the identity to single-precision
// tolerance, which composing an inverse with its original is.
func nearlyIdentity(matrix m.Mat4) bool {
	identity := m.NewMat4()
	for i := range matrix {
		if abs32(matrix[i]-identity[i]) > 1e-4 {
			return false
		}
	}
	return true
}
