package internal

import (
	"errors"
	"reflect"
	"testing"

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
	positions *Store[position]
	spawned   Entity
}

func (*gamePlugin) Name() kernel.PluginName { return "game" }

func (*gamePlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *gamePlugin) Register(registrar *kernel.Registrar, _ any) error {
	p.positions = RegisterComponent[position](registrar, 8)
	RegisterComponent[velocity](registrar, 8)
	registrar.Subscribe[spawnSystem](ToHandler[app.UpdateEvent](registrar, func(sp *Spawn[projectile]) {
		if p.spawned == NoEntity {
			p.spawned = sp.New(projectile{Velocity: velocity{X: 2}})
		}
	})).First()
	registrar.Subscribe[moveSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[moveQuery]) {
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
	engine := kernel.New(map[kernel.PluginName]any{Name: Config{PrewarmEntities: 16}}).
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

	for range 3 {
		engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	}

	if n := game.positions.Len(); n != 1 {
		t.Fatalf("three ticks left %d positions, want the one spawned", n)
	}
	if got, ok := game.positions.Get(game.spawned); !ok || got.X != 6 {
		t.Fatalf("the spawned Entity's position is %+v, %v after three ticks, want {X:6}, true", got, ok)
	}
	var registered bool
	for _, resource := range engine.Describe().Resources {
		if resource.Type == reflect.TypeFor[*Entities]() && resource.Owner == Name {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("the architecture has no *ecs.Entities owned by %q", Name)
	}
}

func TestAMissingAuthorityFailsComposition(t *testing.T) {
	var failure error
	kernel.New(nil).
		Handler(func(err error) error { failure = errors.Join(failure, err); return err }).
		WithPlugins(&gamePlugin{})
	if failure == nil {
		t.Fatalf("a game composed without ecsplugin.New() composed")
	}
}
