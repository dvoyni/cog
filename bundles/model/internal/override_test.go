package internal

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga/ir"
)

// Every member of the material's uniform block is a param the material
// carries, exactly once, of a kind gfx can pack into it, and nothing else the
// material carries is a number.
//
// gfx packs a member nothing supplies as zero, and a zero normalScale or
// texture scale is a broken surface rather than a default one, so a member
// added to the WGSL without a param is caught here. The shader is the
// authority: the struct is read out of the flattened module scene ships.
func TestEveryMemberOfTheMaterialBlockIsAParam(t *testing.T) {
	source := flattenedSceneShader(t)
	members := wgslStructMembers(t, source, "ScenePbrMaterial")
	if len(members) != 18 {
		t.Fatalf("parsed %d members of ScenePbrMaterial: %v", len(members), members)
	}
	white := gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{0xff, 0xff, 0xff, 0xff}, true, false)
	ingredients := model.BundledIngredients(model.PbrDefaults{White: white, FlatNormal: white})
	numbers := map[string]string{}
	for _, param := range ingredients.Params {
		view := gfx.ParameterViewOf(param)
		if view.Texture != nil || view.Sampler != nil {
			continue
		}
		if _, twice := numbers[param.Name()]; twice {
			t.Errorf("%s is carried twice", param.Name())
		}
		numbers[param.Name()] = view.Kind
	}
	for _, name := range members {
		kind, ok := numbers[name]
		if !ok {
			t.Errorf("the shader declares %s and the material carries no param for it", name)
			continue
		}
		delete(numbers, name)
		want := map[string]string{"vec4<f32>": "vec4", "f32": "float", "u32": "raw"}[wgslMemberType(t, source, name)]
		if kind != want {
			t.Errorf("%s is carried as a %s param, want %s", name, kind, want)
		}
	}
	for name := range numbers {
		t.Errorf("the material carries %s, which the shader does not declare", name)
	}
}

// The block is a uniform, which gfx caps at 256 bytes and refuses past it.
func TestTheMaterialBlockIsAUniformWithinGfxsCap(t *testing.T) {
	module := lowerForTest(t, flattenedSceneShader(t))
	for _, global := range module.GlobalVariables {
		if global.Name != "scenePbrMaterial" {
			continue
		}
		if global.Space != ir.SpaceUniform {
			t.Fatalf("scenePbrMaterial is in space %v, want uniform", global.Space)
		}
		if span := module.Types[global.Type].Inner.(ir.StructType).Span; span > 256 {
			t.Fatalf("scenePbrMaterial spans %d bytes, past gfx's 256-byte cap", span)
		}
		return
	}
	t.Fatal("the bundled shader declares no scenePbrMaterial")
}

// wgslMemberType is the declared type of one ScenePbrMaterial member.
func wgslMemberType(t testing.TB, source, member string) string {
	t.Helper()
	start := strings.Index(source, "struct ScenePbrMaterial {")
	body := source[start : start+strings.Index(source[start:], "\n};")]
	for _, line := range strings.Split(body, "\n") {
		name, declared, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(name) == member {
			return strings.TrimSuffix(strings.TrimSpace(declared), ",")
		}
	}
	t.Fatalf("ScenePbrMaterial declares no %s", member)
	return ""
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
