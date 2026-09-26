package internal

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// compileFixture fills gfx's backend Port with a real gfxBackend that has no
// device, which is all the reflection port needs: CompileShaderCmd must work
// before the device exists, on any goroutine.
type compileFixture struct{ backend *gfxBackend }

// compileGfxBackend is the Adapter compileFixture fills gfx's backend Port as.
type compileGfxBackend kernel.Adapter[gfx.BackendPort]

func (compileFixture) Name() kernel.PluginName           { return "compile-test-backend" }
func (compileFixture) Dependencies() []kernel.PluginName { return nil }
func (f compileFixture) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[compileGfxBackend](gfx.Backend(f.backend))
	return nil
}

const compileRoot = `#include ./frame.wgsl
struct Material { tint: vec4<f32>, roughness: f32 }
@group(1) @binding(0) var<uniform> material: Material;
@group(1) @binding(1) var albedo: texture_2d<f32>;
@group(1) @binding(2) var albedoSampler: sampler;
struct Instances { models: array<mat4x4<f32>> }
@group(2) @binding(0) var<storage, read> instances: Instances;

@vertex fn vs_main(@location(0) position: vec3<f32>, @builtin(instance_index) i: u32) -> @builtin(position) vec4<f32> {
	return frame.viewProjection * instances.models[i] * vec4<f32>(position, 1.0) * material.tint.x;
}

@fragment fn fs_main() -> @location(0) vec4<f32> {
	return textureSample(albedo, albedoSampler, vec2<f32>(0.0, 0.0));
}
`

const compileFrame = `struct Frame { viewProjection: mat4x4<f32> }
@group(0) @binding(0) var<uniform> frame: Frame;
`

// Real WGSL through the real port: gfx flattens the root with its include,
// gogpu reflects it with naga, and gfx tables what naga found by global name.
func TestCompileShaderReflectsRealWGSLThroughNaga(t *testing.T) {
	files := fstest.MapFS{
		"shaders/lit.wgsl":   {Data: []byte(compileRoot)},
		"shaders/frame.wgsl": {Data: []byte(compileFrame)},
	}
	engine := kernel.New(nil).Handler(func(err error) error {
		t.Errorf("unexpected report: %v", err)
		return err
	}).WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", FS: files}},
		appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), compileFixture{backend: newGfxBackend()})
	go engine.Run()
	t.Cleanup(engine.Quit)
	<-engine.Ready()
	k := engine.Executioner()

	response := k.ExecuteCommand[gfx.CompileShaderCmd](gfx.CompileShaderRequest{
		FS: files, Descr: gfx.ShaderWithResource("shaders/lit.wgsl"),
	})
	if response.Err != nil {
		t.Fatalf("compile failed: %v", response.Err)
	}

	want := []gfx.ShaderResource{
		{Name: "frame", Kind: gfx.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
		{Name: "material", Kind: gfx.ResourceUniformBuffer, Group: 1, Binding: 0, Size: 32},
		{Name: "albedo", Kind: gfx.ResourceTexture, Group: 1, Binding: 1},
		{Name: "albedoSampler", Kind: gfx.ResourceSampler, Group: 1, Binding: 2},
		{Name: "instances", Kind: gfx.ResourceStorageBuffer, Group: 2, Binding: 0},
	}
	for _, w := range want {
		got, ok := response.Program.Binding(w.Name)
		if !ok || got.Kind != w.Kind || got.Group != w.Group || got.Binding != w.Binding || got.Size != w.Size {
			t.Errorf("Binding(%q) = %+v, %v, want %+v", w.Name, got, ok, w)
		}
	}
	if _, ok := response.Program.Binding("position"); ok {
		t.Error("a vertex input was tabled as a binding")
	}

	broken := k.ExecuteCommand[gfx.CompileShaderCmd](gfx.CompileShaderRequest{
		Descr: gfx.ShaderWithText("this is not WGSL"),
	})
	var source gfx.ErrShaderSource
	if broken.Err == nil || broken.Program.Valid() {
		t.Fatalf("WGSL naga refuses compiled: %+v", broken)
	}
	if !errors.As(broken.Err, &source) {
		t.Errorf("Err = %v, want an ErrShaderSource", broken.Err)
	}
}

// ReserveShader needs no device, and its ids are never reused.
func TestReserveShaderMintsDistinctIDsWithoutADevice(t *testing.T) {
	backend := newGfxBackend()
	first, second := backend.ReserveShader(), backend.ReserveShader()
	if first == 0 || second <= first {
		t.Errorf("reserved %d then %d, want nonzero ids counting up", first, second)
	}
}
