package internal

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// published is every source a custom material may include, by the name of the
// constant that spells its storage path. The rest stay private: what
// they declare can change with the bundled shader and nothing outside model
// may name it.
var published = map[string]string{
	"VertexDecodePath":  VertexDecodePath,
	"FramePath":         FramePath,
	"PbrPath":           PbrPath,
	"VertexStagePath":   VertexStagePath,
	"FragmentStagePath": FragmentStagePath,

	"MaterialProloguePath": MaterialProloguePath,
	"MaterialFieldsPath":   MaterialFieldsPath,
	"MaterialEpiloguePath": MaterialEpiloguePath,
}

// fieldDeclaration matches a member of the material's uniform block in its
// fields source, which is the inside of a struct and so declares at no column
// zero: an indented name and a colon.
var fieldDeclaration = regexp.MustCompile(`^\s+(\w+)\s*:`)

// topLevelDeclaration matches a module-scope name a source declares: a struct,
// a function, a constant, a binding, or a //#const default. A line indented at
// all is inside something else, so only column zero counts.
var topLevelDeclaration = regexp.MustCompile(
	`^(?:(?:struct|fn|const|alias|override)\s+(\w+)|@group\(\d+\)\s*@binding\(\d+\)\s*var(?:<[^>]*>)?\s+(\w+)|//#const\s+(\w+)=)`)

// declaredBy is every module-scope name one published source declares itself,
// not counting what it includes.
func declaredBy(t *testing.T, name string) []string {
	t.Helper()
	source, err := shaderFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	pattern := topLevelDeclaration
	if name == MaterialFieldsPath {
		// The members are the names an extension must not declare again.
		pattern = fieldDeclaration
	}
	var names []string
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		match := pattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		for _, group := range match[1:] {
			if group != "" {
				names = append(names, group)
			}
		}
	}
	if len(names) == 0 {
		t.Fatalf("%s declares nothing the pattern recognises", name)
	}
	return names
}

// publishedDocs is each published constant's doc comment in one Go file,
// read out of the source that declares it. The doc is the contract an includer
// reads, so it is what has to name everything the source brings in. There are
// two: model's own, which is what godoc shows a caller, and the internal
// declaration's, which carries the reasoning.
func publishedDocs(t *testing.T, file string) map[string]string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	docs := map[string]string{}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for _, ident := range value.Names {
				if _, ok := published[ident.Name]; ok && value.Doc != nil {
					docs[ident.Name] = value.Doc.Text()
				}
			}
		}
	}
	return docs
}

// mentions reports whether text names identifier as a whole word, so
// sceneSun is not found inside sceneSunColor.
func mentions(text, identifier string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\b`).MatchString(text)
}

// A published source is a contract in both directions: an includer may call
// what it declares and must not declare those names again. So the constant's
// doc names every one of them, and so does the source's own DECLARES header,
// which is what someone reading the WGSL rather than the Go sees first. A name
// added to the source and to neither is a collision waiting in someone's
// material.
func TestEveryPublishedSourceNamesWhatItDeclares(t *testing.T) {
	for _, file := range []string{"../types.go", "shader.go"} {
		docs := publishedDocs(t, file)
		for constant, name := range published {
			doc, ok := docs[constant]
			if !ok {
				t.Errorf("%s: %s has no doc comment", file, constant)
				continue
			}
			// The doc wraps wherever gofmt left it, so the sentence is looked
			// for with its whitespace folded.
			if folded := strings.ToLower(strings.Join(strings.Fields(doc), " ")); !strings.Contains(folded, "do not declare those names again") {
				t.Errorf("%s: %s's doc does not say \"do not declare those names again\"", file, constant)
			}
			for _, declared := range declaredBy(t, name) {
				if !mentions(doc, declared) {
					t.Errorf("%s declares %s, which %s's doc in %s does not name", name, declared, constant, file)
				}
			}
		}
	}
	for _, name := range published {
		source, err := shaderFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		header, _, _ := strings.Cut(string(source), "\n\n")
		if !strings.Contains(header, "DECLARES:") {
			t.Errorf("%s opens with no DECLARES header", name)
		}
		for _, declared := range declaredBy(t, name) {
			if !mentions(header, declared) {
				t.Errorf("%s declares %s, which its DECLARES header does not name", name, declared)
			}
		}
	}
}

// The three published paths are the storage names the sources are mounted at,
// so an absolute include of each resolves, and the prelude pair declares the one
// binding every renderer fills: an includer of PbrPath gets FramePath with it,
// once, and a light array the size of model.MaxLights without supplying
// anything - the //#const default and Go's cap are the same number.
func TestAnIncluderOfThePreludeGetsOneBindingAndMaxLights(t *testing.T) {
	// The second list names FramePath as well, ahead of PbrPath's own include
	// of it, which is include-once doing its job: one sceneFrame, not two.
	for _, includes := range [][]string{{PbrPath}, {FramePath, PbrPath}} {
		var directives strings.Builder
		for _, include := range includes {
			directives.WriteString("//#include " + include + "\n")
		}
		text, err := flattenShader(t, shaderMountID, shaderFS,
			gfx.ShaderWithText(directives.String()+preludeIncluder))
		if err != nil {
			t.Fatalf("flatten an includer of %v: %v", includes, err)
		}
		module := lowerForTest(t, text)
		var bindings []string
		lights := -1
		for _, global := range module.GlobalVariables {
			if global.Binding == nil {
				continue
			}
			bindings = append(bindings, global.Name)
			if global.Name != "sceneFrame" {
				continue
			}
			if global.Space != ir.SpaceStorage || global.Binding.Group != 0 || global.Binding.Binding != 0 {
				t.Errorf("sceneFrame is space %v at %d/%d, want storage at 0/0",
					global.Space, global.Binding.Group, global.Binding.Binding)
			}
			frame, ok := module.Types[global.Type].Inner.(ir.StructType)
			if !ok {
				t.Fatalf("sceneFrame is not a struct")
			}
			for _, member := range frame.Members {
				if array, ok := module.Types[member.Type].Inner.(ir.ArrayType); ok &&
					member.Name == "lights" && array.Size.Constant != nil {
					lights = int(*array.Size.Constant)
				}
			}
		}
		if len(bindings) != 1 || bindings[0] != "sceneFrame" {
			t.Errorf("an includer of %v declares %v, want sceneFrame alone", includes, bindings)
		}
		if lights != MaxLights {
			t.Errorf("an includer's sceneFrame carries %d lights, want model.MaxLights = %d",
				lights, MaxLights)
		}
	}
}

// An app shader is the bundled PBR plus one step: it includes the two published
// stages, declares its own binding in group 3 and writes a fs_main around
// scenePbrFragment. It flattens and lowers under every variant the renderer
// may add, which is what "the variant is cog's job" needs of a shader cog did
// not write, and it declares exactly the bundled module's bindings plus its
// own - nothing copied, nothing extra.
func TestAnAppShaderOverTheTwoStagesBuildsUnderEveryVariant(t *testing.T) {
	for variant := range VariantCount {
		shader := VariantShader(gfx.ShaderWithText(stageIncluder), ShaderVariant(variant))
		text, err := flattenShader(t, shaderMountID, shaderFS, shader)
		if err != nil {
			t.Fatalf("variant %d: flatten the app shader: %v", variant, err)
		}
		bundled, err := flattenShader(t, shaderMountID, shaderFS,
			VariantShader(gfx.ShaderDescr{}, ShaderVariant(variant)))
		if err != nil {
			t.Fatalf("variant %d: flatten the bundled shader: %v", variant, err)
		}
		want := append(bindingsOf(lowerForTest(t, bundled)), "3/0 sightDepths")
		if got := bindingsOf(lowerForTest(t, text)); strings.Join(got, ", ") != strings.Join(want, ", ") {
			t.Errorf("variant %d: the app shader declares\n%v\nwant the bundled module's and its own\n%v",
				variant, got, want)
		}
	}
}

// stageIncluder is the consuming game's fade, written as the ticket spells it.
const stageIncluder = `//#include builtin/model/vertexstage.wgsl
//#include builtin/model/fragmentstage.wgsl
@group(3) @binding(0) var sightDepths: texture_2d<f32>;

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4<f32> {
    let lit = scenePbrFragment(in, ff);
    let visibility = textureLoad(sightDepths, vec2<i32>(in.worldPosition.xz), 0).r;
    return vec4<f32>(mix(vec3<f32>(0.0), lit.rgb, visibility), lit.a);
}
`

// bindingsOf lists a module's bindings as group/binding name, in declaration
// order.
func bindingsOf(module *ir.Module) []string {
	var out []string
	for _, global := range module.GlobalVariables {
		if global.Binding != nil {
			out = append(out, fmt.Sprintf("%d/%d %s", global.Binding.Group, global.Binding.Binding, global.Name))
		}
	}
	return out
}

// preludeIncluder is the least material that reads the prelude: a vertex stage
// placing a point through the frame, and a fragment stage shading a fixed
// surface with the engine's own lighting.
const preludeIncluder = `
@vertex
fn vs_main(@location(0) position: vec3<f32>) -> @builtin(position) vec4<f32> {
    return sceneFrame.viewProjection * vec4<f32>(position, 1.0);
}

@fragment
fn fs_main(@builtin(position) at: vec4<f32>) -> @location(0) vec4<f32> {
    let s = SceneSurface(at.xyz, vec3<f32>(0.0, 1.0, 0.0), vec3<f32>(0.5, 0.5, 0.5), 0.0, 0.8, 1.0);
    return vec4<f32>(sceneShadeSurface(s), 1.0);
}
`

func lowerForTest(t *testing.T, text string) *ir.Module {
	t.Helper()
	parsed, err := naga.Parse(text)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	return module
}

// A shader with per-draw numbers of its own adds them to the material's one
// uniform block: it composes the block from the published prologue, a fields
// source of its own over the published fields, and the published epilogue,
// then includes the two stages. The material's own three includes are then
// skipped as already seen, so the module declares exactly the bundled
// module's bindings, and the block holds every bundled member at the offset
// the bundled block holds it, then the extension's. A second extension over
// the first stacks the same way.
func TestAShaderExtendsTheMaterialBlockWithNumbersOfItsOwn(t *testing.T) {
	for _, c := range []struct {
		name  string
		files map[string]string
		root  string
		extra []string
	}{
		{"one extension", map[string]string{"game/fadefields.wgsl": fadeFields}, fadeShader,
			[]string{"fadeRun", "fadeBias"}},
		{"an extension over it", map[string]string{
			"game/fadefields.wgsl": fadeFields, "game/tintfields.wgsl": tintFields,
		}, tintShader, []string{"fadeRun", "fadeBias", "tintColor"}},
	} {
		for variant := range VariantCount {
			files := fstest.MapFS{}
			for name, text := range c.files {
				files[name] = &fstest.MapFile{Data: []byte(text)}
			}
			text, err := flattenShader(t, shaderMountID, overlayFS{shaderFS, files},
				VariantShader(gfx.ShaderWithText(c.root), ShaderVariant(variant)))
			if err != nil {
				t.Fatalf("%s, variant %d: flatten: %v", c.name, variant, err)
			}
			bundled, err := flattenShader(t, shaderMountID, shaderFS,
				VariantShader(gfx.ShaderDescr{}, ShaderVariant(variant)))
			if err != nil {
				t.Fatalf("%s, variant %d: flatten the bundled shader: %v", c.name, variant, err)
			}
			module, reference := lowerForTest(t, text), lowerForTest(t, bundled)
			// The block is declared first, since it is composed first; which
			// bindings there are is the contract, not the order they come in.
			if got, want := strings.Join(slices.Sorted(slices.Values(bindingsOf(module))), ", "),
				strings.Join(slices.Sorted(slices.Values(bindingsOf(reference))), ", "); got != want {
				t.Errorf("%s, variant %d: declares\n%v\nwant the bundled module's\n%v", c.name, variant, got, want)
			}
			got, want := blockOf(t, module), blockOf(t, reference)
			for i, member := range want {
				if i >= len(got) || got[i] != member {
					t.Fatalf("%s, variant %d: the block is\n%v\nwant it to open with the bundled\n%v", c.name, variant, got, want)
				}
			}
			var added []string
			for _, member := range got[len(want):] {
				added = append(added, strings.Fields(member)[0])
			}
			if strings.Join(added, " ") != strings.Join(c.extra, " ") {
				t.Errorf("%s, variant %d: the block adds %v, want %v", c.name, variant, added, c.extra)
			}
		}
	}
}

// Composed after the material instead of before, an extension does not
// compile: the block is already declared and closed, and the extension's
// members land outside any struct. The mistake is loud.
func TestAnExtensionComposedTooLateDoesNotCompile(t *testing.T) {
	files := fstest.MapFS{"game/fadefields.wgsl": &fstest.MapFile{Data: []byte(fadeFields)}}
	text, err := flattenShader(t, shaderMountID, overlayFS{shaderFS, files},
		gfx.ShaderWithText(lateFadeShader))
	if err != nil {
		return
	}
	if parsed, err := naga.Parse(text); err == nil {
		if _, err := wgsl.Lower(parsed); err == nil {
			t.Error("an extension composed after the material compiled")
		}
	}
}

// blockOf lists the members of a module's scenePbrMaterial block as
// "name offset", in order.
func blockOf(t *testing.T, module *ir.Module) []string {
	t.Helper()
	for _, global := range module.GlobalVariables {
		if global.Name != "scenePbrMaterial" {
			continue
		}
		block, ok := module.Types[global.Type].Inner.(ir.StructType)
		if !ok || global.Space != ir.SpaceUniform {
			t.Fatalf("scenePbrMaterial is not a uniform struct")
		}
		var out []string
		for _, member := range block.Members {
			out = append(out, fmt.Sprintf("%s %d", member.Name, member.Offset))
		}
		return out
	}
	t.Fatal("no scenePbrMaterial")
	return nil
}

// overlayFS reads a game's own sources beside the embedded ones, as the storage
// mounts do at runtime.
type overlayFS struct {
	base, over fs.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.over.Open(name); err == nil {
		return f, nil
	}
	return o.base.Open(name)
}

const fadeFields = `//#include builtin/model/materialfields.wgsl
    fadeRun: vec4<f32>,
    fadeBias: f32,
`

const tintFields = `//#include game/fadefields.wgsl
    tintColor: vec4<f32>,
`

const fadeShader = `//#include builtin/model/materialprologue.wgsl
//#include game/fadefields.wgsl
//#include builtin/model/materialepilogue.wgsl
//#include builtin/model/vertexstage.wgsl
//#include builtin/model/fragmentstage.wgsl

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4<f32> {
    let lit = scenePbrFragment(in, ff);
    return vec4<f32>(lit.rgb * scenePbrMaterial.fadeRun.x + scenePbrMaterial.fadeBias, lit.a);
}
`

const tintShader = `//#include builtin/model/materialprologue.wgsl
//#include game/tintfields.wgsl
//#include builtin/model/materialepilogue.wgsl
//#include builtin/model/vertexstage.wgsl
//#include builtin/model/fragmentstage.wgsl

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4<f32> {
    let lit = scenePbrFragment(in, ff);
    return vec4<f32>(lit.rgb * scenePbrMaterial.tintColor.rgb * scenePbrMaterial.fadeBias, lit.a);
}
`

const lateFadeShader = `//#include builtin/model/vertexstage.wgsl
//#include builtin/model/fragmentstage.wgsl
//#include builtin/model/materialprologue.wgsl
//#include game/fadefields.wgsl
//#include builtin/model/materialepilogue.wgsl

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4<f32> {
    return scenePbrFragment(in, ff);
}
`
