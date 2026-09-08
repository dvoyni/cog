package scene

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// OverrideParams merges by name into the draw's own copy of the bundled PBR
// record. glTF's parameter names are verbatim and user-facing, so
// gfx.ColorParam("baseColorFactor", c) is what a caller writes to tint a model
// and the glTF specification is the documentation of what it means.
func TestAnOverrideParamMergesIntoTheRecordByName(t *testing.T) {
	record := defaultPbrRecord()
	overrideRecord(&record, []gfx.ParameterDescr{
		gfx.ColorParam("baseColorFactor", m.Color{R: 0.25, G: 0.5, B: 0.75, A: 0.5}),
		gfx.ColorParam("emissiveFactor", m.Color{R: 1, G: 2, B: 3}),
		gfx.FloatParam("metallicFactor", 0.25),
		gfx.FloatParam("roughnessFactor", 0.5),
		gfx.FloatParam("normalScale", 2),
		gfx.FloatParam("occlusionStrength", 0.75),
		gfx.FloatParam("alphaCutoff", 0.5),
	})
	if want := (m.Vec4{X: 0.25, Y: 0.5, Z: 0.75, W: 0.5}); record.BaseColorFactor != want {
		t.Errorf("baseColorFactor = %v, want %v", record.BaseColorFactor, want)
	}
	if want := (m.Vec4{X: 1, Y: 2, Z: 3}); record.EmissiveFactor != want {
		t.Errorf("emissiveFactor = %v, want %v", record.EmissiveFactor, want)
	}
	if record.MetallicFactor != 0.25 || record.RoughnessFactor != 0.5 {
		t.Errorf("metallic/roughness = %v/%v, want 0.25/0.5",
			record.MetallicFactor, record.RoughnessFactor)
	}
	if record.NormalScale != 2 || record.OcclusionStrength != 0.75 {
		t.Errorf("normalScale/occlusionStrength = %v/%v, want 2/0.75",
			record.NormalScale, record.OcclusionStrength)
	}
	if record.AlphaCutoff != 0.5 {
		t.Errorf("alphaCutoff = %v, want 0.5", record.AlphaCutoff)
	}
}

// The per-slot texture metadata is flat named members rather than an array
// precisely so that every one of them is reachable by name - animating
// baseColorTransform per frame is UV scrolling, which the array form would have
// foreclosed permanently.
func TestEverySlotsTransformAndRotationAreReachableByName(t *testing.T) {
	for slot, names := range pbrSlots {
		record := defaultPbrRecord()
		overrideRecord(&record, []gfx.ParameterDescr{
			gfx.VecParam(names.transform, m.Vec4{X: 0.1, Y: 0.2, Z: 2, W: 3}),
			gfx.FloatParam(names.rotation, 1.5),
		})
		if want := (m.Vec4{X: 0.1, Y: 0.2, Z: 2, W: 3}); record.Transforms[slot] != want {
			t.Errorf("%s = %v, want %v", names.transform, record.Transforms[slot], want)
		}
		if record.Rotations[slot] != 1.5 {
			t.Errorf("%s = %v, want 1.5", names.rotation, record.Rotations[slot])
		}
		for other := range pbrSlots {
			if other == slot {
				continue
			}
			if record.Transforms[other] != (m.Vec4{Z: 1, W: 1}) || record.Rotations[other] != 0 {
				t.Fatalf("overriding %s moved slot %d as well", names.transform, other)
			}
		}
	}
}

// A name the record has no member for is left to gfx, which binds a draw
// parameter over a material one by name and drops what the shader does not
// declare. That silence is what keeps the broadcast safe across tags: an
// alphaMode MASK shadow shader declares baseColorTexture and alphaCutoff while
// an OPAQUE one declares neither, and one draw's parameter list is matched
// against both.
func TestAnOverrideParamTheRecordHasNoMemberForLeavesItUntouched(t *testing.T) {
	record := defaultPbrRecord()
	before := record
	overrideRecord(&record, []gfx.ParameterDescr{
		gfx.ColorParam("teamColor", m.Color{R: 1}),
		gfx.FloatParam("dissolve", 0.5),
		gfx.FloatParam("uvSets", 1),
		gfx.FloatParam("pad", 1),
	})
	if record != before {
		t.Errorf("record = %+v, want it untouched by names it has no member for", record)
	}
}

// A parameter whose kind does not fit the member it names is ignored on the
// same footing as a name the record does not carry. The merge runs per batch
// per pass with no reporter on the path, and the broadcast means one parameter
// list is matched against several materials, so "does not fit here" is not on
// its own evidence of a caller bug.
func TestAnOverrideParamOfAKindTheMemberCannotTakeIsIgnored(t *testing.T) {
	record := defaultPbrRecord()
	before := record
	overrideRecord(&record, []gfx.ParameterDescr{
		gfx.FloatParam("baseColorFactor", 0.5),
		gfx.ColorParam("metallicFactor", m.Color{R: 0.5}),
		gfx.TextureParam("baseColorTransform", gfx.TextureDescr{}),
	})
	if record != before {
		t.Errorf("record = %+v, want it untouched by mistyped overrides", record)
	}
}

// The merge is over the record the draw already had, not over glTF's defaults:
// a fade overrides baseColorFactor and leaves the file's roughness where the
// artist put it.
func TestAnOverrideParamLeavesEveryMemberItDoesNotNameAlone(t *testing.T) {
	record := defaultPbrRecord()
	record.RoughnessFactor = 0.3
	record.BaseColorFactor = m.Vec4{X: 1, Y: 0, Z: 0, W: 1}
	overrideRecord(&record, []gfx.ParameterDescr{
		gfx.ColorParam("baseColorFactor", m.Color{R: 1, G: 1, B: 1, A: 0.25}),
	})
	if record.RoughnessFactor != 0.3 {
		t.Errorf("roughnessFactor = %v, want the file's 0.3 kept", record.RoughnessFactor)
	}
	if want := (m.Vec4{X: 1, Y: 1, Z: 1, W: 0.25}); record.BaseColorFactor != want {
		t.Errorf("baseColorFactor = %v, want %v", record.BaseColorFactor, want)
	}
}

// Every member the shader declares is reachable by name, and the two that are
// not are named here on purpose.
//
// The names in member are transcribed from the WGSL by hand, and a hand
// transcription of an asset has been the wrong thing more than once in this
// package. So the shader is the authority: this reads the struct scene ships
// and fails on the member somebody adds without a way to address it, which is
// the whole reason the per-slot metadata is flat rather than an array.
func TestEveryPbrRecordMemberTheShaderDeclaresIsAddressable(t *testing.T) {
	// The struct lives in one of the nine sources scene.wgsl composes, so the
	// authority is the flattened module rather than any one file.
	source := flattenedSceneShader(t)
	// pad is not a member anyone means, and uvSets is a packed five-bit
	// selector no parameter kind expresses - which TEXCOORD set a slot samples
	// is the file's statement about its own mesh.
	unaddressable := map[string]bool{"uvSets": true, "pad": true}
	var record scenePbrRecord
	members := wgslStructMembers(t, source, "ScenePbrMaterial")
	if len(members) != 19 {
		t.Fatalf("parsed %d members of ScenePbrMaterial: %v", len(members), members)
	}
	for _, name := range members {
		vec, scalar := record.member(name)
		if unaddressable[name] {
			if vec != nil || scalar != nil {
				t.Errorf("%s is addressable, but is listed as one that is not", name)
			}
			continue
		}
		if vec == nil && scalar == nil {
			t.Errorf("the shader declares %s and nothing can override it", name)
		}
	}
}

// wgslStructMembers lists the member names one WGSL struct declares, in order.
// It is a line scan rather than a parser because the struct it reads is one
// scene owns and formats itself.
func wgslStructMembers(t testing.TB, source, name string) []string {
	t.Helper()
	start := strings.Index(source, "struct "+name+" {")
	if start < 0 {
		t.Fatalf("the shader declares no struct %s", name)
	}
	body := source[start:]
	body = body[:strings.Index(body, "\n};")]
	var members []string
	for _, line := range strings.Split(body, "\n")[1:] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if member, _, ok := strings.Cut(line, ":"); ok {
			members = append(members, strings.TrimSpace(member))
		}
	}
	return members
}
