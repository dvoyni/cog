package internal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

type position struct{ X float32 }

type velocity struct{ X float32 }

type projectile struct {
	Position position
	Velocity velocity
}

type moveQuery struct {
	Position *position
	Velocity velocity
}

type spawnSystem kernel.Subscription[app.UpdateEvent]

type moveSystem kernel.Subscription[app.UpdateEvent]

// gamePlugin is a game as an app writes one: it registers its Components and
// subscribes its Systems through ecs's root, and declares a dependency
// on ecs.Name, which is what registers the authority first.
type gamePlugin struct {
	positions *ecs.Store[position]
	spawned   ecs.Entity
}

func (*gamePlugin) Name() kernel.PluginName { return "game" }

func (*gamePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.positions = ecs.RegisterComponent[position](registrar, 8)
	ecs.RegisterComponent[velocity](registrar, 8)
	registrar.Subscribe[spawnSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(sp *ecs.Spawn[projectile]) {
		if p.spawned == ecs.NoEntity {
			p.spawned = sp.New(projectile{Velocity: velocity{X: 2}})
		}
	})).First()
	registrar.Subscribe[moveSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[moveQuery]) {
		for _, it := range q.All() {
			it.Position.X += it.Velocity.X
		}
	})).Last()
	return nil
}

// TestNewPublishesTheAuthority composes the real plugin with a game beside it:
// the game's Components and Systems reach the authority New published, a spawn
// allocates through it, and a Query walks what the spawn wrote.
func TestNewPublishesTheAuthority(t *testing.T) {
	game := &gamePlugin{}
	engine := kernel.New(map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: 16}}).
		Handler(func(err error) bool { t.Errorf("unexpected kernel error: %v", err); return true }).
		WithPlugins(New(), game)
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

	for range 3 {
		if err := engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait(); err != nil {
			t.Fatalf("publishing the update: %v", err)
		}
	}

	if n := game.positions.Len(); n != 1 {
		t.Fatalf("three ticks left %d positions, want the one spawned", n)
	}
	if got, ok := game.positions.Get(game.spawned); !ok || got.X != 6 {
		t.Fatalf("the spawned Entity's position is %+v, %v after three ticks, want {X:6}, true", got, ok)
	}
	var registered bool
	for _, resource := range engine.Describe().Resources {
		if resource.Type == reflect.TypeFor[*ecs.Entities]() && resource.Owner == ecs.Name {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("the architecture has no *ecs.Entities owned by %q", ecs.Name)
	}
}

func TestAMissingAuthorityFailsComposition(t *testing.T) {
	var failure error
	kernel.New(nil).
		Handler(func(err error) bool { failure = errors.Join(failure, err); return true }).
		WithPlugins(&gamePlugin{})
	if failure == nil {
		t.Fatalf("a game composed without ecsplugin.New() composed")
	}
}
