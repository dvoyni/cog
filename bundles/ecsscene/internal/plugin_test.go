package internal

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/model/modelplugin"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
	"github.com/dvoyni/cog/slots/gfx/gfxplugin"
	"github.com/dvoyni/cog/slots/storage"
	"github.com/dvoyni/cog/slots/storage/storageplugin"
)

// driftQuery moves a Transform, as a game System does before the binding
// records it.
type driftQuery struct {
	Place *m.Transform
}

// retuneQuery writes one of the binding's own Components.
type retuneQuery struct {
	Model *ecsscene.Model
}

type driftSystem kernel.Subscription[app.UpdateEvent]

// moverPlugin is a game that orders itself against the binding's identity. It
// writes one of the binding's Components, or, when it only places, the
// m.Transform the ecs plugin owns.
type moverPlugin struct {
	deps   []kernel.PluginName
	places bool
}

func (*moverPlugin) Name() kernel.PluginName             { return "mover" }
func (p *moverPlugin) Dependencies() []kernel.PluginName { return p.deps }

func (p *moverPlugin) Register(registrar *kernel.Registrar, _ any) error {
	system := ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[retuneQuery]) {
		for _, it := range q.All() {
			it.Model.Layers = it.Model.Layers | ecsscene.Layer(1)
		}
	})
	if p.places {
		system = ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[driftQuery]) {
			for _, it := range q.All() {
				it.Place.Position = it.Place.Position.Add(m.Vec3{X: 1})
			}
		})
	}
	registrar.Subscribe[driftSystem](system).Before[ecsscene.RecordOnUpdate]()
	return nil
}

// compose builds the binding's engine with the mover beside it and returns
// every error composition reported.
func compose(mover *moverPlugin) error {
	var failure error
	kernel.New(map[kernel.PluginName]any{storage.Name: storage.Config{}}).
		Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), mainLoopAdapter{}, gfxplugin.New(), backendAdapter{&detachedBackend{}},
			modelplugin.New(), ecsplugin.New(), New(), mover)
	return failure
}

// TestTheCouplingCheckStillHoldsOnTheBindingsComponents is the coupling rule
// across the split. The Components are declared in the root and
// registered by the internal plugin under ecsscene.Name, so a game System
// that locks one of their Stores must still declare ecsscene, and composes
// once it does.
func TestTheCouplingCheckStillHoldsOnTheBindingsComponents(t *testing.T) {
	err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name}})
	var undeclared kernel.ErrUndeclaredDependency
	if !errors.As(err, &undeclared) {
		t.Fatalf("a System locking the binding's Stores without declaring ecsscene composed with %v", err)
	}
	if undeclared.Plugin != "mover" || undeclared.Owner != ecsscene.Name ||
		undeclared.Resource != reflect.TypeFor[*ecs.Store[ecsscene.Model]]() {
		t.Errorf("the refusal is %+v, want mover locking *ecs.Store[ecsscene.Model] owned by %q",
			undeclared, ecsscene.Name)
	}

	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name, ecsscene.Name}}); err != nil {
		t.Fatalf("a System declaring ecsscene did not compose: %v", err)
	}
}

// TestASystemPlacingEntitiesNeedsOnlyEcs is the documented cost of the ecs
// plugin owning the m.Transform Store: every plugin with Systems already
// depends on ecs, so a System writing where an Entity stands composes without
// declaring this binding, and the coupling check never sees it.
func TestASystemPlacingEntitiesNeedsOnlyEcs(t *testing.T) {
	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name}, places: true}); err != nil {
		t.Fatalf("a System writing m.Transform with only an ecs dependency did not compose: %v", err)
	}
}

// TestEveryComponentIsStorable checks each Component against the ECS's own
// gate. Registration runs the same check, but a refusal there surfaces as a
// failed composition; this names the type that broke it.
func TestEveryComponentIsStorable(t *testing.T) {
	for _, component := range []reflect.Type{
		reflect.TypeFor[ecsscene.Model](),
		reflect.TypeFor[ecsscene.Mesh](),
		reflect.TypeFor[ecsscene.Animation](),
		reflect.TypeFor[ecsscene.Params](),
		reflect.TypeFor[ecsscene.Material](),
		reflect.TypeFor[ecsscene.Light](),
		reflect.TypeFor[ecsscene.Camera](),
	} {
		if err := ecs.Storable(component); err != nil {
			t.Errorf("%s is not storable: %v", component, err)
		}
	}
}
