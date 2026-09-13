package ecsscene

import (
	"context"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// Everything here runs against a real kernel.Engine with the real ecs and the
// real scene plugin composed beside the binding, and reads back what scene's
// own flush published. The binding is judged by what reaches scene, not by
// what its System looks like.

const crateModel = "models/crate.glb"

// spawnCmd creates Entities. It is a System registered as a command, which is
// how a test reaches a structural change from outside a tick.
type spawnCmd kernel.Command[spawnRequest, spawnResponse]

// spawnRequest describes Count Entities, each carrying the Components whose
// fields are set. A nil field is a Component the Entity does not have, which is
// the difference the binding has to see: absent is not zero.
type spawnRequest struct {
	Count int
	// Place is every Entity's Transform, and Step how far apart along X they
	// stand, so a test can tell one recorded draw from another.
	Place Transform
	Step  float32
	// Unplaced spawns the Entities with no Transform at all.
	Unplaced bool

	Model     *Model
	Mesh      *Mesh
	Animation *Animation
	Params    *Params
	Material  *Material
	Light     *Light
	Camera    *Camera
}

type spawnResponse struct {
	First ecs.Entity
}

// placed and unplaced are the two Bundles the spawn starts from; every other
// Component is added after, through its accessor, so one command covers every
// combination a test names.
type placed struct {
	Place Transform
}

// unmarked is a Tag the game owns, so an Entity can be spawned with nothing of
// the binding's on it.
type unmarked struct{}

type unplaced struct {
	Marker unmarked
}

func spawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[spawnRequest, spawnResponse]) {
	return ecs.ToExecute[spawnRequest, spawnResponse](registrar, func(
		request spawnRequest,
		withPlace *ecs.Spawn[placed],
		withoutPlace *ecs.Spawn[unplaced],
		models *ecs.Set[Model],
		meshes *ecs.Set[Mesh],
		animations *ecs.Set[Animation],
		params *ecs.Set[Params],
		materials *ecs.Set[Material],
		lights *ecs.Set[Light],
		cameras *ecs.Set[Camera],
		answer *ecs.Resp[spawnResponse],
	) {
		var first ecs.Entity
		for i := range request.Count {
			var e ecs.Entity
			if request.Unplaced {
				e = withoutPlace.New(unplaced{})
			} else {
				place := request.Place
				place.Position.X += float32(i) * request.Step
				e = withPlace.New(placed{Place: place})
			}
			if request.Model != nil {
				models.UpdateFor(e, *request.Model)
			}
			if request.Mesh != nil {
				meshes.UpdateFor(e, *request.Mesh)
			}
			if request.Animation != nil {
				animations.UpdateFor(e, *request.Animation)
			}
			if request.Params != nil {
				params.UpdateFor(e, *request.Params)
			}
			if request.Material != nil {
				materials.UpdateFor(e, *request.Material)
			}
			if request.Light != nil {
				lights.UpdateFor(e, *request.Light)
			}
			if request.Camera != nil {
				cameras.UpdateFor(e, *request.Camera)
			}
			if i == 0 {
				first = e
			}
		}
		answer.Set(spawnResponse{First: first})
	})
}

// despawnCmd retires one Entity, which is the only way a drawable stops
// drawing: scene keeps no per-entity state, so nothing has to be told.
type despawnCmd kernel.Command[despawnRequest, despawnResponse]

type despawnRequest struct {
	Entity ecs.Entity
}

type despawnResponse struct{}

func despawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[despawnRequest, despawnResponse]) {
	return ecs.ToExecute[despawnRequest, despawnResponse](registrar, func(
		request despawnRequest, world *ecs.WriteableEntities,
	) {
		world.Despawn(request.Entity)
	})
}

// inspectCmd runs a callback under scene's queue lock, which is the only way to
// read what a frame published from outside scene.
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

// bakeCmd bakes a mesh through scene's own lookup, which is where a MeshRef a
// game stores in a Mesh Component comes from.
type bakeCmd kernel.Command[bakeRequest, bakeResponse]

type bakeRequest struct{}

type bakeResponse struct {
	Ref scene.MeshRef
}

func bakeCmdImpl() (kernel.Lock, kernel.Execute[bakeRequest, bakeResponse]) {
	var lookup kernel.Write[*scene.Lookup]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*scene.Lookup]()
		}, func(k kernel.Kernel, _ bakeRequest) (bakeResponse, error) {
			vertices := []scene.Vertex{
				{Position: m.Vec3{X: -1, Y: -1}, Normal: m.Vec3{Z: 1}},
				{Position: m.Vec3{X: 1, Y: -1}, Normal: m.Vec3{Z: 1}},
				{Position: m.Vec3{Y: 1}, Normal: m.Vec3{Z: 1}},
			}
			la := scene.NewLookupAccess(k, lookup.Get())
			return bakeResponse{Ref: la.BakeMesh(vertices, []uint32{0, 1, 2}, gfx.TopologyTriangleList)}, nil
		}
}

// gamePlugin stands in for the game: it spawns the world's Entities and reads
// back what the frame recorded. It declares the binding because it names the
// binding's Components, and scene because it locks scene's resources.
//
// It subscribes nothing to the tick. A second recorder beside the binding's
// would serialise against it on scene's queue, and the kernel's bookkeeping for
// a blocked request would show up in the allocation figures as noise that
// scales with frame length.
type gamePlugin struct{}

func (p *gamePlugin) Name() kernel.PluginName { return "game" }

func (p *gamePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, scene.Name, Name}
}

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[unmarked](registrar, 8)
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(registrar))
	registrar.HandleCommand[despawnCmd](despawnCmdImpl(registrar))
	registrar.HandleCommand[inspectCmd](inspectCmdImpl)
	registrar.HandleCommand[bakeCmd](bakeCmdImpl)
	return nil
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

func newHarness(t testing.TB) *harness {
	t.Helper()
	return newHarnessOver(t, fstest.MapFS{}, 256)
}

// newHarnessOver composes the whole engine: storage and gfx because scene needs
// them, scene because it is what is being bound to, ecs because it is what is
// being bound from, the binding, and a game plugin standing in for the app.
func newHarnessOver(t testing.TB, files fstest.MapFS, ids uint32) *harness {
	t.Helper()
	sink := &errorSink{}
	configs := map[kernel.PluginName]any{
		storage.Name: storage.DefaultConfig("ecsscene-test").WithReadFS("test", 10, fs.FS(files)),
		scene.Name:   scene.DefaultConfig(),
		ecs.Name:     ecs.DefaultConfig().WithPrewarmEntities(ids),
	}
	engine := kernel.New(configs).
		Handler(func(err error) bool { sink.add(err); return false }).
		WithPlugins(storage.New(), gfx.New(), scene.New(),
			ecs.Plugin(), New(), &gamePlugin{})
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
	if request.Count == 0 {
		request.Count = 1
	}
	response, err := h.kernel.ExecuteCommand[spawnCmd](request)
	if err != nil {
		t.Fatalf("spawning %d Entities: %v", request.Count, err)
	}
	return response.First
}

func (h *harness) despawn(t testing.TB, e ecs.Entity) {
	t.Helper()
	if _, err := h.kernel.ExecuteCommand[despawnCmd](despawnRequest{Entity: e}); err != nil {
		t.Fatalf("despawning: %v", err)
	}
}

func (h *harness) bake(t testing.TB) scene.MeshRef {
	t.Helper()
	response, err := h.kernel.ExecuteCommand[bakeCmd](bakeRequest{})
	if err != nil || response.Ref.ID() == 0 {
		t.Fatalf("baking a mesh: ref %v, %v", response.Ref, err)
	}
	return response.Ref
}

// ops reads back what the last flush published, narrowed to one kind. The
// slices inside alias scene's frame arenas, which stay put until the next
// flush; every test reads them before publishing another frame.
func (h *harness) ops(t testing.TB, kinds ...scene.OpKind) []scene.Op {
	t.Helper()
	var all []scene.Op
	if _, err := h.kernel.ExecuteCommand[inspectCmd](inspectRequest{Run: func(q *scene.OpQueue) {
		all = q.Ops(nil)
	}}); err != nil {
		t.Fatalf("inspecting the queue: %v", err)
	}
	var out []scene.Op
	for _, op := range all {
		for _, kind := range kinds {
			if op.Kind == kind {
				out = append(out, op)
			}
		}
	}
	return out
}

// passes reads back what the last flush decided.
func (h *harness) passes(t testing.TB) []scene.PassView {
	t.Helper()
	var out []scene.PassView
	if _, err := h.kernel.ExecuteCommand[inspectCmd](inspectRequest{Run: func(q *scene.OpQueue) {
		out = q.Passes(nil)
	}}); err != nil {
		t.Fatalf("inspecting the queue: %v", err)
	}
	return out
}
