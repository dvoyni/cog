package internal

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

type placed struct{ Place m.Transform }

type placeSystem kernel.Subscription[app.UpdateEvent]

type readPlaceSystem kernel.Subscription[app.UpdateEvent]

// placerPlugin is a binding that depends on ecs and nothing else: it registers
// no Component of its own, spawns an Entity with an m.Transform, and reads the
// Transform back through a Query.
type placerPlugin struct {
	spawned Entity
	read    []m.Transform
}

func (*placerPlugin) Name() kernel.PluginName { return "placer" }

func (*placerPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (p *placerPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[placeSystem](ToHandler[app.UpdateEvent](registrar, func(sp *Spawn[placed]) {
		if p.spawned == NoEntity {
			p.spawned = sp.New(placed{Place: m.At(1, 2, 3)})
		}
	})).First()
	registrar.Subscribe[readPlaceSystem](ToHandler[app.UpdateEvent](registrar, func(q *Query[placed]) {
		p.read = p.read[:0]
		for _, it := range q.All() {
			p.read = append(p.read, it.Place)
		}
	})).Last()
	return nil
}

// rivalPlugin registers a second Store for m.Transform, which is exactly the
// second placement the ecs plugin owning it exists to rule out.
type rivalPlugin struct{}

func (rivalPlugin) Name() kernel.PluginName { return "rival" }

func (rivalPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{Name} }

func (rivalPlugin) Register(registrar *kernel.Registrar, _ any) error {
	RegisterComponent[m.Transform](registrar, 8)
	return nil
}

func runEngine(t *testing.T, plugins ...kernel.Plugin) *kernel.Engine {
	t.Helper()
	engine := kernel.New(nil).
		Handler(func(err error) error { t.Errorf("unexpected kernel error: %v", err); return err }).
		WithPlugins(plugins...)
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
	return engine
}

// TestTheEcsPluginAloneOwnsTheTransformStore also holds the Store on the ECS's
// fast path: a Transform is plain values, so its Store moves by size and its
// spans are noscan, which recording thousands of placed Entities relies on.
func TestTheEcsPluginAloneOwnsTheTransformStore(t *testing.T) {
	if err := PointerFree(reflect.TypeFor[m.Transform]()); err != nil {
		t.Fatalf("m.Transform is not pointer-free: %v", err)
	}
	engine := runEngine(t, New())
	var owner kernel.PluginName
	for _, resource := range engine.Describe().Resources {
		if resource.Type == reflect.TypeFor[*Store[m.Transform]]() {
			owner = resource.Owner
		}
	}
	if owner != Name {
		t.Fatalf("*ecs.Store[m.Transform] is owned by %q, want %q", owner, Name)
	}
}

// TestAPluginDependingOnlyOnEcsPlacesAnEntity is the cost of the ecs plugin
// owning the Store, made executable: a binding needs no dependency beyond ecs to
// write or read where an Entity stands, so the coupling check never sees it.
func TestAPluginDependingOnlyOnEcsPlacesAnEntity(t *testing.T) {
	placer := &placerPlugin{}
	engine := runEngine(t, New(), placer)
	for range 2 {
		engine.Executioner().PublishEvent(app.UpdateEvent{Dt: 1}).Wait()
	}
	if len(placer.read) != 1 || placer.read[0] != m.At(1, 2, 3) {
		t.Fatalf("the Query read %+v, want the one Transform spawned at (1,2,3)", placer.read)
	}
}

func TestASecondTransformStoreFailsComposition(t *testing.T) {
	var failure error
	kernel.New(nil).
		Handler(func(err error) error { failure = errors.Join(failure, err); return err }).
		WithPlugins(New(), rivalPlugin{})
	var duplicate kernel.ErrDuplicateRegistration
	if !errors.As(failure, &duplicate) {
		t.Fatalf("a second m.Transform Store composed with %v", failure)
	}
	if duplicate.Owner != "rival" || duplicate.Existing != Name ||
		duplicate.Type != reflect.TypeFor[*Store[m.Transform]]() {
		t.Errorf("the refusal is %+v, want rival's *ecs.Store[m.Transform] refused as already %q's", duplicate, Name)
	}
}
