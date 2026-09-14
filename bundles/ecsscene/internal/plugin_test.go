package internal

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene/sceneplugin"
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
	Place *ecsscene.Transform
}

type driftSystem kernel.Subscription[app.UpdateEvent]

// moverPlugin is a game that names the binding's Components through the
// root alone and orders itself against the binding's identity.
type moverPlugin struct{ deps []kernel.PluginName }

func (*moverPlugin) Name() kernel.PluginName             { return "mover" }
func (p *moverPlugin) Dependencies() []kernel.PluginName { return p.deps }

func (*moverPlugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[driftSystem](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[driftQuery]) {
		for _, it := range q.All() {
			it.Place.Position = it.Place.Position.Add(m.Vec3{X: 1})
		}
	})).Before[ecsscene.RecordOnUpdate]()
	return nil
}

// compose builds the binding's engine with the mover beside it and returns
// every error composition reported.
func compose(mover *moverPlugin) error {
	var failure error
	kernel.New(map[kernel.PluginName]any{storage.Name: storage.Config{}}).
		Handler(func(err error) bool { failure = errors.Join(failure, err); return false }).
		WithPlugins(storageplugin.New(), permanentAdapter{}, appplugin.New(), driverAdapter{}, gfxplugin.New(), backendAdapter{&detachedBackend{}},
			sceneplugin.New(), ecsplugin.New(), New(), mover)
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
		undeclared.Resource != reflect.TypeFor[*ecs.Store[ecsscene.Transform]]() {
		t.Errorf("the refusal is %+v, want mover locking *ecs.Store[ecsscene.Transform] owned by %q",
			undeclared, ecsscene.Name)
	}

	if err := compose(&moverPlugin{deps: []kernel.PluginName{ecs.Name, ecsscene.Name}}); err != nil {
		t.Fatalf("a System declaring ecsscene did not compose: %v", err)
	}
}
