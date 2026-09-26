package internal

import (
	"bytes"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/wgsl"
	"github.com/qmuntal/gltf"
)

// These run a model load the way a renderer's load System does: through the
// device facade, inside a handler holding the Lookup, the filesystem and the
// resource queue and declaring CompileShaderCmd, against the real gfx. The
// backend reflects through naga, so a set is resolved against the bundled
// shader's real layout and a ScenePbrMaterial of the wrong size is reported.

const (
	crateGLBPath  = "models/crate.glb"
	barrelGLBPath = "models/barrel.glb"
)

// paintedGLB is one triangle under one material, which is all a load needs to
// create a material's sets.
func paintedGLB(t testing.TB) []byte {
	t.Helper()
	doc := testDoc()
	doc.Materials = []*gltf.Material{{Name: "paint"}}
	triangleMesh(doc, gltf.Index(0))
	doc.Nodes = []*gltf.Node{{Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	var buffer bytes.Buffer
	if err := gltf.NewEncoder(&buffer).Encode(doc); err != nil {
		t.Fatalf("encoding the model: %v", err)
	}
	return buffer.Bytes()
}

// setsCmd is one handler's worth of the facade: load some paths, unload some,
// and drain. The compiles it made are counted per call, in its response,
// because a handler keeps no state of its own.
type setsCmd kernel.Command[setsRequest, setsResponse]

type setsRequest struct {
	load   []string
	unload []string
	drain  bool
	// broken compiles through a compiler whose every compile fails.
	broken bool
}

type setsResponse struct {
	// sets are each loaded path's materials' sets, in material order.
	sets     map[string][][VariantCount]gfx.DrawParams
	compiles int
	// released are the sets the drain handed its baker.
	released []gfx.DrawParams
}

// setsProbe registers setsCmd and hands gfx its backend.
type setsProbe struct{ backend gfx.Backend }

type setsGfxBackend kernel.Adapter[gfx.BackendPort]

func (setsProbe) Name() kernel.PluginName { return "model-test-sets-probe" }
func (setsProbe) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{storage.Name, gfx.Name, Name}
}

func (p setsProbe) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[setsGfxBackend](gfx.Backend(p.backend))
	registrar.HandleCommand[setsCmd](func() (kernel.Lock, kernel.Execute[setsRequest, setsResponse]) {
		var (
			lookup    kernel.Write[*Lookup]
			files     kernel.Read[storage.FileSystem]
			resources kernel.Write[*gfx.ResourceQueue]
			compile   gfx.ShaderCompiler
		)
		return func(access kernel.ResourceAccess) {
				lookup = access.GetWrite[*Lookup]()
				files = access.GetRead[storage.FileSystem]()
				resources = access.GetWrite[*gfx.ResourceQueue]()
				compile = access.Uses[gfx.CompileShaderCmd]()
			}, func(k kernel.Kernel, request setsRequest) setsResponse {
				response := setsResponse{sets: map[string][][VariantCount]gfx.DrawParams{}}
				counted := func(k kernel.Kernel, compileRequest gfx.CompileShaderRequest) gfx.CompileShaderResponse {
					response.compiles++
					if request.broken {
						return gfx.CompileShaderResponse{Err: errors.New("the shader is broken")}
					}
					return compile(k, compileRequest)
				}
				queue := resources.Get()
				device := NewLookupDeviceAccess(k, lookup.Get(), fs.FS(files.Get()), queue, counted)
				for _, path := range request.load {
					handle, ok := device.Resolve(ModelRef{Path: path})
					if !ok {
						continue
					}
					view, _, _ := NewLookupReadAccess(lookup.Get()).View(handle, "", "")
					for _, material := range view.Materials {
						response.sets[path] = append(response.sets[path], material.Sets)
					}
				}
				for _, path := range request.unload {
					NewLookupAccess(k, lookup.Get()).UnloadModel(path)
				}
				if request.drain {
					lookup.Get().DrainMeshes(MeshBaker{
						Bake: func(data []byte) gfx.BufferDescr { return queue.UploadBuffer(queue.NewBuffer(), data, false) },
						Rebake: func(buffer gfx.BufferDescr, data []byte) gfx.BufferDescr {
							return queue.UploadBuffer(buffer, data, false)
						},
						Release: queue.ReleaseBuffer,
						ReleaseDrawParams: func(set gfx.DrawParams) {
							response.released = append(response.released, set)
							queue.ReleaseDrawParams(k, set)
						},
					})
				}
				return response
			}
	})
	return nil
}

// nagaBackend is flattenBackend reflecting through naga, as gogpu does: every
// buffer binding by its address space, a uniform with its size, and every
// texture and sampler.
type nagaBackend struct{ flattenBackend }

func (*nagaBackend) ReflectShader(code []byte) (gfx.ShaderLayout, error) {
	parsed, err := naga.Parse(string(code))
	if err != nil {
		return gfx.ShaderLayout{}, err
	}
	module, err := wgsl.Lower(parsed)
	if err != nil {
		return gfx.ShaderLayout{}, err
	}
	var layout gfx.ShaderLayout
	for _, global := range module.GlobalVariables {
		if global.Binding == nil {
			continue
		}
		resource := gfx.ShaderResource{
			Name: global.Name, Group: int(global.Binding.Group), Binding: int(global.Binding.Binding),
		}
		switch global.Space {
		case ir.SpaceUniform:
			resource.Kind, resource.Size = gfx.ResourceUniformBuffer, int(ir.TypeSize(module, global.Type))
		case ir.SpaceStorage:
			resource.Kind = gfx.ResourceStorageBuffer
		default:
			switch inner := module.Types[global.Type].Inner.(type) {
			case ir.ImageType:
				resource.Kind = gfx.ResourceTexture
				if inner.Arrayed {
					resource.TextureView = gfx.TextureView2DArray
				}
			case ir.SamplerType:
				resource.Kind = gfx.ResourceSampler
			default:
				continue
			}
		}
		layout.Resources = append(layout.Resources, resource)
	}
	return layout, nil
}

// setsEngine composes storage with both models mounted, gfx over the naga
// backend, model and the probe, and collects everything the engine reports.
func setsEngine(t *testing.T) (kernel.Executioner, func() []error) {
	t.Helper()
	files := fstest.MapFS{
		crateGLBPath:  {Data: paintedGLB(t)},
		barrelGLBPath: {Data: paintedGLB(t)},
	}
	var (
		mu     sync.Mutex
		errs   []error
		backed = &nagaBackend{}
	)
	engine := kernel.New(nil).
		Handler(func(err error) error { mu.Lock(); errs = append(errs, err); mu.Unlock(); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{},
			readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: files}},
			appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), New(), setsProbe{backend: backed})
	stopped := make(chan struct{})
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	return engine.Executioner(), func() []error {
		mu.Lock()
		defer mu.Unlock()
		return append([]error(nil), errs...)
	}
}

// The first load compiles the bundled shader's four variants and every load
// after it builds on the same four: two models are four compiles, not eight,
// and each model's material has a set of its own for every variant.
func TestLoadingTwoModelsCompilesTheBundledShaderOnce(t *testing.T) {
	k, reported := setsEngine(t)
	first := k.ExecuteCommand[setsCmd](setsRequest{load: []string{crateGLBPath}})
	second := k.ExecuteCommand[setsCmd](setsRequest{load: []string{barrelGLBPath}})

	if first.compiles != VariantCount || second.compiles != 0 {
		t.Errorf("the loads compiled %d and %d times, want %d for the first and none after", first.compiles, second.compiles, VariantCount)
	}
	seen := map[gfx.DrawParams]bool{}
	for _, response := range []setsResponse{first, second} {
		for path, materials := range response.sets {
			if len(materials) != 1 {
				t.Fatalf("%s loaded %d materials, want its one", path, len(materials))
			}
			for variant, set := range materials[0] {
				if set == (gfx.DrawParams{}) {
					t.Errorf("%s has no set for variant %d", path, variant)
				}
				if seen[set] {
					t.Errorf("%s's variant %d shares a set with another material", path, variant)
				}
				seen[set] = true
			}
		}
	}
	if len(seen) != 2*VariantCount {
		t.Errorf("the two models hold %d sets, want %d", len(seen), 2*VariantCount)
	}
	for _, err := range reported() {
		t.Errorf("the loads reported: %v", err)
	}
}

// Releasing a model releases its sets: unloading it queues them, and the drain
// hands exactly those to its baker, the other model's staying live.
func TestUnloadingAModelReleasesItsSets(t *testing.T) {
	k, reported := setsEngine(t)
	loaded := k.ExecuteCommand[setsCmd](setsRequest{load: []string{crateGLBPath, barrelGLBPath}})
	unloaded := k.ExecuteCommand[setsCmd](setsRequest{unload: []string{crateGLBPath}})
	if len(unloaded.released) != 0 {
		t.Fatalf("the unload released %v before any drain, want the sets held for the frame boundary", unloaded.released)
	}
	drained := k.ExecuteCommand[setsCmd](setsRequest{drain: true})

	want := map[gfx.DrawParams]bool{}
	for _, set := range loaded.sets[crateGLBPath][0] {
		want[set] = true
	}
	if len(drained.released) != len(want) {
		t.Errorf("the drain released %d sets, want the unloaded model's %d", len(drained.released), len(want))
	}
	for _, set := range drained.released {
		if !want[set] {
			t.Errorf("the drain released %v, which is not the unloaded model's", set)
		}
	}
	if again := k.ExecuteCommand[setsCmd](setsRequest{drain: true}); len(again.released) != 0 {
		t.Errorf("a second drain released %v again", again.released)
	}
	for _, err := range reported() {
		t.Errorf("the unload reported: %v", err)
	}
}

// A variant that does not compile is reported once and not compiled again: the
// model still loads, its sets stay zero, and a later load finds the failure
// remembered rather than compiling a frame.
func TestABundledShaderThatDoesNotCompileIsReportedOnceAndLeavesTheSetsZero(t *testing.T) {
	k, reported := setsEngine(t)
	first := k.ExecuteCommand[setsCmd](setsRequest{load: []string{crateGLBPath}, broken: true})
	second := k.ExecuteCommand[setsCmd](setsRequest{load: []string{barrelGLBPath}, broken: true})

	if first.compiles != VariantCount || second.compiles != 0 {
		t.Errorf("the loads compiled %d and %d times, want %d for the first and none after", first.compiles, second.compiles, VariantCount)
	}
	for _, response := range []setsResponse{first, second} {
		for path, materials := range response.sets {
			if len(materials) != 1 || materials[0] != ([VariantCount]gfx.DrawParams{}) {
				t.Errorf("%s loaded with sets %v, want one material with none", path, materials)
			}
		}
		if len(response.sets) != 1 {
			t.Errorf("a load loaded %d models, want the one it named despite the shader", len(response.sets))
		}
	}
	variants := map[ShaderVariant]bool{}
	for _, err := range reported() {
		var unavailable ErrSceneShaderUnavailable
		if !errors.As(err, &unavailable) {
			t.Errorf("the loads reported: %v", err)
			continue
		}
		if variants[unavailable.Variant] {
			t.Errorf("variant %d was reported twice", unavailable.Variant)
		}
		variants[unavailable.Variant] = true
	}
	if len(variants) != VariantCount {
		t.Errorf("%d variants were reported, want all %d", len(variants), VariantCount)
	}
}
