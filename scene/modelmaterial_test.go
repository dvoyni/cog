package scene

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
)

// twoMaterialModel is one node whose mesh has two primitives, each with its own
// glTF material: red and green, one rough and one smooth. It is the model the
// broadcast tests need - the six-material case in miniature.
func twoMaterialModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	doc.Materials = []*gltf.Material{
		{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{1, 0, 0, 1},
			RoughnessFactor: gltf.Float(0.25),
		}},
		{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			BaseColorFactor: &[4]float64{0, 1, 0, 1},
			RoughnessFactor: gltf.Float(0.75),
		}},
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
	return doc
}

// residentModelDraw runs frames until the model at modelPath draws, then hands
// back the draw records the flush expanded it into.
func residentModelDraw(t testing.TB, doc *gltf.Document, draw ModelDraw) (*harness, []drawRecord) {
	t.Helper()
	h := newHarnessWithFiles(t, modelFiles(glb(t, doc)), drawModel(modelPath, draw))
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances > 0
	})
	var records []drawRecord
	h.inspect(func(q *OpQueue) { records = append(records, q.flushDraws()...) })
	return h, records
}

// A plain draw binds the file's own records directly: the Material the load
// built and the PBR record beside it, both owned by the resident entry and
// shared by every draw of the path. There is no per-draw copy of either, which
// is what makes the common path cost what the same geometry recorded by hand
// would.
func TestAPlainModelDrawBindsTheFilesRecordsWithNoCopy(t *testing.T) {
	h, records := residentModelDraw(t, twoMaterialModel(t), ModelDraw{})
	if len(records) != 2 {
		t.Fatalf("expanded to %d draws, want one per primitive", len(records))
	}
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		key, _ := modelKey(modelPath)
		entry := lookup.modelEntry(key)
		if entry == nil || len(entry.materials) != 2 {
			t.Fatalf("entry = %v, want the two materials the file declares", entry)
		}
		for i := range records {
			owned := &entry.materials[i]
			if records[i].pbr != &owned.record {
				t.Errorf("draw %d binds a copied record, want the entry's own", i)
			}
			if &records[i].material[0] != &owned.variants[variantStatic][0] {
				t.Errorf("draw %d binds a copied Material, want the entry's own", i)
			}
		}
	}})
}

// OverrideParams merges by name over each primitive's own record, and it
// broadcasts: one call's parameter list reaches every material the draw binds,
// which is what the common per-draw override - team colour, hit flash, fade -
// actually wants. The file's textures and every member the override does not
// name survive, because the merge is over the file's record rather than over
// glTF's defaults.
func TestOverrideParamsBroadcastToEveryMaterialTheDrawBinds(t *testing.T) {
	tint := m.Color{R: 0.5, G: 0.5, B: 0.5, A: 0.5}
	h, records := residentModelDraw(t, twoMaterialModel(t), ModelDraw{
		OverrideParams: []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", tint)},
	})
	defer h.frame()
	if len(records) != 2 {
		t.Fatalf("expanded to %d draws, want one per primitive", len(records))
	}
	want := m.Vec4{X: 0.5, Y: 0.5, Z: 0.5, W: 0.5}
	roughness := map[float32]bool{0.25: true, 0.75: true}
	for i := range records {
		record := records[i].pbrRecord()
		if record.BaseColorFactor != want {
			t.Errorf("draw %d has baseColorFactor %v, want the broadcast %v",
				i, record.BaseColorFactor, want)
		}
		if !roughness[record.RoughnessFactor] {
			t.Errorf("draw %d has roughness %v, want one of the file's two",
				i, record.RoughnessFactor)
		}
		delete(roughness, record.RoughnessFactor)
	}
	if len(roughness) != 0 {
		t.Errorf("the two draws did not keep both of the file's roughnesses")
	}
}

// The merge keeps the file's textures: the Material the draw binds is still the
// entry's own, so every texture and sampler the load bound is bound unchanged.
// The overrides ride along as the draw's gfx parameters, which is where a
// texture override would land - gfx resolves a draw parameter over a material
// one of the same name, against the reflected layout of the entry's own shader,
// and drops what that shader does not declare.
func TestOverrideParamsKeepTheFilesTexturesAndReachTheDrawsParameters(t *testing.T) {
	overrides := []gfx.ParameterDescr{
		gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1}),
		gfx.TextureParam("baseColorTexture", gfx.TextureDescr{}),
	}
	h, records := residentModelDraw(t, twoMaterialModel(t), ModelDraw{OverrideParams: overrides})
	defer h.frame()
	for i := range records {
		if len(records[i].params) != len(overrides) {
			t.Fatalf("draw %d carries %d parameters, want the %d it was given",
				i, len(records[i].params), len(overrides))
		}
		for j := range overrides {
			if records[i].params[j].Name() != overrides[j].Name() {
				t.Errorf("draw %d parameter %d is %q, want %q",
					i, j, records[i].params[j].Name(), overrides[j].Name())
			}
		}
	}
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		key, _ := modelKey(modelPath)
		entry := lookup.modelEntry(key)
		for i := range records {
			if &records[i].material[0] != &entry.materials[i].variants[variantStatic][0] {
				t.Errorf("draw %d binds a copied Material, want the file's textures kept", i)
			}
		}
	}})
}

// The caller's parameter array is copied into the frame's own arena, so a
// caller may reuse its backing the moment the call returns - the same rule
// Plays and MorphWeights follow.
func TestOverrideParamsAreCopiedIntoTheFramesArena(t *testing.T) {
	overrides := []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1})}
	h, records := residentModelDraw(t, twoMaterialModel(t), ModelDraw{OverrideParams: overrides})
	defer h.frame()
	for i := range records {
		if &records[i].params[0] == &overrides[0] {
			t.Fatalf("draw %d aliases the caller's array, want the frame's arena", i)
		}
	}
}

// A non-nil Material replaces the file's wholesale. The file's PBR records are
// not bound and their parameters do not survive: this is the dissolve, the
// silhouette and the depth-only case, where binding the artist's base colour
// under a shader that never heard of it is the wrong picture with nothing in
// the frame to explain it.
func TestAModelDrawWithAMaterialReplacesTheFilesWholesale(t *testing.T) {
	replacement := Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), pbrState(alphaOpaque, false))}}
	h, records := residentModelDraw(t, twoMaterialModel(t),
		ModelDraw{Material: replacement})
	defer h.frame()
	if len(records) != 2 {
		t.Fatalf("expanded to %d draws, want one per primitive", len(records))
	}
	for i := range records {
		if &records[i].material[0] != &replacement[0] {
			t.Errorf("draw %d binds a material other than the caller's", i)
		}
		if records[i].pbr != nil {
			t.Errorf("draw %d still points at the file's record, want it unbound", i)
		}
		record := records[i].pbrRecord()
		if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
			t.Errorf("draw %d has baseColorFactor %v, want the file's colour gone",
				i, record.BaseColorFactor)
		}
		if record.RoughnessFactor != 1 {
			t.Errorf("draw %d has roughness %v, want the file's roughness gone",
				i, record.RoughnessFactor)
		}
	}
}

// The two knobs do not overlap, but they compose: a replacement material takes
// the record glTF's own defaults would give it, and the overrides then merge
// over that. What does not survive is the file's numbers, which is the whole of
// "replaces wholesale".
func TestOverrideParamsMergeOverAReplacementMaterialsOwnDefaults(t *testing.T) {
	replacement := Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), pbrState(alphaOpaque, false))}}
	h, records := residentModelDraw(t, twoMaterialModel(t), ModelDraw{
		Material:       replacement,
		OverrideParams: []gfx.ParameterDescr{gfx.FloatParam("roughnessFactor", 0.5)},
	})
	defer h.frame()
	for i := range records {
		record := records[i].pbrRecord()
		if record.RoughnessFactor != 0.5 {
			t.Errorf("draw %d has roughness %v, want the override's 0.5",
				i, record.RoughnessFactor)
		}
		if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
			t.Errorf("draw %d has baseColorFactor %v, want the file's colour gone",
				i, record.BaseColorFactor)
		}
	}
}

// A mesh draw's Params are for what a custom material declares and scene knows
// nothing about, so they do not reach the bundled PBR record. OverrideParams is
// the model draw's field and the record is the model's own; a mesh that wants a
// colour names a Material.
func TestAMeshDrawsParamsDoNotReachTheBundledRecord(t *testing.T) {
	ref := MeshRef{source: meshDurable, id: 1, generation: 1}
	record := drawRecord{
		mesh:   ref,
		params: []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.Color{R: 1})},
	}.pbrRecord()
	if record.BaseColorFactor != (m.Vec4{X: 1, Y: 1, Z: 1, W: 1}) {
		t.Errorf("baseColorFactor = %v, want a mesh draw's params left out of the record",
			record.BaseColorFactor)
	}
}
