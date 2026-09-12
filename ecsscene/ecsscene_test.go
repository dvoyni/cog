package ecsscene

import (
	"context"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
	"github.com/dvoyni/cog/storage"
)

// Everything here runs against a real kernel.Engine with the real ecs and the
// real scene plugin composed beside the binding. That is the point of the
// ticket: the binding shape was settled against a prototype standing in for
// scene, and what it has to survive is scene itself.

const (
	crateName  = "crate"
	crateModel = "models/crate.glb"
	walkName   = "walk"
	walkClip   = "Walk"
	idleName   = "idle"
	idleClip   = "Idle"
)

var (
	crate = ecs.HashOf[ModelHash](crateName)
	walk  = ecs.HashOf[ClipHash](walkName)
	idle  = ecs.HashOf[ClipHash](idleName)
)

// testConfig is the manifest every test below draws from.
func testConfig() Config {
	return DefaultConfig().
		WithModel(crateName, crateModel).
		WithClip(walkName, walkClip).
		WithClip(idleName, idleClip)
}

// staticDrawable and animatedDrawable are the two Bundles the game plugin
// spawns. A Bundle is not a Component set: it describes one act of creation,
// and an Entity spawned without an Animation is an ordinary drawable rather
// than an incomplete one.
type staticDrawable struct {
	Place Transform
	Draw  Drawable
}

type animatedDrawable struct {
	Place Transform
	Draw  Drawable
	Anim  Animation
}

// spawnCmd creates drawables. It is a System registered as a command, which is
// how a test reaches a structural change from outside a tick: the barrier a
// spawn takes is the same one either way.
type spawnCmd kernel.Command[spawnRequest, spawnResponse]

type spawnRequest struct {
	Count int
	Model ModelHash
	// Step is how far apart along X the spawned drawables stand, so a test can
	// tell one recorded draw from another.
	Step float32
	// Animation is given to every spawned drawable when Animated is set. A
	// Bundle field is a Component value, so the two Bundles differ by whether
	// this Component exists at all.
	Animated  bool
	Animation Animation
}

type spawnResponse struct {
	First ecs.Entity
}

func spawnCmdImpl(world *ecs.Entities) func() (kernel.Lock, kernel.Execute[spawnRequest, spawnResponse]) {
	return ecs.ToExecute[spawnRequest, spawnResponse](world, func(
		request spawnRequest,
		static *ecs.Spawn[staticDrawable],
		animated *ecs.Spawn[animatedDrawable],
		answer *ecs.Resp[spawnResponse],
	) {
		var first ecs.Entity
		for i := range request.Count {
			place := Transform{}
			place.Position.X = float32(i) * request.Step
			draw := Drawable{Model: request.Model}
			var e ecs.Entity
			if request.Animated {
				e = animated.New(animatedDrawable{Place: place, Draw: draw, Anim: request.Animation})
			} else {
				e = static.New(staticDrawable{Place: place, Draw: draw})
			}
			if i == 0 {
				first = e
			}
		}
		answer.Set(spawnResponse{First: first})
	})
}

// inspectCmd runs a callback under scene's queue lock, which is the only way to
// read what a frame recorded from outside scene.
type inspectCmd kernel.Command[inspectRequest, inspectResponse]

type inspectRequest struct {
	Run func(*scene.OpQueue)
}

type inspectResponse struct{}

func inspectCmdImpl() (kernel.Lock, kernel.Execute[inspectRequest, inspectResponse]) {
	var queue kernel.Read[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetRead[*scene.OpQueue]()
		}, func(_ kernel.Kernel, request inspectRequest) (inspectResponse, error) {
			request.Run(queue.Get())
			return inspectResponse{}, nil
		}
}

// cameraCmd records one camera into the next frame, for the tests that want
// scene to actually decide something. It is a second recorder, which the spec
// says is the shape to avoid in production and is exactly right for a test: it
// shows that the binding's System is an ordinary recorder beside any other.
type cameraCmd kernel.Command[cameraRequest, cameraResponse]

type cameraRequest struct {
	Descr scene.CameraDescr
}

type cameraResponse struct{}

// gamePlugin stands in for the game: it spawns the world's Entities and reads
// back what the frame recorded. It declares the binding because it names the
// binding's Components, and scene because it locks scene's queue.
type gamePlugin struct {
	world  *ecs.Entities
	camera scene.CameraDescr
	mu     sync.Mutex
}

func (p *gamePlugin) Name() kernel.PluginName { return "game" }

func (p *gamePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name, Name}
}

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(p.world))
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	registrar.HandleCommand[cameraCmd](p.cameraCmdImpl)
	registrar.HandleCommand[layerCmd](layerCmdImpl(p.world))
	registrar.HandleCommand[despawnCmd](despawnCmdImpl(p.world))
	registrar.Subscribe[cameraHandler](p.recordCamera)
	return nil
}

type cameraHandler kernel.Subscription[app.UpdateEvent]

func (p *gamePlugin) cameraCmdImpl() (kernel.Lock, kernel.Execute[cameraRequest, cameraResponse]) {
	return func(kernel.ResourceAccess) {}, func(_ kernel.Kernel, request cameraRequest) (cameraResponse, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.camera = request.Descr
		return cameraResponse{}, nil
	}
}

// recordCamera is the game's own recorder, and it declares no ordering either:
// two recorders both hold scene's one queue for write, so the scheduler
// serialises them and both land before the flush.
func (p *gamePlugin) recordCamera() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var queue kernel.Write[*scene.OpQueue]
	return func(access kernel.ResourceAccess) {
			queue = access.GetWrite[*scene.OpQueue]()
		}, func(_ kernel.Kernel, _ app.UpdateEvent) error {
			p.mu.Lock()
			descr := p.camera
			p.mu.Unlock()
			if descr.Near == 0 {
				return nil
			}
			queue.Get().Camera(0, descr)
			return nil
		}
}

type harness struct {
	kernel kernel.Executioner
	engine *kernel.Engine
	errs   *errorSink
}

type errorSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errorSink) add(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *errorSink) snapshot() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.errs...)
}

func newHarness(t testing.TB, config Config) *harness {
	t.Helper()
	return newHarnessOver(t, fstest.MapFS{}, config, 256)
}

// newHarnessOver composes the whole engine: storage and gfx because scene needs
// them, scene because it is what is being bound to, ecs because it is what is
// being bound from, the binding, and a game plugin standing in for the app.
func newHarnessOver(t testing.TB, files fstest.MapFS, config Config, ids uint32) *harness {
	t.Helper()
	world := ecs.NewEntities(ids)
	sink := &errorSink{}
	configs := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("ecsscene-test").WithReadFS("test", 10, fs.FS(files)),
		scene.Name:   scene.DefaultConfig(),
		Name:         config,
	}
	engine := kernel.New(configs).
		Handler(func(err error) bool { sink.add(err); return false }).
		WithPlugins(storage.New(), gfx.New(), scene.New(),
			ecs.Plugin(world), New(world), &gamePlugin{world: world})
	ctx, cancel := context.WithCancel(context.Background())
	// The cleanup waits for Run to return rather than only cancelling it: a
	// dying engine allocates while it winds down, and the allocation claims here
	// count every goroutine's mallocs.
	stopped := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run(ctx)
	}()
	<-engine.Ready()
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	return &harness{kernel: k, engine: engine, errs: sink}
}

// frame publishes one real app.UpdateEvent and waits for it: publish, acquire
// every declared lock, run every System and every flush, wait.
func (h *harness) frame(t testing.TB) {
	t.Helper()
	if err := h.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}
}

func (h *harness) spawn(t testing.TB, request spawnRequest) ecs.Entity {
	t.Helper()
	response, err := h.kernel.ExecuteCommand[spawnCmd](request)
	if err != nil {
		t.Fatalf("spawning %d drawables: %v", request.Count, err)
	}
	return response.First
}

// ops reads back what the last flush published, copied out under the lock: the
// slices scene hands back alias its own storage and are valid only until the
// next flush.
func (h *harness) ops(t testing.TB) []scene.Op {
	t.Helper()
	var out []scene.Op
	_, err := h.kernel.ExecuteCommand[inspectCmd](inspectRequest{Run: func(q *scene.OpQueue) {
		out = q.Ops(nil)
	}})
	if err != nil {
		t.Fatalf("inspecting the queue: %v", err)
	}
	return out
}

// modelOps is ops narrowed to the model draws the binding recorded, with every
// borrowed slice copied so the result survives the next flush.
func (h *harness) modelOps(t testing.TB) []scene.Op {
	t.Helper()
	var models []scene.Op
	for _, op := range h.ops(t) {
		if op.Kind != scene.OpModel {
			continue
		}
		op.Model.Plays = append([]scene.ClipPlay(nil), op.Model.Plays...)
		models = append(models, op)
	}
	return models
}

// TestADrawableEntityRecordsAModelDraw is the tracer bullet: three Components,
// one System nobody wrote a Lock for, and a real scene.OpQueue with the draw in
// it. Nothing in ecs knows about scene, nothing in scene knows about ecs, and
// the binding between them is an ordinary plugin.
func TestADrawableEntityRecordsAModelDraw(t *testing.T) {
	h := newHarness(t, testConfig())
	h.spawn(t, spawnRequest{Count: 1, Model: crate})

	h.frame(t)

	models := h.modelOps(t)
	if len(models) != 1 {
		t.Fatalf("the frame recorded %d model draws, want 1", len(models))
	}
	if models[0].Path != crateModel {
		t.Errorf("the draw named %q, want the manifest's %q", models[0].Path, crateModel)
	}
	if got := models[0].Model.Transform.Position; got != (m.Vec3{}) {
		t.Errorf("the draw stands at %v, want the origin", got)
	}
}
