package scene

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/storage"
)

// flattenedSceneShader is the bundled module as the backend sees it, with every
// feature on. Tests that read the shader's text read this rather than any one
// source, because a declaration now lives in whichever of the nine sources owns
// it.
func flattenedSceneShader(t testing.TB, opts ...gfx.ShaderOption) string {
	t.Helper()
	text, _, err := gfx.FlattenShader(storage.NewFileSystem(shaderMountID, shaderFS),
		sceneShader(append([]gfx.ShaderOption{
			gfx.ShaderDefine("SCENE_SKIN"), gfx.ShaderDefine("SCENE_MORPH"),
		}, opts...)...))
	if err != nil {
		t.Fatalf("flatten the bundled scene shader: %v", err)
	}
	return text
}

// The split is line-preserving, which is what makes the source map a small
// table rather than a per-line array: the ten sources' lines total exactly the
// flattened module's, with a zero-line §4 prologue.
func TestTheSplitFlattensToExactlyItsSourcesLineCount(t *testing.T) {
	names, err := fs.Glob(shaderFS, "builtin/scene/*.wgsl")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(names) != 10 {
		t.Fatalf("the split ships %d sources, want the ten named in the migration: %v", len(names), names)
	}
	total := 0
	for _, name := range names {
		source, err := shaderFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		total += lineCountOf(string(source))
	}

	text, sourceMap, err := gfx.FlattenShader(storage.NewFileSystem(shaderMountID, shaderFS),
		sceneShader(gfx.ShaderDefine("SCENE_SKIN"), gfx.ShaderDefine("SCENE_MORPH")))
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if got := lineCountOf(text); got != total {
		t.Errorf("the module is %d lines, want the sources' %d", got, total)
	}
	for _, segment := range sourceMap.Segments {
		if segment.Source == "" {
			t.Errorf("the module carries a %d-line hoisted prologue; scene declares no §4 directive",
				segment.Length)
		}
	}
}

// Every variant flattens to the same line count, because a losing branch blanks
// rather than being deleted. That is what keeps a WGSL error on a real line
// whichever variant it lands in.
func TestEveryVariantFlattensToTheSameLineCount(t *testing.T) {
	var want int
	for _, defines := range [][]string{nil, {"SCENE_SKIN"}, {"SCENE_MORPH"}, {"SCENE_SKIN", "SCENE_MORPH"}} {
		opts := make([]gfx.ShaderOption, 0, len(defines))
		for _, name := range defines {
			opts = append(opts, gfx.ShaderDefine(name))
		}
		text, _, err := gfx.FlattenShader(storage.NewFileSystem(shaderMountID, shaderFS), sceneShader(opts...))
		if err != nil {
			t.Fatalf("%v: flatten: %v", defines, err)
		}
		got := lineCountOf(text)
		if want == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("%v flattens to %d lines, want %d", defines, got, want)
		}
	}
}

// A feature source guards its own body rather than being conditionally
// included, which is what keeps the include graph a pure function of the root:
// the set of sources a module is built from does not vary by define set.
func TestEveryFeatureSourceGuardsItsOwnBody(t *testing.T) {
	names, err := fs.Glob(shaderFS, "builtin/scene/*.wgsl")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	included := map[string]bool{}
	for _, name := range names {
		source, err := shaderFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(source), "\n") {
			line = strings.TrimSpace(line)
			after, ok := strings.CutPrefix(line, "//#include ")
			if !ok {
				continue
			}
			included[path.Join(path.Dir(name), strings.TrimSpace(after))] = true
		}
	}
	for _, name := range names {
		if name != "builtin/scene/scene.wgsl" && !included[name] {
			t.Errorf("%s is shipped but nothing includes it", name)
		}
	}
	if !included["builtin/scene/skin.wgsl"] || !included["builtin/scene/morph.wgsl"] {
		t.Error("the feature sources are not included unconditionally")
	}
}

// Two defines drive the split, not three: sceneAnim is guarded by a condition
// rather than by a define of its own. That is the only place `|` earns its keep
// in the whole split, and it earns it by removing a define.
func TestTheSplitUsesTwoDefinesAndOneConst(t *testing.T) {
	names, err := fs.Glob(shaderFS, "builtin/scene/*.wgsl")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	names = append(names, "")
	seen := map[string]bool{}
	consts := 0
	anyOfEither := false
	for _, name := range names[:len(names)-1] {
		source, err := shaderFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(source), "\n") {
			line = strings.TrimSpace(line)
			if condition, ok := strings.CutPrefix(line, "//#if "); ok {
				for _, field := range strings.FieldsFunc(condition, func(r rune) bool {
					return r == '!' || r == '&' || r == '|' || r == '(' || r == ')' || r == ' '
				}) {
					seen[field] = true
				}
				if strings.Contains(condition, "|") {
					anyOfEither = true
				}
			}
			if strings.HasPrefix(line, "//#const ") {
				consts++
			}
		}
	}
	if len(seen) != 2 || !seen["SCENE_SKIN"] || !seen["SCENE_MORPH"] {
		t.Errorf("the split reads the defines %v, want exactly SCENE_SKIN and SCENE_MORPH", seen)
	}
	if !anyOfEither {
		t.Error("no condition combines the two defines; sceneAnim needs `SCENE_SKIN | SCENE_MORPH` rather than a third define")
	}
	if consts != 1 {
		t.Errorf("the split declares %d consts, want the one that had a correctness reason to be shared", consts)
	}
}

// The Go cap and the shader's array can no longer drift: the shader declares its
// own default and scene supplies maxLights over it, so the two are one value.
func TestSceneSuppliesItsOwnLightCap(t *testing.T) {
	if !strings.Contains(flattenedSceneShader(t), "const SCENE_MAX_LIGHTS = 16;") {
		t.Fatalf("the flattened module does not carry scene's cap of %d", maxLights)
	}
}

func lineCountOf(text string) int {
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}
