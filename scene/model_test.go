package scene

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

const modelPath = "models/prop.glb"

// glb encodes a document as a single-buffer GLB, which is what every asset in
// the vendored set is.
func glb(t testing.TB, doc *gltf.Document) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := gltf.NewEncoder(&buffer).Encode(doc); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buffer.Bytes()
}

// onePrimitiveModel is the smallest drawable file: one triangle, one node, one
// scene, no material.
func onePrimitiveModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Name: "prop", Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	return doc
}

// modelFiles wraps one encoded document as the filesystem a harness mounts.
func modelFiles(data []byte) fstest.MapFS {
	return fstest.MapFS{modelPath: &fstest.MapFile{Data: data}}
}

// modelCamera stands well back from the origin, so that a model test asserts
// what the packer did rather than where the shared test camera happens to point.
func modelCamera() CameraDescr {
	return CameraDescr{
		Transform: LookAt(m.Vec3{Z: 30}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      1.0472,
		Near:      0.1, Far: 200,
	}
}

// drawModel is the recorder every model test uses: one camera looking at the
// origin and one model draw.
func drawModel(path string, draw ModelDraw) func(*OpQueue) {
	return func(q *OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(LayersAll, path, draw)
	}
}

// A non-resident model is skipped, never substituted: the first frame draws
// nothing at all, and residency lands some frames later.
func TestAModelDrawsNothingUntilItIsResident(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, onePrimitiveModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frame()
	if passes := h.passes(); len(passes) != 1 || passes[0].Instances != 0 {
		t.Fatalf("first frame packed %v, want nothing while the load is in flight", passes)
	}
	h.frameUntil(t, "the model to become resident", func() bool {
		passes := h.passes()
		return len(passes) == 1 && passes[0].Instances == 1
	})
	passes := h.passes()
	if len(passes[0].Batches) != 1 {
		t.Fatalf("batches = %v, want one per primitive", passes[0].Batches)
	}
	if passes[0].Batches[0].MeshID == 0 {
		t.Error("a model primitive must carry a real mesh id, which is the sort key")
	}
}

// Ops reports the call as the recorder made it, not the draws scene derived
// from it: a model is one op whatever it expands to.
func TestAModelCallIsOneOp(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(mesh)}, {Mesh: gltf.Index(mesh)}}
	sceneOf(doc, 0, 1)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)),
		drawModel(modelPath, ModelDraw{Transform: At(1, 2, 3)}))
	h.frameUntil(t, "the model to become resident", func() bool {
		passes := h.passes()
		return len(passes) == 1 && passes[0].Instances == 2
	})
	ops := h.ops()
	models := 0
	for _, op := range ops {
		if op.Kind == OpModel {
			models++
			if op.Path != modelPath {
				t.Errorf("op path = %q, want %q", op.Path, modelPath)
			}
			if op.Model.Transform.Position != (m.Vec3{X: 1, Y: 2, Z: 3}) {
				t.Errorf("op transform = %v", op.Model.Transform.Position)
			}
		}
	}
	if models != 1 {
		t.Fatalf("ops = %v, want exactly one OpModel for a two-primitive model", ops)
	}
}

// One draw per primitive, so a multi-node model is a batch per node and every
// primitive keeps its own flattened place in the file.
func TestAResidentModelDrawsOneBatchPerPrimitive(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Mesh: gltf.Index(mesh)},
		{Mesh: gltf.Index(mesh), Translation: [3]float64{0, 1, 0}},
		{Mesh: gltf.Index(mesh), Translation: [3]float64{0, 2, 0}},
	}
	sceneOf(doc, 0, 1, 2)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 3
	})
	batches := h.passes()[0].Batches
	if len(batches) != 3 {
		t.Fatalf("batches = %v, want one per primitive", batches)
	}
	// Three nodes sharing one mesh and one material share a sort key, so they
	// are three separate single-instance batches rather than one run: only an
	// instanced call collapses, and collapsing consecutive equal draws is the
	// deferred automatic optimisation.
	for i, batch := range batches {
		if batch.InstanceCount != 1 {
			t.Errorf("batch %d holds %d instances, want one", i, batch.InstanceCount)
		}
	}
}

// Instancing is per primitive: a two-primitive model at three transforms is two
// batches of three, not six draw calls.
func TestModelInstancingPacksEachPrimitiveAsOneBatch(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(mesh)}, {Mesh: gltf.Index(mesh), Translation: [3]float64{0, 1, 0}}}
	sceneOf(doc, 0, 1)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{
		Transforms: []Transform{At(0, 0, 0), At(2, 0, 0), At(4, 0, 0)},
	}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 6
	})
	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("batches = %v, want one per primitive", batches)
	}
	for i, batch := range batches {
		if batch.InstanceCount != 3 {
			t.Fatalf("batch %d holds %d instances, want the call's three", i, batch.InstanceCount)
		}
	}
}

// A draw's own Transform multiplies the primitive's flattened localMatrix, so a
// node authored away from the origin keeps its offset when the model is placed.
func TestAModelDrawFoldsTheDrawTransformOverTheFlattenedMatrix(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(mesh), Translation: [3]float64{0, 3, 0}}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)),
		drawModel(modelPath, ModelDraw{Transform: At(10, 0, 0)}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	// The pass's frustum is the assertion surface with no GPU: the draw's world
	// sphere has to sit where the two matrices put it, which is what the cull
	// tests it against.
	var world m.Sphere
	h.inspect(func(q *OpQueue) {
		draws := q.flushDraws()
		if len(draws) != 1 {
			t.Fatalf("draws = %d, want the one expanded primitive", len(draws))
		}
		world = prepareDraw(draws[0], meshRecord{}).sphere
	})
	// The triangle's declared box is (0,0,0)..(1,1,0), so its sphere sits at
	// (0.5, 0.5, 0) before the node's 3 on Y and the draw's 10 on X.
	if got := world.Center; abs32(got.X-10.5) > 1e-4 || abs32(got.Y-3.5) > 1e-4 {
		t.Errorf("the primitive sits at %v, want {10.5 3.5 0}", got)
	}
}

// A path that does not exist fails wholesale and never retries: a typo must not
// spawn a load command every frame forever.
func TestAMissingModelReportsOnceAndNeverRetries(t *testing.T) {
	h := newHarnessWithFiles(t, fstest.MapFS{}, drawModel("models/absent.glb", ModelDraw{}))
	h.frameUntil(t, "the failure to be reported", func() bool {
		var unavailable ErrModelUnavailable
		return anyErrorAs(h.errors(), &unavailable)
	})
	for i := 0; i < 20; i++ {
		h.frame()
	}
	reports := 0
	for _, err := range h.errors() {
		var unavailable ErrModelUnavailable
		if errors.As(err, &unavailable) {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("a missing model reported %d times over 20-odd frames, want once", reports)
	}
}

// A required extension has no geometry to fall back to, so the model fails
// wholesale rather than binding a default.
func TestARequiredExtensionFailsTheModelWholesale(t *testing.T) {
	doc := onePrimitiveModel(t)
	doc.ExtensionsRequired = []string{"KHR_draco_mesh_compression"}
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the failure to be reported", func() bool {
		var unavailable ErrModelUnavailable
		return anyErrorAs(h.errors(), &unavailable)
	})
	if passes := h.passes(); passes[0].Instances != 0 {
		t.Fatalf("a failed model packed %d instances, want none", passes[0].Instances)
	}
}

// A model that parses but is missing a texture still becomes resident, with the
// slot bound to the 1x1 white texel.
func TestAModelMissingATextureStaysResident(t *testing.T) {
	doc := onePrimitiveModel(t)
	doc.Images = []*gltf.Image{{URI: "absent.png"}}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(0)}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	var missing ErrModelTextureUnavailable
	if !anyErrorAs(h.errors(), &missing) {
		t.Fatalf("errors = %v, want a texture report", h.errors())
	}
	// All five slots and all five samplers bound, because WGSL requires every
	// declared binding bound and gfx does no preprocessing.
	for _, slot := range pbrSlots {
		if len(h.backend.texturesBoundTo(slot.texture)) == 0 {
			t.Errorf("%s was never bound", slot.texture)
		}
		if len(h.backend.samplersBoundTo(slot.sampler)) == 0 {
			t.Errorf("%s was never bound", slot.sampler)
		}
	}
}

// An embedded image reaches the GPU as its own texture, so the model's base
// colour is the file's picture rather than the 1x1 default every empty slot
// binds.
func TestAModelBindsItsEmbeddedTexture(t *testing.T) {
	doc := onePrimitiveModel(t)
	index, err := modeler.WriteImage(doc, "colour", "image/png", bytes.NewReader(onePixelPNG(t)))
	if err != nil {
		t.Fatalf("write image: %v", err)
	}
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(index)}}
	doc.Samplers = []*gltf.Sampler{{WrapS: gltf.WrapMirroredRepeat, WrapT: gltf.WrapClampToEdge}}
	doc.Textures[0].Sampler = gltf.Index(0)
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
		BaseColorTexture: &gltf.TextureInfo{Index: 0},
	}}}
	doc.Meshes[0].Primitives[0].Material = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	base := h.backend.texturesBoundTo("baseColorTexture")
	normal := h.backend.texturesBoundTo("normalTexture")
	if len(base) == 0 || len(normal) == 0 {
		t.Fatalf("base = %v, normal = %v; both slots must bind", base, normal)
	}
	if base[len(base)-1] == normal[len(normal)-1] {
		t.Error("the file's base colour must be its own texture, not the flat-normal default")
	}
}

// glTF specifies the three filters and the two wrap modes independently, which
// is the shape SamplerDesc took to hold them.
func TestModelSamplerMapsWrapAndFilter(t *testing.T) {
	doc := testDoc()
	doc.Samplers = []*gltf.Sampler{{
		WrapS: gltf.WrapMirroredRepeat, WrapT: gltf.WrapClampToEdge,
		MagFilter: gltf.MagNearest, MinFilter: gltf.MinLinearMipMapNearest,
	}}
	sampler := modelSampler(doc, 0)
	if sampler.AddressU != gfx.AddressMirror || sampler.AddressV != gfx.AddressClamp {
		t.Errorf("address = %v/%v, want mirror/clamp", sampler.AddressU, sampler.AddressV)
	}
	if sampler.Mag != gfx.FilterNearest {
		t.Errorf("mag = %v, want nearest", sampler.Mag)
	}
	if sampler.Min != gfx.FilterLinear || sampler.Mip != gfx.FilterNearest {
		t.Errorf("min/mip = %v/%v, want linear/nearest", sampler.Min, sampler.Mip)
	}
	// A texture that names no sampler takes glTF's own default, which is repeat
	// filtered linearly - not SamplerDesc's zero value, which clamps.
	if defaultModelSampler.AddressU != gfx.AddressRepeat {
		t.Error("glTF's default wrap is repeat")
	}
}

// Every query fires the same idempotent load a draw does, so a caller polling
// this eventually gets an answer rather than polling an empty list forever.
func TestModelLightsTriggerTheLoadAndReturnTheFilesLights(t *testing.T) {
	doc := onePrimitiveModel(t)
	doc.ExtensionsUsed = []string{"KHR_lights_punctual"}
	doc.Extensions = gltf.Extensions{"KHR_lights_punctual": map[string]any{
		"lights": []map[string]any{{"type": "point", "name": "bulb", "intensity": 2.0}},
	}}
	doc.Nodes[0].Extensions = gltf.Extensions{"KHR_lights_punctual": map[string]any{"light": 0}}
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), func(*OpQueue) {})

	var lights []ModelLight
	var ok bool
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{run: func(la LookupAccess) {
		lights, ok = la.ModelLights(modelPath, nil)
	}})
	if ok {
		t.Fatal("the first query cannot be resident; it triggers the load")
	}
	h.frameUntil(t, "the model to become resident", func() bool {
		h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{run: func(la LookupAccess) {
			lights, ok = la.ModelLights(modelPath, nil)
		}})
		return ok
	})
	if len(lights) != 1 || lights[0].Name != "bulb" || lights[0].Descr.Intensity != 2 {
		t.Fatalf("lights = %+v, want the file's one bulb", lights)
	}
}

// onePixelPNG encodes the smallest picture that is not the default texel, so a
// test can tell the file's texture from the 1x1 fallback.
func onePixelPNG(t testing.TB) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.NRGBA{R: 0x40, G: 0x80, B: 0xc0, A: 0xff})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

// A mesh several nodes reference is converted, uploaded and given a mesh id
// exactly once. glTF authors repeated parts that way - two wheels on one truck -
// and a per-node conversion would silently double the geometry.
func TestNodesSharingAMeshShareOneMeshID(t *testing.T) {
	doc := testDoc()
	mesh := triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "left", Mesh: gltf.Index(mesh), Translation: [3]float64{-2, 0, 0}},
		{Name: "right", Mesh: gltf.Index(mesh), Translation: [3]float64{2, 0, 0}},
	}
	sceneOf(doc, 0, 1)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2
	})
	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("batches = %v, want one per placement", batches)
	}
	if batches[0].MeshID != batches[1].MeshID {
		t.Errorf("mesh ids = %d and %d, want one upload shared by both nodes",
			batches[0].MeshID, batches[1].MeshID)
	}
}

// An in-flight path is a state rather than an absence, so a model drawn every
// frame while it loads enqueues exactly one command. A second install would
// claim fresh mesh slots, so a stable mesh id over many frames is what pins it.
func TestAModelDrawnEveryFrameLoadsOnce(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, onePrimitiveModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	first := h.passes()[0].Batches[0].MeshID
	for i := 0; i < 30; i++ {
		h.frame()
	}
	if got := h.passes()[0].Batches[0].MeshID; got != first {
		t.Fatalf("mesh id moved from %d to %d; the path loaded more than once", first, got)
	}
	if errs := h.errors(); len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}
}

// A primitive whose POSITION accessor declares no min/max leaves the whole
// model with no bound at all, so it is never culled - drawing too much is a
// cost you can profile, where a wrong box is a model that vanishes at one
// camera angle and nowhere else.
func TestAnUnboundedModelIsNeverCulled(t *testing.T) {
	doc := testDoc()
	position := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Accessors[position].Min, doc.Accessors[position].Max = nil, nil
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{
		{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: position}},
	}}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	// Far behind the camera, where a bounded model would certainly be culled.
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)),
		drawModel(modelPath, ModelDraw{Transform: At(0, 0, 400)}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 1
	})
	if culled := h.passes()[0].Culled; culled != 0 {
		t.Fatalf("culled = %d, want a never-cull model kept", culled)
	}
	var missing ErrModelBoundsMissing
	if !anyErrorAs(h.errors(), &missing) {
		t.Fatalf("errors = %v, want a missing-bounds report", h.errors())
	}
}

// A model's own materials reach the record scene binds per batch.
//
// Two glTF materials that differ only in their factors are deliberately one gfx
// material: the factors live in the scenePbrRecord, which is a bound range of
// the frame's arena, so the pipeline and the bindings are identical and the two
// share a material id. That is what "the binding is the addressing" buys - one
// pipeline, two records, no index anyone has to agree on.
func TestAModelBindsItsOwnMaterialRecords(t *testing.T) {
	doc := testDoc()
	doc.Materials = []*gltf.Material{
		{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{1, 0, 0, 1}}},
		{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{0, 1, 0, 1}}},
	}
	first := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	second := triangleAttributes(doc, [][3]float32{{2, 0, 0}, {3, 0, 0}, {2, 1, 0}})
	doc.Meshes = []*gltf.Mesh{{Primitives: []*gltf.Primitive{
		{Attributes: first, Material: gltf.Index(0)},
		{Attributes: second, Material: gltf.Index(1)},
	}}}
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2
	})
	batches := h.passes()[0].Batches
	if batches[0].MaterialID != batches[1].MaterialID {
		t.Errorf("material ids = %d and %d; two materials differing only in their factors "+
			"are one pipeline and two records", batches[0].MaterialID, batches[1].MaterialID)
	}
	if batches[0].MeshID == batches[1].MeshID {
		t.Errorf("both primitives report mesh %d, want two", batches[0].MeshID)
	}
	var records []m.Vec4
	h.inspect(func(q *OpQueue) {
		for _, draw := range q.flushDraws() {
			records = append(records, draw.pbrRecord().BaseColorFactor)
		}
	})
	want := map[m.Vec4]bool{{X: 1, W: 1}: true, {Y: 1, W: 1}: true}
	for _, record := range records {
		if !want[record] {
			t.Fatalf("baseColorFactor = %v, want one of the file's two", record)
		}
		delete(want, record)
	}
	if len(want) != 0 {
		t.Fatalf("records = %v, want both of the file's factors", records)
	}
}
