package internal

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

// Everything here runs against a real kernel.Engine with the real ecs and the
// real app plugin composed beside physics, and reads back what the Components
// hold after a tick. The plugin is judged by what a Body does over the ticks it
// is published, not by what its Systems look like.

// tick is the fixed step every test publishes at, which is the rate every
// number quoted in the spec is quoted at.
const tick = 1.0 / 60

// The four Component sets a Body is spawned from. Which set an Entity carries
// is the whole of what says which kind of Body it is: Dynamic present is
// Dynamic, a Velocity with no Dynamic is Kinematic, and the Static Tag is
// Static and carries no Velocity at all.
type (
	dynamicBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Body     ecsphysics2d.Dynamic
	}
	// forcelessBody is the one trap in the Component set: a Dynamic body with
	// no Force falls out of the velocity integrator's Query and never moves.
	forcelessBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Body     ecsphysics2d.Dynamic
	}
	kinematicBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
	}
	staticBody struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
	}
)

// bodyKind names which of the four sets a spawn carries.
type bodyKind int

const (
	kindDynamic bodyKind = iota
	kindForceless
	kindKinematic
	kindStatic
)

// spawnCmd creates Bodies. It is a System registered as a command, which is how
// a test reaches a structural change from outside a tick.
type spawnCmd kernel.Command[spawnRequest, spawnResponse]

type spawnRequest struct {
	Kind     bodyKind
	Count    int
	Place    ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	Body     ecsphysics2d.Dynamic
}

type spawnResponse struct{ First ecs.Entity }

func spawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[spawnRequest, spawnResponse]) {
	return ecs.ToExecute[spawnRequest, spawnResponse](registrar, func(
		request spawnRequest,
		dynamics *ecs.Spawn[dynamicBody],
		forceless *ecs.Spawn[forcelessBody],
		kinematics *ecs.Spawn[kinematicBody],
		statics *ecs.Spawn[staticBody],
		answer *ecs.Resp[spawnResponse],
	) {
		var first ecs.Entity
		for i := range max(request.Count, 1) {
			var e ecs.Entity
			switch request.Kind {
			case kindDynamic:
				e = dynamics.New(dynamicBody{Place: request.Place, Velocity: request.Velocity, Body: request.Body})
			case kindForceless:
				e = forceless.New(forcelessBody{Place: request.Place, Velocity: request.Velocity, Body: request.Body})
			case kindKinematic:
				e = kinematics.New(kinematicBody{Place: request.Place, Velocity: request.Velocity})
			case kindStatic:
				e = statics.New(staticBody{Place: request.Place})
			}
			if i == 0 {
				first = e
			}
		}
		answer.Set(spawnResponse{First: first})
	})
}

// readCmd reads one Body's three Components back, which is how a test sees what
// a tick did without holding a lock of its own across one.
type readCmd kernel.Command[readRequest, readResponse]

type readRequest struct{ Entity ecs.Entity }

type readResponse struct {
	Place    ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	Force    ecsphysics2d.Force
	HasForce bool
}

func readCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[readRequest, readResponse]) {
	return ecs.ToExecute[readRequest, readResponse](registrar, func(
		request readRequest,
		places *ecs.Get[ecsphysics2d.Position],
		velocities *ecs.Get[ecsphysics2d.Velocity],
		forces *ecs.Get[ecsphysics2d.Force],
		answer *ecs.Resp[readResponse],
	) {
		var reply readResponse
		reply.Place, _ = places.Of(request.Entity)
		reply.Velocity, _ = velocities.Of(request.Entity)
		reply.Force, reply.HasForce = forces.Of(request.Entity)
		answer.Set(reply)
	})
}

// pushQuery is the gameplay write that Force exists for: a System of the app's,
// ordered Before[IntegrateOnUpdate], adding this tick's Force to every Body
// that can take one.
type pushQuery struct {
	Force *ecsphysics2d.Force
}

// pushOnUpdate is the game's own ordering identity for that System.
type pushOnUpdate kernel.Subscription[app.UpdateEvent]

// The probe Systems the chain is read off. Each is sandwiched between two of
// the plugin's four identities, so the order they run in is the order the four
// run in: Integrate < first < Index < second < Detect < third < Solve < fourth.
type (
	afterIntegrateOnUpdate kernel.Subscription[app.UpdateEvent]
	afterIndexOnUpdate     kernel.Subscription[app.UpdateEvent]
	afterDetectOnUpdate    kernel.Subscription[app.UpdateEvent]
	afterSolveOnUpdate     kernel.Subscription[app.UpdateEvent]
)

// game stands in for the app: it spawns Bodies, writes Force, reads Components
// back, and records the order the plugin's four Systems ran in.
//
// push is written only between ticks, and kernel.PublishEvent(…).Wait() is the
// happens-before edge that makes that safe: no test writes it while a tick that
// reads it is in flight.
type game struct {
	push m.Vec2d
	// torque is the angular half of the same write.
	torque float64

	mu    sync.Mutex
	order []string
}

func (g *game) Name() kernel.PluginName { return "physicstestgame" }

func (g *game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (g *game) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(registrar))
	registrar.HandleCommand[readCmd](readCmdImpl(registrar))
	registrar.Subscribe[pushOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[pushQuery]) {
		for _, it := range q.All() {
			it.Force.Force = it.Force.Force.Add(g.push)
			it.Force.Torque += g.torque
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()

	g.probe(registrar)
	return nil
}

// probe registers the four recorders, each ordered into one gap of the chain.
func (g *game) probe(registrar *kernel.Registrar) {
	record := func(name string) func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		return func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
			return nil, func(kernel.Kernel, app.UpdateEvent) error {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.order = append(g.order, name)
				return nil
			}
		}
	}
	registrar.Subscribe[afterIntegrateOnUpdate](record("integrate")).
		After[ecsphysics2d.IntegrateOnUpdate]().Before[ecsphysics2d.IndexOnUpdate]()
	registrar.Subscribe[afterIndexOnUpdate](record("index")).
		After[ecsphysics2d.IndexOnUpdate]().Before[ecsphysics2d.DetectOnUpdate]()
	registrar.Subscribe[afterDetectOnUpdate](record("detect")).
		After[ecsphysics2d.DetectOnUpdate]().Before[ecsphysics2d.SolveOnUpdate]()
	registrar.Subscribe[afterSolveOnUpdate](record("solve")).
		After[ecsphysics2d.SolveOnUpdate]()
}

// ran is the order the recorders saw, as a fresh copy.
func (g *game) ran() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.order...)
}

// harness is physics composed with app, ecs and the game, running in a real
// engine.
type harness struct {
	kernel kernel.Executioner
	game   *game
	plugin *plugin
}

func newHarness(t testing.TB) *harness { return newHarnessWith(t, nil, 1024) }

// newHarnessWith composes the whole engine: app because the step is
// app.UpdateEvent's, ecs because the Components live in its Stores, physics
// because it is what is being tested, and the game standing in for the app.
func newHarnessWith(t testing.TB, config any, ids uint32) *harness {
	t.Helper()
	configs := map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: ids}}
	if config != nil {
		configs[ecsphysics2d.Name] = config
	}
	physics, world := New().(*plugin), &game{}
	var failure error
	engine := kernel.New(configs).
		Handler(func(err error) bool { failure = err; return false }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), physics, world)
	ctx, cancel := context.WithCancel(context.Background())
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
	if failure != nil {
		t.Fatalf("composing the engine: %v", failure)
	}
	k := engine.Executioner()
	if err := k.PublishEvent(app.InitEvent{}).Wait(); err != nil {
		t.Fatalf("publishing the init: %v", err)
	}
	return &harness{kernel: k, game: world, plugin: physics}
}

// frame publishes one real app.UpdateEvent and waits for it: publish, acquire
// every declared lock, run every System, wait.
func (h *harness) frame(t testing.TB) {
	t.Helper()
	if err := h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait(); err != nil {
		t.Fatalf("publishing the update: %v", err)
	}
}

func (h *harness) frames(t testing.TB, n int) {
	t.Helper()
	for range n {
		h.frame(t)
	}
}

func (h *harness) spawn(t testing.TB, request spawnRequest) ecs.Entity {
	t.Helper()
	response, err := h.kernel.ExecuteCommand[spawnCmd](request)
	if err != nil {
		t.Fatalf("spawning: %v", err)
	}
	return response.First
}

func (h *harness) read(t testing.TB, e ecs.Entity) readResponse {
	t.Helper()
	response, err := h.kernel.ExecuteCommand[readCmd](readRequest{Entity: e})
	if err != nil {
		t.Fatalf("reading %v: %v", e, err)
	}
	return response
}

// dynamic is the Dynamic a test spawns with, built through the constructor so
// that every test runs against a Body an app could have built.
func dynamic(t testing.TB, mass, moment, damping, angularDamping float64) ecsphysics2d.Dynamic {
	t.Helper()
	body, err := ecsphysics2d.NewDynamic(mass, moment, damping, angularDamping)
	if err != nil {
		t.Fatalf("NewDynamic(%v, %v, %v, %v): %v", mass, moment, damping, angularDamping, err)
	}
	return body
}

// moverQuery is a game System's write of a Body's Position, which is what an
// app does to place or teleport one. Locking that Store is what makes the
// coupling check bite.
type moverQuery struct {
	Place *ecsphysics2d.Position
}

type moverOnUpdate kernel.Subscription[app.UpdateEvent]

// mover is a game that names the plugin's Components through its root alone,
// and declares whichever dependencies the test hands it.
type mover struct{ deps []kernel.PluginName }

func (*mover) Name() kernel.PluginName             { return "physicstestmover" }
func (p *mover) Dependencies() []kernel.PluginName { return p.deps }

func (*mover) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[moverOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[moverQuery]) {
		for _, it := range q.All() {
			it.Place.Current = it.Place.Current.Add(m.Vec2d{X: 1})
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()
	return nil
}

// composeWithMover builds physics' engine with a mover declaring deps beside
// it, and returns every error composition reported.
func composeWithMover(deps []kernel.PluginName) error {
	var failure error
	kernel.New(map[kernel.PluginName]any{}).
		Handler(func(err error) bool { failure = errors.Join(failure, err); return false }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), New(), &mover{deps: deps})
	return failure
}

func near(got, want float64) bool { return abs(got-want) <= 1e-12 }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
