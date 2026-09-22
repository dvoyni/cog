package internal

import (
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// Everything here runs against a real kernel.Engine with the real ecs, the real
// model plugin and the real gfx composed beside the binding, and reads back what
// gfx handed a recording backend. The binding is judged by what reaches the
// GPU, not by what its Systems look like. scene is not composed: an app runs
// ecsscene or scene, never both.

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
	Place m.Transform
	Step  float32
	// Unplaced spawns the Entities with no Transform at all.
	Unplaced bool

	Model     *ecsscene.Model
	Mesh      *ecsscene.Mesh
	Animation *ecsscene.Animation
	Params    *ecsscene.Params
	Material  *ecsscene.Material
	Light     *ecsscene.Light
	Camera    *ecsscene.Camera
	// ParamsEach, when set, gives the i-th Entity its own Params in place of
	// Params, which is how a test spawns thousands of distinct values in one
	// command.
	ParamsEach func(i int) ecsscene.Params
	// AnimationEach is the same for Animation.
	AnimationEach func(i int) ecsscene.Animation
}

type spawnResponse struct {
	First ecs.Entity
}

// placed and unplaced are the two Component sets the spawn starts from; every
// other Component is added after, through its accessor, so one command covers
// every combination a test names.
type placed struct {
	Place m.Transform
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
		models *ecs.Set[ecsscene.Model],
		meshes *ecs.Set[ecsscene.Mesh],
		animations *ecs.Set[ecsscene.Animation],
		params *ecs.Set[ecsscene.Params],
		materials *ecs.Set[ecsscene.Material],
		lights *ecs.Set[ecsscene.Light],
		cameras *ecs.Set[ecsscene.Camera],
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
			if request.AnimationEach != nil {
				animations.UpdateFor(e, request.AnimationEach(i))
			}
			if request.Params != nil {
				params.UpdateFor(e, *request.Params)
			}
			if request.ParamsEach != nil {
				params.UpdateFor(e, request.ParamsEach(i))
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
// drawing: the frame is rebuilt from the Stores each tick, so nothing else has
// to be told.
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

// bakeCmd bakes a mesh through model's Lookup, which is where a MeshRef a game
// stores in a Mesh Component comes from.
type bakeCmd kernel.Command[bakeRequest, bakeResponse]

type bakeRequest struct{}

type bakeResponse struct {
	Ref model.MeshRef
}

func bakeCmdImpl() (kernel.Lock, kernel.Execute[bakeRequest, bakeResponse]) {
	var lookup kernel.Write[*model.Lookup]
	return func(access kernel.ResourceAccess) {
			lookup = access.GetWrite[*model.Lookup]()
		}, func(k kernel.Kernel, _ bakeRequest) bakeResponse {
			vertices := []model.Vertex{
				{Position: m.Vec3{X: -1, Y: -1}, Normal: m.Vec3{Z: 1}},
				{Position: m.Vec3{X: 1, Y: -1}, Normal: m.Vec3{Z: 1}},
				{Position: m.Vec3{Y: 1}, Normal: m.Vec3{Z: 1}},
			}
			la := model.NewLookupAccess(k, lookup.Get())
			return bakeResponse{Ref: la.BakeMesh(vertices, []uint32{0, 1, 2}, gfx.TopologyTriangleList)}
		}
}

// gamePlugin stands in for the game: it spawns the world's Entities and reads
// back what the frame recorded. It declares the binding because it names the
// binding's Components, and model because it locks model's Lookup.
//
// It subscribes nothing to the tick. A second recorder beside the binding's
// would serialise against it on gfx's queue, and the kernel's bookkeeping for
// a blocked request would show up in the allocation figures as noise that
// scales with frame length.
type gamePlugin struct{}

func (p *gamePlugin) Name() kernel.PluginName { return "game" }

func (p *gamePlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, model.Name, ecsscene.Name}
}

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[unmarked](registrar, 8)
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(registrar))
	registrar.HandleCommand[despawnCmd](despawnCmdImpl(registrar))
	registrar.HandleCommand[bakeCmd](bakeCmdImpl)
	registrar.HandleCommand[keysCmd](keysCmdImpl)
	registrar.HandleCommand[setParamsCmd](setParamsCmdImpl(registrar))
	return nil
}

type harness struct {
	kernel kernel.Executioner
	engine *kernel.Engine
	errs   *errorSink
	// backend is the recording backend a drawing harness renders through, and
	// nil for one whose device never arrives.
	backend *testBackend
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

// newHarnessOver composes the whole engine: storage and gfx because the binding
// loads and draws through them, model because it holds what is drawn, ecs
// because it is what is being bound from, the binding, and a game plugin
// standing in for the app.
func newHarnessOver(t testing.TB, files fstest.MapFS, ids uint32) *harness {
	t.Helper()
	return newHarnessWith(t, files, ids, &detachedBackend{})
}

// newHarnessWith is newHarnessOver rendering through backend.
func newHarnessWith(t testing.TB, files fstest.MapFS, ids uint32, backend gfx.Backend) *harness {
	t.Helper()
	sink := &errorSink{}
	configs := map[kernel.PluginName]any{
		ecs.Name: ecs.Config{PrewarmEntities: ids},
	}
	engine := kernel.New(configs).
		Handler(func(err error) error { sink.add(err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, readMountAdapter{storage.ReadMount{Id: "test", Priority: 10, FS: fs.FS(files)}}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), backendAdapter{backend}, modelplugin.New(),
			ecsplugin.New(), New(), &gamePlugin{})
	// The cleanup waits for Run to return rather than only cancelling it: a
	// dying engine allocates while it winds down, and the allocation claims here
	// count every goroutine's mallocs.
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
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	recording, _ := backend.(*testBackend)
	return &harness{kernel: k, engine: engine, errs: sink, backend: recording}
}

// frame publishes one real app.UpdateEvent and then one app.RenderEvent, and
// waits for each: publish, acquire every declared lock, run every System and
// every flush, wait - and then hand the frame gfx recorded to the backend.
func (h *harness) frame(t testing.TB) {
	t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: 1.0 / 60}).Wait()
	h.kernel.PublishEvent(app.RenderEvent{}).Wait()
}

func (h *harness) spawn(t testing.TB, request spawnRequest) ecs.Entity {
	t.Helper()
	if request.Count == 0 {
		request.Count = 1
	}
	response := h.kernel.ExecuteCommand[spawnCmd](request)
	return response.First
}

func (h *harness) despawn(t testing.TB, e ecs.Entity) {
	t.Helper()
	h.kernel.ExecuteCommand[despawnCmd](despawnRequest{Entity: e})
}

func (h *harness) bake(t testing.TB) model.MeshRef {
	t.Helper()
	response := h.kernel.ExecuteCommand[bakeCmd](bakeRequest{})
	if response.Ref.ID() == 0 {
		t.Fatalf("baking a mesh: ref %v", response.Ref)
	}
	return response.Ref
}
