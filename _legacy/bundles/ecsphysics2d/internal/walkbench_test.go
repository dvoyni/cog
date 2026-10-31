package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

// The ECS Query walk over Body Components, re-measured in float64.
//
// The spec quotes ≈ 3.4 ns an Entity for this walk and that number came out of
// ecs.md, where it was taken over float32-sized Components. Physics computes in
// float64: Position is four float64 and a float64 angle where the prototype's
// was four float32 and a float32, so the row the walk streams roughly doubles
// and the cost the design's arithmetic is built on has to be taken again over
// the Components physics actually has.
//
// What is measured is the *slope*, at two populations an order of magnitude
// apart, because a frame's fixed cost — the kernel's dispatch over the
// subscriptions the engine composes — is the engine's and not the walk's. Two
// sizes and a subtraction remove it, which is the same method the ECS's own
// frame measurement uses. The empty engine is measured beside them so the
// subtraction can be checked rather than trusted.
//
// The engine here composes ecs and app and no physics, and registers the Body
// Components itself. That is deliberate: the four physics Systems in a frame
// would put Index, Detect and Solve inside a number that is about one Query
// walk.

// walkPopulations are the two sizes the slope is taken between, and the empty
// engine that shows what the frame costs with no Body in it at all.
var walkPopulations = []int{0, 1_000, 10_000}

// BenchmarkTheQueryWalkOverBodyComponents is the bare walk: one Query over
// Position and Velocity, one read and one write a row, which is the shape
// ecs.md's own walk measured and the shape the gather-through-a-slot-table
// arithmetic quotes.
func BenchmarkTheQueryWalkOverBodyComponents(b *testing.B) {
	benchmarkWalk(b, func(registrar *kernel.Registrar) {
		registrar.Subscribe[walkOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar,
			func(q *ecs.Query[positionQuery]) {
				for _, it := range q.All() {
					it.Place.Current.X += it.Velocity.Linear.X
				}
			}))
	})
}

// BenchmarkIntegrateOverBodyComponents is the same walk doing Integrate's own
// work, which is what the System costs rather than what the walk costs. The
// difference between the two is the arithmetic the specification lays out for
// the position update.
func BenchmarkIntegrateOverBodyComponents(b *testing.B) {
	benchmarkWalk(b, func(registrar *kernel.Registrar) {
		registrar.Subscribe[walkOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar,
			func(q *ecs.Query[positionQuery]) {
				for _, it := range q.All() {
					IntegratePosition(it.Place, &it.Velocity, tick)
				}
			}))
	})
}

// BenchmarkAFrameWithNoWalkAtAll is the floor both are read against: the same
// engine, the same population, and nothing subscribed to walk it.
func BenchmarkAFrameWithNoWalkAtAll(b *testing.B) {
	benchmarkWalk(b, func(*kernel.Registrar) {})
}

// walkOnUpdate is the one System the walk benchmarks subscribe.
type walkOnUpdate kernel.Subscription[app.UpdateEvent]

func benchmarkWalk(b *testing.B, subscribe func(*kernel.Registrar)) {
	for _, n := range walkPopulations {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			executioner := newWalkWorld(b, n, subscribe)
			b.ReportAllocs()
			b.ResetTimer()
			// The classic b.N form, deliberately: testing.B.Loop is rejected in
			// this repo for pinning loop variables through runtime.KeepAlive.
			for range b.N {
				executioner.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}

// walkSpawnCmd fills the world with n Bodies carrying a Position and a
// Velocity, which is the Component pair the walk streams.
type walkSpawnCmd kernel.Command[int, struct{}]

// walkBody is the Component set the walk is measured over: the two Body
// Components of the Query, in float64, as physics declares them.
type walkBody struct {
	Place    Position
	Velocity Velocity
}

// walkGame registers the two Body Components and whatever System the benchmark
// under it subscribes. It registers the Components itself because physics is
// not composed here.
type walkGame struct{ subscribe func(*kernel.Registrar) }

func (*walkGame) Name() kernel.PluginName { return "physicswalkgame" }

func (*walkGame) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (g *walkGame) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[Position](registrar, 1024)
	ecs.RegisterComponent[Velocity](registrar, 1024)
	registrar.HandleCommand[walkSpawnCmd](ecs.ToExecute[int, struct{}](registrar, func(
		n int, bodies *ecs.Spawn[walkBody],
	) {
		for i := range n {
			bodies.New(walkBody{
				Place:    Position{Current: m.Vec2d{X: float64(i)}},
				Velocity: Velocity{Linear: m.Vec2d{X: 1, Y: 2}},
			})
		}
	}))
	g.subscribe(registrar)
	return nil
}

func newWalkWorld(b *testing.B, n int, subscribe func(*kernel.Registrar)) kernel.Executioner {
	b.Helper()
	configs := map[kernel.PluginName]any{
		ecs.Name: ecs.Config{PrewarmEntities: uint32(max(n, 1))},
	}
	var failure error
	engine := kernel.New(configs).
		Handler(func(err error) error { failure = err; return nil }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), &walkGame{subscribe: subscribe})
	stopped := make(chan struct{})
	b.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	if failure != nil {
		b.Fatalf("composing the engine: %v", failure)
	}
	executioner := engine.Executioner()
	executioner.PublishEvent(app.InitEvent{}).Wait()
	if n > 0 {
		executioner.ExecuteCommand[walkSpawnCmd](n)
	}
	// A hundred warm frames, so what is measured is a walk over Stores that
	// have stopped growing.
	for range 100 {
		executioner.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
	}
	return executioner
}
