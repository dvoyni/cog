package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/render"
	"github.com/dvoyni/cog/plugins/render/cmd/declarebindings/plugin"
	"github.com/dvoyni/cog/plugins/render/internal/bindingdeclaration"
)

// mainThread takes the functions the tests run on the main thread, which
// init keeps the main goroutine on.
var mainThread = make(chan func())

func TestMain(m *testing.M) {
	exitCode := make(chan int)
	go func() { exitCode <- m.Run() }()
	for {
		select {
		case run := <-mainThread:
			run()
		case code := <-exitCode:
			os.Exit(code)
		}
	}
}

func TestAShaderWithIncludesIsDeclaredWithASectionForEachDefineSet(t *testing.T) {
	directory := copyTestdata(t)
	defineSets := [][]render.ShaderDefine{
		{{Name: "SKINNED"}},
		{{Name: "JOINT_COUNT", Value: "4"}, {Name: "SKINNED"}},
	}

	if err := runDeclareBindings(plugin.Config{Directory: directory, ShaderPaths: []string{"lit.wgsl"}, DefineSets: defineSets}); err != nil {
		t.Fatal(err)
	}

	files := os.DirFS(directory)
	for _, test := range []struct {
		name    string
		defines []gfx.ShaderDefine
		want    bindingdeclaration.Reflection
	}{
		{"no defines", nil, makeLitReflection(nil, positionInput)},
		{"SKINNED", []gfx.ShaderDefine{{Name: "SKINNED"}}, makeLitReflection(&jointsBinding2, positionInput, weightsInput)},
		{"SKINNED with JOINT_COUNT=4", []gfx.ShaderDefine{{Name: "SKINNED"}, {Name: "JOINT_COUNT", Value: "4"}}, makeLitReflection(&jointsBinding4, positionInput, weightsInput)},
	} {
		reflection, err := bindingdeclaration.Read(files, "lit.wgsl", test.defines)
		if err != nil || !reflect.DeepEqual(reflection, test.want) {
			t.Errorf("%s: the declaration says\n%+v, %v\nwant\n%+v", test.name, reflection, err, test.want)
		}
	}
	declarationJSON, err := os.ReadFile(filepath.Join(directory, "lit.wgsl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declaration struct{ Variants []json.RawMessage }
	if err := json.Unmarshal(declarationJSON, &declaration); err != nil || len(declaration.Variants) != len(defineSets) {
		t.Errorf("the declaration has %d variants, %v, want one for each of the %d define sets", len(declaration.Variants), err, len(defineSets))
	}
}

func TestAShaderRenderRefusesOrADeclarationCannotSpellFailsTheRunWritingNoDeclaration(t *testing.T) {
	for name, source := range map[string]string{
		"failing to compile":  "fn (",
		"refused":             "@group(3) @binding(0) var<uniform> reserved: vec4f;\n@fragment fn fragment_main() -> @location(0) vec4f { return reserved; }\n",
		"of a format unnamed": "@group(0) @binding(0) var written: texture_storage_2d<r8unorm, write>;\n@compute @workgroup_size(1) fn main() { textureStore(written, vec2u(0), vec4f(1)); }\n",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "bad.wgsl"), []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}

			err := runDeclareBindings(plugin.Config{Directory: directory, ShaderPaths: []string{"bad.wgsl"}})

			if err == nil || !strings.Contains(err.Error(), "bad.wgsl") {
				t.Errorf("the run answered %v, want an error naming bad.wgsl", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "bad.wgsl.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a declaration was written: %v", err)
			}
		})
	}
}

func TestAShaderCompilingOnlyWithADefineIsDeclaredForItsVariantsAlone(t *testing.T) {
	directory := t.TempDir()
	source := "#if ARGUMENT\n@group(1) @binding(0) var<uniform> tint: vec4f;\n#endif\n@fragment fn fragment_main() -> @location(0) vec4f { return tint; }\n"
	if err := os.WriteFile(filepath.Join(directory, "argument.wgsl"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	defines := []render.ShaderDefine{{Name: "ARGUMENT"}}

	if err := runDeclareBindings(plugin.Config{Directory: directory, ShaderPaths: []string{"argument.wgsl"}, DefineSets: [][]render.ShaderDefine{defines}, VariantsOnly: true}); err != nil {
		t.Fatal(err)
	}

	files := os.DirFS(directory)
	reflection, err := bindingdeclaration.Read(files, "argument.wgsl", []gfx.ShaderDefine{{Name: "ARGUMENT"}})
	if err != nil || len(reflection.Bindings) != 1 || reflection.Bindings[0].Name != "tint" {
		t.Errorf("with ARGUMENT the declaration says %+v, %v, want the binding tint", reflection, err)
	}
	if _, err := bindingdeclaration.Read(files, "argument.wgsl", nil); err == nil {
		t.Error("with no defines the declaration answered a reflection, want an error, as it declares ARGUMENT's variant alone")
	}
}

func TestAShaderIncludingAFileOfAMountedFolderIsDeclaredWithItsBindings(t *testing.T) {
	// shaders/glow.wgsl, in the folder a game mounts at its root, includes a
	// file of another folder by a path relative to its own, mounted under a
	// prefix beside it, as a 2D material's shader includes canvas's include,
	// mounted under cog.canvas.
	directory := t.TempDir()
	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "camera.wgsl"), []byte("@group(0) @binding(0) var<uniform> camera: vec4f;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "shaders"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "#include ../shared.lib/camera.wgsl\n@fragment fn fragment_main() -> @location(0) vec4f { return camera; }\n"
	if err := os.WriteFile(filepath.Join(directory, "shaders", "glow.wgsl"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDeclareBindings(plugin.Config{Directory: directory, Mounts: []plugin.Mount{{Prefix: "shared.lib", Directory: shared}}, ShaderPaths: []string{"shaders/glow.wgsl"}}); err != nil {
		t.Fatal(err)
	}

	reflection, err := bindingdeclaration.Read(os.DirFS(directory), "shaders/glow.wgsl", nil)
	if err != nil || len(reflection.Bindings) != 1 || reflection.Bindings[0].Name != "camera" {
		t.Errorf("the declaration says %+v, %v, want the included binding camera", reflection, err)
	}
	if _, err := os.Stat(filepath.Join(shared, "camera.wgsl.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a declaration was written into the mounted folder: %v", err)
	}
}

func TestAMountFlagIsAPrefixAndAFolderAndAnythingElseIsRefused(t *testing.T) {
	var mounts mountFlag
	if err := mounts.Set("cog.canvas=../canvas/assets"); err != nil {
		t.Fatal(err)
	}
	if want := (mountFlag{{Prefix: "cog.canvas", Directory: "../canvas/assets"}}); !reflect.DeepEqual(mounts, want) {
		t.Errorf("the flag holds %+v, want %+v", mounts, want)
	}
	if spelt := mounts.String(); spelt != "cog.canvas=../canvas/assets" {
		t.Errorf("the flag is spelt %q, want cog.canvas=../canvas/assets", spelt)
	}
	for _, text := range []string{"cog.canvas", "=assets", "cog.canvas=", ".=assets", "/cog.canvas=assets", "cog.canvas/=assets"} {
		if err := mounts.Set(text); err == nil {
			t.Errorf("-mount %s was taken, want an error", text)
		}
	}
}

// runDeclareBindings runs declareBindings on the main thread, and answers its
// error.
func runDeclareBindings(config plugin.Config) error {
	var err error
	finished := make(chan struct{})
	mainThread <- func() {
		defer close(finished)
		err = declareBindings(config)
	}
	<-finished
	return err
}

// copyTestdata answers a folder of the test's own holding testdata's files,
// so the declarations written there leave testdata as it is.
func copyTestdata(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.CopyFS(directory, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	return directory
}

// jointsBinding2 and jointsBinding4 are lit.wgsl's joints when SKINNED, for a
// JOINT_COUNT of 2 and of 4, and positionInput and weightsInput its vertex
// inputs, the second only when SKINNED.
var (
	jointsBinding2 = gfx.ShaderBinding{
		Name: "joints", Group: 2,
		Buffer:      gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 128},
		Type:        gfx.ShaderType{Size: 128, Element: &gfx.ShaderType{Size: 64}, ElementCount: 2, ElementStride: 64},
		EntryPoints: []string{"vertex_main"},
	}
	jointsBinding4 = gfx.ShaderBinding{
		Name: "joints", Group: 2,
		Buffer:      gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 256},
		Type:        gfx.ShaderType{Size: 256, Element: &gfx.ShaderType{Size: 64}, ElementCount: 4, ElementStride: 64},
		EntryPoints: []string{"vertex_main"},
	}
	positionInput = gfx.VertexInput{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 3}
	weightsInput  = gfx.VertexInput{Location: 1, Type: gfx.ScalarTypeFloat32, Components: 4}
)

// makeLitReflection answers what gfx reflects of lit.wgsl, with joints when it is
// not nil, its vertex entry point reading inputs.
func makeLitReflection(joints *gfx.ShaderBinding, inputs ...gfx.VertexInput) bindingdeclaration.Reflection {
	bindings := []gfx.ShaderBinding{
		{
			Name:   "camera",
			Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 80},
			Type: gfx.ShaderType{Size: 80, Members: []gfx.ShaderStructMember{
				{Name: "view_projection", Offset: 0, Type: gfx.ShaderType{Size: 64}},
				{Name: "exposure", Offset: 64, Type: gfx.ShaderType{Size: 4}},
			}},
			EntryPoints: []string{"vertex_main", "fragment_main"},
		},
		{
			Name: "albedo", Group: 1, Binding: 0,
			Texture:     gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D},
			EntryPoints: []string{"fragment_main"},
		},
		{
			Name: "albedo_sampler", Group: 1, Binding: 1,
			Sampler:     gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering},
			EntryPoints: []string{"fragment_main"},
		},
	}
	if joints != nil {
		bindings = append(bindings, *joints)
	}
	return bindingdeclaration.Reflection{
		Bindings:     bindings,
		EntryPoints:  []gfx.ShaderEntryPoint{{Name: "vertex_main", Stage: gfx.ShaderStageFlagVertex}, {Name: "fragment_main", Stage: gfx.ShaderStageFlagFragment}},
		VertexInputs: map[string][]gfx.VertexInput{"vertex_main": inputs},
	}
}
