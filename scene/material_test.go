package scene

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/gfx"
)

// testMaterial builds a caller material serving the given tags, each with its
// own gfx material, so two entries are distinguishable by their state.
func testMaterial(tags ...PassTag) Material {
	material := make(Material, 0, len(tags))
	for i, tag := range tags {
		state := gfx.StateOpaque3D
		if i%2 == 1 {
			state = gfx.StateTransparent3D
		}
		material = append(material, MaterialTag{
			Tag:   tag,
			Descr: gfx.MaterialWithState(gfx.ShaderWithResource(sceneShaderPath), state),
		})
	}
	return material
}

func TestANilMaterialResolvesToTheBundledPbrOnTheForwardTag(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward := table.internTag(TagForward)

	entry, ok := resolve(&table, discardErrors, nil, forward)
	if !ok {
		t.Fatal("a nil material does not serve the forward tag; nil is the bundled PBR")
	}
	if entry.materialID == 0 {
		t.Fatal("the bundled entry has no material id")
	}
}

// The bundled PBR is one forward entry and nothing else, so a pass with any
// other tag draws none of it - which is what makes a shadow pass additive
// rather than a change to every existing draw.
func TestTheBundledPbrServesTheForwardTagAndNothingElse(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	shadow := table.internTag("shadow")

	if _, ok := resolve(&table, discardErrors, nil, shadow); ok {
		t.Fatal("the bundled PBR served a shadow pass; it has one forward entry and nothing else")
	}
}

func TestAMaterialWithoutAnEntryForAPassTagIsSkipped(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	material := testMaterial(TagForward)
	shadow := table.internTag("shadow")

	if _, ok := resolve(&table, discardErrors, material, shadow); ok {
		t.Fatal("a material with no shadow entry was drawn in the shadow pass")
	}
	forward := table.internTag(TagForward)
	if _, ok := resolve(&table, discardErrors, material, forward); !ok {
		t.Fatal("a material with a forward entry was skipped in the forward pass")
	}
}

// An empty Tag reads as TagForward, so the hand-written one-entry case needs no
// tag at all.
func TestAnEmptyTagEntryServesTheForwardPass(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	material := Material{{Descr: gfx.Material(gfx.ShaderWithResource(sceneShaderPath))}}

	if _, ok := resolve(&table, discardErrors, material, table.internTag(TagForward)); !ok {
		t.Fatal("an entry with no tag did not serve the forward pass")
	}
}

func TestADuplicateTagIsReportedOnceAndTheFirstEntryWins(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	material := testMaterial(TagForward, TagForward)
	var reported []error
	report := func(err error) { reported = append(reported, err) }

	entry, ok := resolve(&table, report, material, table.internTag(TagForward))
	if !ok {
		t.Fatal("a material with a duplicate tag serves nothing at all")
	}
	if entry.index != 0 {
		t.Fatalf("entry %d won the duplicate tag, want the first", entry.index)
	}
	if len(reported) != 1 {
		t.Fatalf("a duplicate tag reported %d errors, want 1: %v", len(reported), reported)
	}
	var duplicate ErrMaterialTagAlreadyServed
	if !errors.As(reported[0], &duplicate) || duplicate.Tag != TagForward {
		t.Fatalf("reported %v, want ErrMaterialTagAlreadyServed for the forward tag", reported[0])
	}

	// The check runs at intern time, not per draw: a second draw of the same
	// material this frame reports nothing more.
	resolve(&table, report, material, table.internTag(TagForward))
	if len(reported) != 1 {
		t.Fatalf("resolving the same material twice reported %d errors, want 1", len(reported))
	}
}

// Ids are per (material, tag), because the sort key must distinguish the
// pipelines actually bound in a pass.
func TestMaterialIdsAreOnePerMaterialAndTag(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward, shadow := table.internTag(TagForward), table.internTag("shadow")
	material := testMaterial(TagForward, "shadow")
	other := testMaterial(TagForward)

	first, _ := resolve(&table, discardErrors, material, forward)
	again, _ := resolve(&table, discardErrors, material, forward)
	cast, _ := resolve(&table, discardErrors, material, shadow)
	elsewhere, _ := resolve(&table, discardErrors, other, forward)

	if first.materialID != again.materialID {
		t.Fatalf("one material interned twice took ids %d and %d", first.materialID, again.materialID)
	}
	if cast.materialID == first.materialID {
		t.Fatalf("two tags of one material share material id %d", cast.materialID)
	}
	if elsewhere.materialID == first.materialID {
		t.Fatalf("two materials share material id %d", elsewhere.materialID)
	}
}

// Resolving is what a draw pays per pass, so it must not allocate once the
// table has seen the frame's materials before.
func TestResolvingAnInternedMaterialAllocatesNothing(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward := table.internTag(TagForward)
	material := testMaterial(TagForward)
	resolve(&table, discardErrors, material, forward)

	allocations := testing.AllocsPerRun(100, func() {
		resolve(&table, discardErrors, material, forward)
	})
	if allocations != 0 {
		t.Fatalf("resolving an interned material allocated %v times per draw", allocations)
	}
}

// A frame reuses the table's backing, so a steady frame interns without
// allocating after the first.
func TestTheMaterialTableKeepsItsBackingAcrossFrames(t *testing.T) {
	var table materialTable
	bundled := bundledPbr(pbrDefaults{})
	material := testMaterial(TagForward)
	table.reset(bundled)
	forward := table.internTag(TagForward)
	resolve(&table, discardErrors, material, forward)
	resolve(&table, discardErrors, nil, forward)

	allocations := testing.AllocsPerRun(100, func() {
		table.reset(bundled)
		resolve(&table, discardErrors, material, forward)
		resolve(&table, discardErrors, nil, forward)
	})
	if allocations != 0 {
		t.Fatalf("a steady frame allocated %v times interning its materials", allocations)
	}
}

func TestPassTagsInternToStableDenseIds(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward, shadow := table.internTag(TagForward), table.internTag("shadow")

	if forward == shadow {
		t.Fatalf("two tags interned to the same id %d", forward)
	}
	if again := table.internTag(TagForward); again != forward {
		t.Fatalf("the forward tag interned to %d and then %d", forward, again)
	}
	// An empty tag is the forward tag, not a second one.
	if empty := table.internTag(""); empty != forward {
		t.Fatalf("the empty tag interned to %d, want the forward tag's %d", empty, forward)
	}
}

// discardErrors is the report sink for a test that is not asserting reports.
func discardErrors(error) {}

// glTF's alphaMode and doubleSided are the only things that change the bundled
// PBR's fixed-function state, and MASK is fixed-function-identical to OPAQUE:
// it writes depth and batches with the opaque geometry, because the cutoff is
// entirely a fragment-shader concern.
func TestAlphaModeAndDoubleSidedMapOntoPipelineState(t *testing.T) {
	opaque := pbrState(alphaOpaque, false)
	if opaque != (gfx.MaterialState{
		Blend: gfx.BlendOpaque, DepthCompare: gfx.CompareLess, DepthWrite: true, Cull: gfx.CullBack,
	}) {
		t.Fatalf("an opaque single-sided material is %+v, want the opaque 3D state culling back faces", opaque)
	}
	if mask := pbrState(alphaMask, false); mask != opaque {
		t.Fatalf("a MASK material is %+v, want the opaque state exactly: %+v", mask, opaque)
	}
	blend := pbrState(alphaBlend, false)
	if blend.Blend != gfx.BlendAlpha || blend.DepthWrite {
		t.Fatalf("a BLEND material is %+v, want alpha blending without depth writes", blend)
	}
	if double := pbrState(alphaOpaque, true); double.Cull != gfx.CullNone {
		t.Fatalf("a double-sided material culls %v, want CullNone", double.Cull)
	}
}

// resolve is what one frame does with one draw: intern its material once, then
// read its entry for the pass being flushed.
func resolve(table *materialTable, report func(error), material Material, tag tagID) (materialEntry, bool) {
	return table.entry(table.intern(report, material), tag)
}

// A caller-supplied gfx.MaterialDescr has no id of its own, so materials intern
// per frame by content: two Material values built separately with the same
// shader, state and parameters take one id and sort together.
func TestMaterialsInternByContentNotByBacking(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward := table.internTag(TagForward)
	build := func() Material {
		return Material{{Descr: gfx.MaterialWithState(
			gfx.ShaderWithResource(sceneShaderPath), gfx.StateOpaque3D, gfx.FloatParam("k", 1),
		)}}
	}
	first, _ := resolve(&table, discardErrors, build(), forward)
	second, _ := resolve(&table, discardErrors, build(), forward)
	if first.materialID != second.materialID {
		t.Fatalf("two identical materials took ids %d and %d", first.materialID, second.materialID)
	}
	different := Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), gfx.StateOpaque3D, gfx.FloatParam("k", 2),
	)}}
	if other, _ := resolve(&table, discardErrors, different, forward); other.materialID == first.materialID {
		t.Fatal("a material with a different parameter took the same id")
	}
	// The tag is part of the identity too: the same descr under another tag is
	// another (material, tag) pair.
	tagged := Material{{Tag: "shadow", Descr: build()[0].Descr}}
	if _, ok := resolve(&table, discardErrors, tagged, forward); ok {
		t.Fatal("a shadow-only material served the forward pass")
	}
}

// The sort class is the material's: anything that blends against the target
// sorts back to front, and only an opaque blend mode sorts by material key.
// Alpha-masked is opaque plus a shader discard, so it lands in the opaque class
// through its state alone.
func TestTheBlendClassFollowsTheEntrysBlendMode(t *testing.T) {
	var table materialTable
	table.reset(bundledPbr(pbrDefaults{}))
	forward := table.internTag(TagForward)
	shader := gfx.ShaderWithResource(sceneShaderPath)
	cases := []struct {
		name  string
		descr gfx.MaterialDescr
		blend bool
	}{
		{"opaque 3D", gfx.MaterialWithState(shader, gfx.StateOpaque3D), false},
		{"mask", gfx.MaterialWithState(shader, pbrState(alphaMask, false)), false},
		{"transparent 3D", gfx.MaterialWithState(shader, gfx.StateTransparent3D), true},
		{"default gfx.Material", gfx.Material(shader), true},
		{"additive", gfx.MaterialWithState(shader, gfx.MaterialState{Blend: gfx.BlendAdditive}), true},
	}
	for _, c := range cases {
		entry, ok := resolve(&table, discardErrors, Material{{Descr: c.descr}}, forward)
		if !ok {
			t.Fatalf("%s: the material does not serve the forward pass", c.name)
		}
		if entry.blend != c.blend {
			t.Fatalf("%s: blend class %v, want %v", c.name, entry.blend, c.blend)
		}
	}
	bundled, _ := resolve(&table, discardErrors, nil, forward)
	if bundled.blend {
		t.Fatal("the bundled PBR is in the blend class")
	}
}
