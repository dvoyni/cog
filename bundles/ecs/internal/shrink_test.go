package internal

import (
	"context"
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
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
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
// System that moves everything, and the two commands that make a spike.
type steadyGame struct {
	churned []ecs.Entity
	held    []ecs.Entity
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
// back on the line its control is on.
func TestAShrunkWorldReturnsToItsSteadyState(t *testing.T) {
	const frames = 10_000
	measure := func(shrink bool) float64 {
		engine := kernel.New(map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: 64}}).
			Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
			WithPlugins(New(), &steadyGame{})
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
		executioner := engine.Executioner()
		run := func(n int) {
			for range n {
				if err := executioner.PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
					t.Fatalf("publishing the update: %v", err)
				}
			}
		}
		execute := func(err error) {
			if err != nil {
				t.Fatalf("executing a command: %v", err)
			}
		}

		_, err := executioner.ExecuteCommand[growCmd](1_000)
		execute(err)
		run(100)
		_, err = executioner.ExecuteCommand[growCmd](20_000)
		execute(err)
		run(1)
		_, err = executioner.ExecuteCommand[cutCmd](1_000)
		execute(err)
		run(1)
		if shrink {
			released, err := executioner.ExecuteCommand[ecs.ShrinkCmd](ecs.ShrinkRequest{})
			execute(err)
			if released.Stores == 0 || released.Entities == 0 || released.Scratch == 0 {
				t.Fatalf("the zero request after a spike released %+v, want every area but Hooks above 0", released)
			}
		}
		// The regrowth frames, excluded: the free list, the Stores and the walks
		// grow back to what the steady state needs.
		run(100)

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		run(frames)
		runtime.ReadMemStats(&after)
		return float64(after.Mallocs-before.Mallocs) / frames
	}

	control := measure(false)
	shrunk := measure(true)
	t.Logf("objects a frame after a spike: control %.3f, after the zero request %.3f", control, shrunk)
	if shrunk > control+0.05 {
		t.Fatalf("after the zero request the frame costs %.3f objects against its control's %.3f", shrunk, control)
	}
}
