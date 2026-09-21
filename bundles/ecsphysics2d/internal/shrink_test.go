package internal

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

// TestThePluginRegistersShrinkOverItsOwnResourcesAlone reads the declaration off
// the engine's description: the Command is physics' own, and its lock is write
// on the three Resources whose buffers it cuts and nothing besides — no
// Component Store and not the id authority.
func TestThePluginRegistersShrinkOverItsOwnResourcesAlone(t *testing.T) {
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), New())

	want := map[reflect.Type]bool{
		reflect.TypeFor[*ecsphysics2d.Contacts]():    true,
		reflect.TypeFor[*ecsphysics2d.StaticIndex](): true,
		reflect.TypeFor[*ecsphysics2d.BodyIndex]():   true,
	}
	for _, command := range engine.Describe().Commands {
		if command.Type != reflect.TypeFor[ecsphysics2d.ShrinkCmd]() {
			continue
		}
		if command.Owner != ecsphysics2d.Name {
			t.Fatalf("ShrinkCmd is owned by %q, want %q", command.Owner, ecsphysics2d.Name)
		}
		if len(command.Writes) != len(want) {
			t.Fatalf("ShrinkCmd writes %v, want the three physics Resources", command.Writes)
		}
		for _, written := range command.Writes {
			if !want[written] {
				t.Fatalf("ShrinkCmd writes %v, which is not one of the three physics Resources", written)
			}
		}
		if len(command.Reads) != 0 || len(command.Uses) != 0 {
			t.Fatalf("ShrinkCmd reads %v and uses %v, want nothing besides its three writes",
				command.Reads, command.Uses)
		}
		if !command.SelfExclusive {
			t.Fatal("ShrinkCmd is not self-exclusive, so the closure its handles live in is shared by overlapping invocations as soon as a lock narrows")
		}
		return
	}
	t.Fatal("the architecture has no ecsphysics2d.ShrinkCmd")
}

// TestAShrunkPhysicsWorldReturnsToItsSteadyState executes ShrinkCmd as an app
// would, after a spike, and measures the ticks that follow against a control
// that had the same spike and no shrink. The ticks that regrow what the shrink
// cut are excluded, as the command's documentation says they allocate; after
// them the step must be back on the line its control is on.
//
// It refuses a scene with no Contacts in it, for the same reason the three
// allocation-line measurements do: a step over an empty Contact list walks the
// buffers this command is about without filling any of them.
func TestAShrunkPhysicsWorldReturnsToItsSteadyState(t *testing.T) {
	const ticks = 10_000

	measure := func(shrink bool) (float64, int) {
		world := newShrinkWorld(t)
		world.grow(t, steadyBodies)
		world.run(t, 100)
		world.grow(t, spikeBodies)
		world.run(t, 2)
		world.cut(t, steadyBodies)
		world.run(t, 2)

		if shrink {
			released := world.kernel.ExecuteCommand[ecsphysics2d.ShrinkCmd](ecsphysics2d.ShrinkRequest{})
			t.Logf("the zero request after a %d Body spike released %+v", spikeBodies, released)
			if released.Contacts == 0 || released.Indices == 0 || released.WorldCache == 0 || released.Scratch == 0 {
				t.Fatalf("the zero request after a spike released %+v, want Contacts, Indices, WorldCache and Scratch above 0",
					released)
			}
		}
		// The regrowth ticks, excluded: the Contact buffers, the pair tables,
		// the cell lists, the slab and the solver scratch all grow back to what the steady scene
		// needs.
		world.run(t, 100)

		touching := world.touching(t)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		world.run(t, ticks)
		runtime.ReadMemStats(&after)
		return float64(after.Mallocs-before.Mallocs) / ticks, touching
	}

	control, controlTouching := measure(false)
	shrunk, shrunkTouching := measure(true)
	t.Logf("objects a tick after a spike: control %.3f over %d Contacts, after the zero request %.3f over %d",
		control, controlTouching, shrunk, shrunkTouching)
	if controlTouching == 0 || shrunkTouching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it measures neither Detect nor Solve")
	}
	if shrunk > control+0.05 {
		t.Errorf("after the zero request the step costs %.3f objects a tick against its control's %.3f",
			shrunk, control)
	}
}

// TestAShrunkPhysicsWorldGivesTheSolverScratchBack is the fifth area in the
// same shape: spike, cut, shrink the scratch alone, exclude the regrowth ticks,
// and measure against a control that had the same spike and no shrink.
//
// What it measures is what each world still holds. The scratch is released
// whole rather than clipped, because nothing in it is read across a tick, so a
// scratch-only shrink run as a probe at the end reports exactly what the world
// was holding: the control still holds its spike's slot table and gather, and
// the shrunk world holds what the steady scene regrew. The step after it must
// be back on its control's allocation line, and — because the scratch carries
// nothing a later tick reads — it must move every Body exactly where the
// control moved it.
func TestAShrunkPhysicsWorldGivesTheSolverScratchBack(t *testing.T) {
	const ticks = 2_000
	scratchOnly := ecsphysics2d.ShrinkRequest{
		KeepContacts: true, KeepCached: true, KeepIndices: true, KeepWorldCache: true,
	}

	type outcome struct {
		held     uintptr
		objects  float64
		touching int
		regrown  []m.Vec2d
		places   []m.Vec2d
	}
	measure := func(shrink bool) outcome {
		world := newShrinkWorld(t)
		world.grow(t, steadyBodies)
		world.run(t, 100)
		world.grow(t, spikeBodies)
		world.run(t, 2)
		world.cut(t, steadyBodies)
		world.run(t, 2)

		if shrink {
			released := world.kernel.ExecuteCommand[ecsphysics2d.ShrinkCmd](scratchOnly)
			t.Logf("the scratch alone after a %d Body spike released %+v", spikeBodies, released)
			if released.Scratch == 0 {
				t.Fatalf("the scratch alone after a spike released %+v, want Scratch above 0", released)
			}
			if released.Contacts != 0 || released.Cached != 0 || released.Indices != 0 || released.WorldCache != 0 {
				t.Fatalf("the scratch alone released %+v, want every kept area at 0", released)
			}
		}
		// The regrowth ticks, excluded: the slot table, the gather and the
		// Probe buffer grow back to what the steady scene needs. The first of
		// them is the one that regrows it all at once, so where it leaves the
		// Bodies is kept.
		var result outcome
		world.run(t, 1)
		result.regrown = world.places(t)
		world.run(t, 99)

		result.touching = world.touching(t)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		world.run(t, ticks)
		runtime.ReadMemStats(&after)
		result.objects = float64(after.Mallocs-before.Mallocs) / ticks
		result.places = world.places(t)
		result.held = world.kernel.ExecuteCommand[ecsphysics2d.ShrinkCmd](scratchOnly).Scratch
		return result
	}

	control := measure(false)
	shrunk := measure(true)
	t.Logf("scratch held after a spike: control %d bytes, after the shrink %d; objects a tick %.3f against %.3f over %d and %d Contacts",
		control.held, shrunk.held, control.objects, shrunk.objects, control.touching, shrunk.touching)
	if control.touching == 0 || shrunk.touching == 0 {
		t.Fatal("the measured scene has no Contacts at all, so it gathers nothing for the solver")
	}
	if shrunk.held >= control.held {
		t.Errorf("after the shrink the world holds %d scratch bytes against its control's %d, want fewer",
			shrunk.held, control.held)
	}
	// The spike's slot table alone is four bytes a Body slot, and the control
	// still holds it.
	if gave := control.held - shrunk.held; gave < 4*spikeBodies {
		t.Errorf("the shrink gave back %d scratch bytes, want at least the %d a %d Body slot table holds",
			gave, 4*spikeBodies, spikeBodies)
	}
	if shrunk.objects > control.objects+0.05 {
		t.Errorf("after the shrink the step costs %.3f objects a tick against its control's %.3f",
			shrunk.objects, control.objects)
	}
	samePlaces(t, "the tick after the shrink", control.regrown, shrunk.regrown)
	samePlaces(t, "the end of the measurement", control.places, shrunk.places)
}

// samePlaces fails unless two worlds left every Body at exactly the same place.
func samePlaces(t *testing.T, when string, control, shrunk []m.Vec2d) {
	t.Helper()
	if len(shrunk) != len(control) || len(control) == 0 {
		t.Fatalf("at %s the two worlds hold %d and %d Bodies, want the same steady scene",
			when, len(control), len(shrunk))
	}
	for i := range control {
		if shrunk[i] != control[i] {
			t.Fatalf("at %s Body %d is at %v after the shrink and at %v in its control: shrinking the scratch changed the simulation",
				when, i, shrunk[i], control[i])
		}
	}
}

// The two populations the spike runs between: the scene the steady state is
// measured over, and the level load that grows every buffer past it.
const (
	steadyBodies = 256
	spikeBodies  = 4096
)

// shrinkWorld is physics in a real engine with a game that can make a spike:
// grow spawns Bodies and the Statics they press into, cut despawns all but as
// many as it is told to keep, and count reports what Detect found.
//
// It composes an engine of its own rather than the shared harness because what
// it needs from a game is exactly what no other test here needs: a population
// it can raise and lower from outside a tick.
type shrinkWorld struct {
	kernel kernel.Executioner
	game   *shrinkGame
}

func newShrinkWorld(t testing.TB) *shrinkWorld {
	t.Helper()
	game := &shrinkGame{body: dynamic(t, 2, 8, 15, 0.3)}
	configs := map[kernel.PluginName]any{
		ecs.Name: ecs.Config{PrewarmEntities: 4 * spikeBodies},
	}
	var failure error
	engine := kernel.New(configs).
		Handler(func(err error) error { failure = err; return nil }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), New(), game)
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
	if failure != nil {
		t.Fatalf("composing the engine: %v", failure)
	}
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	return &shrinkWorld{kernel: k, game: game}
}

func (w *shrinkWorld) run(t testing.TB, ticks int) {
	t.Helper()
	for range ticks {
		w.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
	}
}

func (w *shrinkWorld) grow(t testing.TB, to int) {
	t.Helper()
	w.kernel.ExecuteCommand[shrinkGrowCmd](to)
}

func (w *shrinkWorld) cut(t testing.TB, keep int) {
	t.Helper()
	w.kernel.ExecuteCommand[shrinkCutCmd](keep)
}

func (w *shrinkWorld) touching(t testing.TB) int {
	t.Helper()
	return w.kernel.ExecuteCommand[shrinkCountCmd](struct{}{})
}

func (w *shrinkWorld) places(t testing.TB) []m.Vec2d {
	t.Helper()
	return w.kernel.ExecuteCommand[shrinkPlacesCmd](struct{}{})
}

type (
	shrinkGrowCmd   kernel.Command[int, struct{}]
	shrinkCutCmd    kernel.Command[int, struct{}]
	shrinkCountCmd  kernel.Command[struct{}, int]
	shrinkPlacesCmd kernel.Command[struct{}, []m.Vec2d]
)

// shrinkPushOnUpdate is the game's own Force write, ordered ahead of the step
// so the Bodies are pressed into their Statics every tick and the pairs the
// measurement needs keep touching.
type shrinkPushOnUpdate kernel.Subscription[app.UpdateEvent]

// shrinkGame spawns pairs of a Dynamic Body and the Static it is pushed into,
// a pair at a time, so that growing and cutting the population is a matter of
// how many pairs are held.
type shrinkGame struct {
	body ecsphysics2d.Dynamic
	held []ecs.Entity
}

func (*shrinkGame) Name() kernel.PluginName { return "physicsshrinkgame" }

func (*shrinkGame) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (g *shrinkGame) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[shrinkGrowCmd](ecs.ToExecute[int, struct{}](registrar, func(
		to int,
		bodies *ecs.Spawn[shapedBody],
		statics *ecs.Spawn[shapedStatic],
	) {
		for len(g.held) < 2*to {
			at := gridAt(len(g.held) / 2)
			g.held = append(g.held,
				bodies.New(shapedBody{
					Place: ecsphysics2d.Position{Current: at},
					Body:  g.body,
					Shape: measured(0.4),
				}),
				statics.New(shapedStatic{
					Place: ecsphysics2d.Position{Current: at.Add(m.Vec2d{X: 0.75})},
					Shape: measured(0.4),
				}))
		}
	}))
	registrar.HandleCommand[shrinkCutCmd](ecs.ToExecute[int, struct{}](registrar, func(
		keep int, entities *ecs.WriteableEntities,
	) {
		for _, e := range g.held[2*keep:] {
			entities.Despawn(e)
		}
		g.held = g.held[:2*keep]
	}))
	registrar.HandleCommand[shrinkCountCmd](ecs.ToExecute[struct{}, int](registrar, func(
		_ struct{},
		contacts *ecs.Read[*ecsphysics2d.Contacts],
		answer *ecs.Resp[int],
	) {
		answer.Set(contacts.Get().Len())
	}))
	registrar.HandleCommand[shrinkPlacesCmd](ecs.ToExecute[struct{}, []m.Vec2d](registrar, func(
		_ struct{},
		places *ecs.Get[ecsphysics2d.Position],
		answer *ecs.Resp[[]m.Vec2d],
	) {
		out := make([]m.Vec2d, 0, len(g.held))
		for _, e := range g.held {
			place, _ := places.Of(e)
			out = append(out, place.Current)
		}
		answer.Set(out)
	}))
	registrar.Subscribe[shrinkPushOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		q *ecs.Query[pushQuery],
	) {
		for _, it := range q.All() {
			it.Force.Force = it.Force.Force.Add(m.Vec2d{X: 10})
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()
	return nil
}
