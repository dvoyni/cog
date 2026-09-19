package internal

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// TestThePluginRegistersShrinkUnderTheAuthorityAlone reads the declaration off
// the engine's description: the Command is the ecs plugin's own, and its lock is
// write{*Entities} and nothing besides, which excludes every ECS System without
// naming a Store.
func TestThePluginRegistersShrinkUnderTheAuthorityAlone(t *testing.T) {
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(New(), &gamePlugin{})

	for _, command := range engine.Describe().Commands {
		if command.Type != reflect.TypeFor[ecs.ShrinkCmd]() {
			continue
		}
		if command.Owner != ecs.Name {
			t.Fatalf("ShrinkCmd is owned by %q, want %q", command.Owner, ecs.Name)
		}
		if len(command.Writes) != 1 || command.Writes[0] != reflect.TypeFor[*ecs.Entities]() {
			t.Fatalf("ShrinkCmd writes %v, want write{*ecs.Entities} alone", command.Writes)
		}
		if len(command.Reads) != 0 || len(command.Uses) != 0 {
			t.Fatalf("ShrinkCmd reads %v and uses %v, want nothing besides write{*ecs.Entities}", command.Reads, command.Uses)
		}
		return
	}
	t.Fatalf("the architecture has no ecs.ShrinkCmd")
}

type churnSystem kernel.Subscription[app.UpdateEvent]

type steadyMoveSystem kernel.Subscription[app.UpdateEvent]

type (
	steadyLifetimeSystem kernel.Subscription[app.UpdateEvent]
	steadyMirrorSystem   kernel.Subscription[app.UpdateEvent]
)

// growCmd spawns as many projectiles as it is asked for, and cutCmd despawns all
// but as many as it is asked to keep. They are how the test makes a spike from
// outside the frame, the way a level load would.
type growCmd kernel.Command[int, struct{}]

type cutCmd kernel.Command[int, struct{}]

// churnPerTick is how many projectiles the churn System spawns and despawns
// every frame, so the steady state carries a structural change and a shrunk
// free list, Store and walk all have something to regrow.
const churnPerTick = 16

// steadyGame is an app's game: a System that churns Entities every frame, a
// System that moves everything, and the two commands that make a spike. With
// hooks it also has two Hook readers: one of every Spawn and Despawn carrying
// velocity, and one of everything that happens to position, which move changes
// on every Entity every frame. So the Hook logs, both readers' copies and
// move's row copy for Changed all hold the spike's capacity, and all regrow
// after a shrink.
type steadyGame struct {
	hooks     bool
	churned   []ecs.Entity
	held      []ecs.Entity
	delivered int
}

func (*steadyGame) Name() kernel.PluginName { return "steady" }

func (*steadyGame) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (g *steadyGame) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[position](registrar, 64)
	ecs.RegisterComponent[velocity](registrar, 64)
	g.churned = make([]ecs.Entity, 0, churnPerTick)
	registrar.Subscribe[churnSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(sp *ecs.Spawn[projectile], we *ecs.WriteableEntities) {
		for _, e := range g.churned {
			we.Despawn(e)
		}
		g.churned = g.churned[:0]
		for range churnPerTick {
			g.churned = append(g.churned, sp.New(projectile{Velocity: velocity{X: 1}}))
		}
	})).First()
	registrar.Subscribe[steadyMoveSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[moveQuery]) {
		for _, it := range q.All() {
			it.Position.X += it.Velocity.X
		}
	})).Last()
	if g.hooks {
		registrar.Subscribe[steadyLifetimeSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(h *ecs.Hooks[velocity, ecs.HookSpawnedDespawned]) {
			for range h.All() {
				g.delivered++
			}
		}))
		registrar.Subscribe[steadyMirrorSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(h *ecs.Hooks[position, ecs.HookAll]) {
			for range h.All() {
				g.delivered++
			}
		}))
	}
	registrar.HandleCommand[growCmd](ecs.ToExecute[int, struct{}](registrar, func(n int, sp *ecs.Spawn[projectile]) {
		for range n {
			g.held = append(g.held, sp.New(projectile{Velocity: velocity{X: 1}}))
		}
	}))
	registrar.HandleCommand[cutCmd](ecs.ToExecute[int, struct{}](registrar, func(keep int, we *ecs.WriteableEntities) {
		for _, e := range g.held[keep:] {
			we.Despawn(e)
		}
		g.held = g.held[:keep]
	}))
	return nil
}

// TestAShrunkWorldReturnsToItsSteadyState executes ShrinkCmd as an app would,
// after a spike, and measures the frames that follow against a control that
// had the same spike and no shrink. The frames that regrow what the shrink cut
// are excluded, as the spec says they allocate; after them the frame must be
// back on the line its control is on. It does so without Hooks, and with Hook
// readers watching the spike, where the zero request also gives back the Hook
// logs, the readers' copies and the row copy for Changed.
func TestAShrunkWorldReturnsToItsSteadyState(t *testing.T) {
	const frames = 10_000
	measure := func(hooks, shrink bool) float64 {
		game := &steadyGame{hooks: hooks}
		engine := kernel.New(map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: 64}}).
			Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
			WithPlugins(New(), game)
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
		executioner := engine.Executioner()
		run := func(n int) {
			for range n {
				executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
			}
		}

		executioner.ExecuteCommand[growCmd](1_000)
		run(100)
		executioner.ExecuteCommand[growCmd](20_000)
		run(1)
		executioner.ExecuteCommand[cutCmd](1_000)
		run(1)
		if shrink {
			released := executioner.ExecuteCommand[ecs.ShrinkCmd](ecs.ShrinkRequest{})
			if released.Stores == 0 || released.Entities == 0 || released.Scratch == 0 {
				t.Fatalf("the zero request after a spike released %+v, want Stores, Entities and Scratch above 0", released)
			}
			if (released.Hooks > 0) != hooks {
				t.Fatalf("the zero request after a spike released %d Hooks bytes, with Hook readers %v", released.Hooks, hooks)
			}
		}
		// The regrowth frames, excluded: the free list, the Stores, the walks, the
		// Hook logs, the readers' copies and the row copy grow back to what the
		// steady state needs.
		run(100)

		delivered := game.delivered
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		run(frames)
		runtime.ReadMemStats(&after)
		if hooks && game.delivered-delivered < frames*churnPerTick {
			t.Fatalf("the Hook readers were given %d records over %d frames, want at least %d: the arm is not reading",
				game.delivered-delivered, frames, frames*churnPerTick)
		}
		return float64(after.Mallocs-before.Mallocs) / frames
	}

	for _, hooks := range []bool{false, true} {
		control := measure(hooks, false)
		shrunk := measure(hooks, true)
		t.Logf("objects a frame after a spike, Hook readers %v: control %.3f, after the zero request %.3f", hooks, control, shrunk)
		if shrunk > control+0.05 {
			t.Errorf("after the zero request, Hook readers %v, the frame costs %.3f objects against its control's %.3f", hooks, shrunk, control)
		}
	}
}
