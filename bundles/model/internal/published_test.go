package internal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
)

// published is every source a custom material may include, by the name of the
// constant that spells its storage path. The other eight stay private: what
// they declare can change with the bundled shader and nothing outside model
// may name it.
var published = map[string]string{
	"VertexDecodePath": model.VertexDecodePath,
	"FramePath":        model.FramePath,
	"PbrPath":          model.PbrPath,
}

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
	var names []string
	for _, line := range strings.Split(string(source), "\n") {
		match := topLevelDeclaration.FindStringSubmatch(strings.TrimRight(line, "\r"))
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
	for _, file := range []string{"../types.go", "types/shader.go"} {
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
	for _, includes := range [][]string{{model.PbrPath}, {model.FramePath, model.PbrPath}} {
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
		if lights != model.MaxLights {
			t.Errorf("an includer's sceneFrame carries %d lights, want model.MaxLights = %d",
				lights, model.MaxLights)
		}
	}
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
