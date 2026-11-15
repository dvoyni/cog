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

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/internal/bindingdeclaration"
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

	if err := runDeclareBindings(directory, []string{"lit.wgsl"}, defineSets); err != nil {
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

			err := runDeclareBindings(directory, []string{"bad.wgsl"}, nil)

			if err == nil || !strings.Contains(err.Error(), "bad.wgsl") {
				t.Errorf("the run answered %v, want an error naming bad.wgsl", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "bad.wgsl.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a declaration was written: %v", err)
			}
		})
	}
}

// runDeclareBindings runs declareBindings on the main thread, and answers its
// error.
func runDeclareBindings(directory string, shaderPaths []string, defineSets [][]render.ShaderDefine) error {
	var err error
	finished := make(chan struct{})
	mainThread <- func() {
		defer close(finished)
		err = declareBindings(directory, shaderPaths, defineSets)
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
		EntryPoints: []string{"vertex_main"},
	}
	jointsBinding4 = gfx.ShaderBinding{
		Name: "joints", Group: 2,
		Buffer:      gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 256},
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
			Members: []gfx.ShaderStructMember{
				{Name: "view_projection", Offset: 0, Size: 64},
				{Name: "exposure", Offset: 64, Size: 4},
			},
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
