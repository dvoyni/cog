package internal

// Research-only: the whole-frame arms for "where ecsscene's frame time goes
// today". The five arms in recordbench_test.go have no camera, no resident
// model and a backend that is never Ready, so scene's flush returns before it
// culls, sorts, packs or reaches gfx. These arms draw: a camera, the crate on
// disk and resident, a Ready stub backend, 5 000 Entities in a grid the camera
// sees whole.
//
// BenchmarkDirect* is the same frame with no ECS at all: a plugin System
// records the same 5 000 Model calls into scene's queue straight from a Go
// slice, so the difference between the two is what the ECS walk and the
// binding's copy-out cost.

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

const (
	gridColumns = 100
	gridRows    = 50
	gridStep    = 0.5
)

var baselineCamera = scene.LookAt(m.Vec3{Z: 80}, m.Vec3{}, m.Vec3{Y: 1})

// gridPlace is where the i-th of the 5 000 stands: the same grid scene's own
// BenchmarkFrame records, which a camera at Z=80 sees whole.
func gridPlace(row int) ecsscene.Transform {
	return ecsscene.Transform{Position: m.Vec3{
		X: -float32(gridColumns) * gridStep / 2,
		Y: float32(row)*gridStep - float32(gridRows)*gridStep/2,
	}}
}

// newDrawnHarness is the drawing harness with 5 000 Entities of the arm's shape
// in view, run until every one is packed into the pass.
func newDrawnHarness(b *testing.B, arm population, mesh bool) *harness {
	b.Helper()
	files := fstest.MapFS{crateModel: &fstest.MapFile{Data: crateGLB(b)}}
	h := newHarnessWith(b, files, uint32(gridColumns*gridRows)+8, &stubBackend{})
	h.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	h.spawn(b, spawnRequest{
		Place:  ecsscene.Transform(baselineCamera),
		Camera: &ecsscene.Camera{FovY: 1.0472, Near: 0.1, Far: 200},
	})
	var ref scene.MeshRef
	if mesh {
		ref = h.bake(b)
	}
	for row := range gridRows {
		request := spawnRequest{Count: gridColumns, Step: gridStep, Place: gridPlace(row)}
		if mesh {
			request.Mesh = &ecsscene.Mesh{Ref: ref}
		} else {
			request.Model = crateModelComponent()
		}
		if arm.params {
			request.Params = &benchParams
		}
		if arm.material {
			request.Material = &benchMaterial
		}
		h.spawn(b, request)
	}
	want := gridColumns * gridRows
	h.frameUntil(b, "every Entity to be packed", func() bool {
		passes := h.passes(b)
		return len(passes) >= 1 && passes[0].Instances == want
	})
	for range 100 {
		h.frame(b)
	}
	return h
}

func report(b *testing.B, h *harness) {
	passes := h.passes(b)
	instances, batches := 0, 0
	for _, p := range passes {
		instances += p.Instances
		batches += len(p.Batches)
	}
	b.ReportMetric(float64(len(passes)), "passes/frame")
	b.ReportMetric(float64(instances), "instances/frame")
	b.ReportMetric(float64(batches), "batches/frame")
	for _, err := range h.errs.snapshot() {
		b.Fatalf("the frame reported %v", err)
	}
}

func runFrames(b *testing.B, h *harness) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.frame(b)
	}
	b.StopTimer()
	report(b, h)
}

func BenchmarkDrawnModel5000(b *testing.B) {
	runFrames(b, newDrawnHarness(b, population{n: 5000}, false))
}

func BenchmarkDrawnModelParams5000(b *testing.B) {
	runFrames(b, newDrawnHarness(b, population{n: 5000, params: true}, false))
}

func BenchmarkDrawnModelMaterial5000(b *testing.B) {
	runFrames(b, newDrawnHarness(b, population{n: 5000, material: true}, false))
}

func BenchmarkDrawnMesh5000(b *testing.B) {
	runFrames(b, newDrawnHarness(b, population{n: 5000}, true))
}

// directOnUpdate records the same frame as the binding with no ECS under it.
type directOnUpdate kernel.Subscription[app.UpdateEvent]

type directPlugin struct {
	places    []scene.Transform
	instanced bool
}

func (*directPlugin) Name() kernel.PluginName { return "direct" }

func (*directPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name}
}

func (p *directPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[directOnUpdate](func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var queue kernel.Write[*scene.OpQueue]
		return func(access kernel.ResourceAccess) {
				queue = access.GetWrite[*scene.OpQueue]()
			}, func(_ kernel.Kernel, _ app.UpdateEvent) {
				q := queue.Get()
				if p.instanced {
					q.Model(0, crateModel, scene.ModelDraw{Transforms: p.places})
				}
				for _, place := range p.places[:len(p.places)*boolInt(!p.instanced)] {
					q.Model(0, crateModel, scene.ModelDraw{Transform: place})
				}
				q.Camera(0, scene.CameraDescr{Transform: baselineCamera, FovY: 1.0472, Near: 0.1, Far: 200})
			}
	})
	return nil
}

// BenchmarkDirectModel5000 is BenchmarkDrawnModel5000's frame recorded by a
// plain System from a slice: the same 5 000 Model calls, the same camera, the
// same scene flush, and no ECS plugin composed at all.
func BenchmarkDirectModel5000(b *testing.B) { benchmarkDirect(b, false) }

// BenchmarkDirectModelInstanced5000 is the same 5 000 crates as one Model call
// with 5 000 Transforms, which scene packs as one instanced batch: what a
// renderer that groups Entities by mesh and material before it records gets.
func BenchmarkDirectModelInstanced5000(b *testing.B) { benchmarkDirect(b, true) }

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func benchmarkDirect(b *testing.B, instanced bool) {
	direct := &directPlugin{instanced: instanced}
	for row := range gridRows {
		for col := range gridColumns {
			place := gridPlace(row)
			place.Position.X += float32(col) * gridStep
			direct.places = append(direct.places, scene.Transform(place))
		}
	}
	files := fstest.MapFS{crateModel: &fstest.MapFile{Data: crateGLB(b)}}
	configs := map[kernel.PluginName]any{
		storage.Name: storage.Config{}.WithReadFS("test", 10, fs.FS(files)),
	}
	sink := &errorSink{}
	engine := kernel.New(configs).
		Handler(func(err error) error { sink.add(err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(),
			backendAdapter{&stubBackend{}}, sceneplugin.New(), direct, &inspectOnly{})
	stopped := make(chan struct{})
	b.Cleanup(func() { engine.Quit(); <-stopped })
	go func() { defer close(stopped); engine.Run() }()
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	h := &harness{kernel: k, engine: engine, errs: sink}
	h.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	})
	h.frameUntil(b, "every call to be packed", func() bool {
		passes := h.passes(b)
		return len(passes) >= 1 && passes[0].Instances == len(direct.places)
	})
	for range 100 {
		h.frame(b)
	}
	runFrames(b, h)
}

// inspectOnly gives the direct harness the one command the readers need.
type inspectOnly struct{}

func (*inspectOnly) Name() kernel.PluginName { return "inspect" }

func (*inspectOnly) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{scene.Name}
}

func (*inspectOnly) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	return nil
}

var _ = ecs.Name
var _ = ecsplugin.New
