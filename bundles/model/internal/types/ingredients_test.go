package types

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// testDefaults stands in for the two baked default textures: two distinct
// descriptors are all a binding test needs to tell them apart.
func testDefaults() PbrDefaults {
	return PbrDefaults{
		White:      gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}, true, false),
		FlatNormal: gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{0x80, 0x80, 0xff, 0xff}, true, false),
	}
}

// A file's material keeps what it is made of, not only what the bundled shader
// made of it: the forward material of every variant is exactly the bundled
// variant over the ingredients, so a renderer resolving a caller's shader over
// the same ingredients loses nothing the bundled draw would have bound.
func TestAModelMaterialsForwardIsTheBundledShaderOverItsIngredients(t *testing.T) {
	loaded := &loadedMaterial{
		values: paintPbrValues(m.NewColorLinear(0.5, 0.25, 1, 1), false),
		state:  PbrState(AlphaBlend, true),
	}
	for slot := range loaded.slots {
		loaded.slots[slot] = -1
	}
	built := bindModelMaterial(loaded, nil, testDefaults())

	if built.State != loaded.state {
		t.Fatalf("the ingredients lost the file's state: %+v", built.MaterialIngredients)
	}
	if len(built.Params) != 2*len(PbrSlots)+pbrValueCount {
		t.Fatalf("the ingredients carry %d params, want every slot's texture and sampler and every number",
			len(built.Params))
	}
	if got, _ := built.Params[2*len(PbrSlots)].VecValue(); got != loaded.values.baseColorFactor {
		t.Errorf("the ingredients carry baseColorFactor %v, want the file's %v", got, loaded.values.baseColorFactor)
	}
	for i, slot := range PbrSlots {
		if built.Params[2*i].Name() != slot.Texture || built.Params[2*i+1].Name() != slot.Sampler {
			t.Errorf("param pair %d is %s, %s; want slot %s's texture and sampler",
				i, built.Params[2*i].Name(), built.Params[2*i+1].Name(), slot.Texture)
		}
	}
	for variant := range VariantCount {
		want := gfx.MaterialWithState(VariantShader(gfx.ShaderDescr{}, ShaderVariant(variant)), built.State, built.Params...)
		if built.Forward[variant].Fingerprint() != want.Fingerprint() {
			t.Errorf("variant %d's forward material is not the bundled variant over the ingredients", variant)
		}
	}
}

// A baked mesh has a file too: the bundled PBR's ingredients, white and flat
// in every slot, opaque, single-sided and painted white. Its forward material
// is the same shader over them.
func TestTheBundledIngredientsAreWhatTheBundledMaterialBinds(t *testing.T) {
	defaults := testDefaults()
	ingredients := BundledIngredients(defaults)
	if ingredients.State != PbrState(AlphaOpaque, false) {
		t.Errorf("the bundled state is %+v, want opaque single-sided", ingredients.State)
	}
	white := paintPbrValues(m.NewColorLinear(1, 1, 1, 1), false)
	if got := ingredients.Params[2*len(PbrSlots):]; gfx.FingerprintParams(got) != gfx.FingerprintParams(white.appendParams(nil)) {
		t.Errorf("the bundled numbers are not white paint")
	}
	bundled := BundledPbr(defaults)
	for variant := range VariantCount {
		want := gfx.MaterialWithState(VariantShader(gfx.ShaderDescr{}, ShaderVariant(variant)),
			ingredients.State, ingredients.Params...)
		if bundled[variant].Fingerprint() != want.Fingerprint() {
			t.Errorf("variant %d of the bundled PBR is not its shader over the bundled ingredients", variant)
		}
	}
}

// The variant is the geometry's, whatever shader is in effect: a caller's
// shader gains exactly the defines the bundled one would, and the zero shader
// is the bundled one.
func TestVariantShaderAddsTheGeometrysDefinesToAnyShader(t *testing.T) {
	custom := gfx.ShaderWithResource("app/fade.wgsl", gfx.ShaderConst("K", "2"))
	cases := []struct {
		variant ShaderVariant
		want    gfx.ShaderDescr
	}{
		{VariantStatic, custom},
		{VariantFor(true, false), gfx.ShaderWithResource("app/fade.wgsl", gfx.ShaderConst("K", "2"), gfx.ShaderDefine("SCENE_SKIN"))},
		{VariantFor(false, true), gfx.ShaderWithResource("app/fade.wgsl", gfx.ShaderConst("K", "2"), gfx.ShaderDefine("SCENE_MORPH"))},
		{VariantFor(true, true), gfx.ShaderWithResource("app/fade.wgsl", gfx.ShaderConst("K", "2"),
			gfx.ShaderDefine("SCENE_SKIN"), gfx.ShaderDefine("SCENE_MORPH"))},
	}
	for _, c := range cases {
		if got := VariantShader(custom, c.variant); got != c.want {
			t.Errorf("variant %d: supply %q, want %q", c.variant, got.Supply(), c.want.Supply())
		}
		if got, want := VariantShader(gfx.ShaderDescr{}, c.variant), c.variant.shader(); got != want {
			t.Errorf("variant %d of the zero shader is %q, want the bundled variant %q", c.variant, got.Supply(), want.Supply())
		}
	}
}

// The default scene shader is the Lookup's, set through the write facade and
// read through the read one. Unset, it is the zero descriptor, which every
// renderer reads as the bundled PBR. Its params are copied at the call, so a
// caller reusing its slice does not rewrite what every draw binds.
func TestTheDefaultSceneShaderIsSetOnceAndReadBack(t *testing.T) {
	lookup := NewLookup()
	read := NewLookupReadAccess(lookup)
	if got := read.DefaultSceneShader(); got.Source != (gfx.ShaderDescr{}) || len(got.Params) != 0 {
		t.Fatalf("an untouched Lookup's default is %+v, want the zero descriptor", got)
	}
	params := []gfx.ParameterDescr{gfx.FloatParam("fade", 0.5)}
	source := gfx.ShaderWithResource("app/fade.wgsl")
	NewLookupAccess(kernel.Kernel{}, lookup).SetDefaultSceneShader(SceneShaderDescr{Source: source, Params: params})
	params[0] = gfx.FloatParam("other", 1)

	got := read.DefaultSceneShader()
	if got.Source != source {
		t.Errorf("the default's source is %q, want %q", got.Source.Path(), source.Path())
	}
	if len(got.Params) != 1 || got.Params[0].Name() != "fade" {
		t.Errorf("the default's params are %v, want the fade the call passed", gfx.ParameterViewsOf(got.Params))
	}

	NewLookupAccess(kernel.Kernel{}, lookup).SetDefaultSceneShader(SceneShaderDescr{})
	if got := read.DefaultSceneShader(); got.Source != (gfx.ShaderDescr{}) || len(got.Params) != 0 {
		t.Errorf("setting the zero descriptor left %+v, want the bundled PBR back", got)
	}
}
