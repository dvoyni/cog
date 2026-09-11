package proto

import (
	"errors"
	"strings"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// The zero-allocation prototype ran on one plugin, so it never exercised the
// question cog#244 asks: who owns a Component store, and what does the coupling
// check do once Components are in play. This file splits the same design across
// three plugins and composes it, because "arguably correct and arguably
// crippling" is a thing to measure rather than argue about.

// Guarded is this file's own Component, so it cannot collide with the ones
// protoPlugin registers.
type Guarded struct{ V float64 }

type GuardedQ struct{ G *Guarded }

type (
	guardSub kernel.Subscription[app.UpdateEvent]
)

func guardedSystem(q *ecs.Query[GuardedQ], ev app.UpdateEvent) {
	for _, g := range q.All() {
		g.G.V += ev.Dt
	}
}

// couplingWorld is the registration-time handle the plugins share. It is a
// plain Go value passed to each plugin's constructor, and it has to be: a
// kernel resource is readable only inside a handler under its locks, so the
// *Entities a builder needs at registration cannot come through one.
type couplingWorld struct {
	en    *ecs.Entities
	store *ecs.Store[Guarded]
}

// ecsPlug stands in for the ecs plugin: it owns the id authority and nothing else.
type ecsPlug struct{ w *couplingWorld }

func (ecsPlug) Name() kernel.PluginName           { return "ecs" }
func (ecsPlug) Dependencies() []kernel.PluginName { return nil }
func (p ecsPlug) Register(r *kernel.Registrar, _ any) error {
	r.InitResource[*ecs.Entities](p.w.en)
	return nil
}

// ecsPlugOwningComponents is the counterfactual: the ecs plugin registers the
// Component itself, so every store is owned by "ecs".
type ecsPlugOwningComponents struct{ w *couplingWorld }

func (ecsPlugOwningComponents) Name() kernel.PluginName           { return "ecs" }
func (ecsPlugOwningComponents) Dependencies() []kernel.PluginName { return nil }
func (p ecsPlugOwningComponents) Register(r *kernel.Registrar, _ any) error {
	r.InitResource[*ecs.Entities](p.w.en)
	p.w.store = ecs.RegisterComponent[Guarded](r, p.w.en, 64)
	return nil
}

// compPlug declares the Component, so under the caller-owns rule it owns the store.
type compPlug struct{ w *couplingWorld }

func (compPlug) Name() kernel.PluginName           { return "components" }
func (compPlug) Dependencies() []kernel.PluginName { return []kernel.PluginName{"ecs"} }
func (p compPlug) Register(r *kernel.Registrar, _ any) error {
	p.w.store = ecs.RegisterComponent[Guarded](r, p.w.en, 64)
	return nil
}

// sysPlug has a System over a Component it did not declare. Its dependency list
// is the variable under test.
type sysPlug struct {
	w    *couplingWorld
	deps []kernel.PluginName
}

func (sysPlug) Name() kernel.PluginName             { return "systems" }
func (p sysPlug) Dependencies() []kernel.PluginName { return p.deps }
func (p sysPlug) Register(r *kernel.Registrar, _ any) error {
	r.Subscribe[guardSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](p.w.en, guardedSystem))
	return nil
}

// compose runs composition only -- WithPlugins registers and finalizes
// synchronously -- and collects whatever the error handler is given.
func compose(t *testing.T, plugins ...kernel.Plugin) (*kernel.Engine, []error) {
	t.Helper()
	var errs []error
	e := kernel.New(nil).
		Handler(func(err error) bool { errs = append(errs, err); return true }).
		WithPlugins(plugins...)
	return e, errs
}

func newCouplingWorld() *couplingWorld { return &couplingWorld{en: ecs.NewEntities(64)} }

// A System reaching a Component registered by another plugin composes when it
// declares that plugin -- which is the ordinary case, since it already imports
// the Component's Go type.
func TestSystemComposesWhenItDeclaresTheComponentsPlugin(t *testing.T) {
	w := newCouplingWorld()
	e, errs := compose(t, ecsPlug{w}, compPlug{w}, sysPlug{w, []kernel.PluginName{"ecs", "components"}})
	if len(errs) != 0 {
		t.Fatalf("composition failed: %v", errors.Join(errs...))
	}
	// The store is an ordinary resource, owned and reported like any other.
	// Note the rendered name: an instantiated generic spells its type argument
	// with the full import path, so a real user sees
	// *ecs.Store[github.com/dvoyni/nox/game.Health].
	var found bool
	for _, res := range e.Describe().Resources {
		if strings.HasPrefix(res.Type.String(), "*ecs.Store[") && strings.Contains(res.Type.String(), "Guarded") {
			found = true
			t.Logf("Describe reports %v owned by %q", res.Type, res.Owner)
			if res.Owner != "components" {
				t.Fatalf("store owner = %q, want components", res.Owner)
			}
		}
	}
	if !found {
		t.Fatalf("Describe did not report the Component store; got %v", e.Describe().Resources)
	}
}

// The cost of caller-owns, stated exactly: reaching another plugin's Component
// without declaring it is a composition failure, and the error names the store
// type rather than the Component type.
func TestSystemFailsCompositionWhenItDoesNotDeclareTheComponentsPlugin(t *testing.T) {
	w := newCouplingWorld()
	_, errs := compose(t, ecsPlug{w}, compPlug{w}, sysPlug{w, []kernel.PluginName{"ecs"}})
	if len(errs) == 0 {
		t.Fatalf("composition succeeded; the coupling check did not fire on a Component store")
	}
	joined := errors.Join(errs...)
	var undeclared kernel.ErrUndeclaredDependency
	if !errors.As(joined, &undeclared) {
		t.Fatalf("want ErrUndeclaredDependency, got %v", joined)
	}
	if undeclared.Plugin != "systems" || undeclared.Owner != "components" {
		t.Fatalf("error names plugin %q owner %q", undeclared.Plugin, undeclared.Owner)
	}
	t.Logf("the error a user sees: %v", undeclared)
}

// The counterfactual: with every store owned by "ecs", the same System composes
// with no dependency on the plugin that declared the Component. That is the
// whole trade -- ownership by ecs buys convenience by making the check vacuous.
func TestOwnershipByEcsMakesTheCouplingCheckVacuous(t *testing.T) {
	w := newCouplingWorld()
	_, errs := compose(t, ecsPlugOwningComponents{w}, sysPlug{w, []kernel.PluginName{"ecs"}})
	if len(errs) != 0 {
		t.Fatalf("composition failed: %v", errors.Join(errs...))
	}
}

// Registering the same Component twice is caught, and names both plugins -- the
// diagnostic that ownership by ecs would lose.
func TestDuplicateComponentRegistrationIsCaught(t *testing.T) {
	w := newCouplingWorld()
	_, errs := compose(t, ecsPlugOwningComponents{w}, compPlug{w},
		sysPlug{w, []kernel.PluginName{"ecs", "components"}})
	if len(errs) == 0 {
		t.Fatalf("composition succeeded; a Component registered twice was not caught")
	}
	joined := errors.Join(errs...)
	var duplicate kernel.ErrDuplicateRegistration
	if !errors.As(joined, &duplicate) {
		t.Fatalf("want ErrDuplicateRegistration, got %v", joined)
	}
	t.Logf("the error a user sees: %v", duplicate)
}

// A Query naming a Component nobody registered fails composition rather than
// reaching Run, and the ECS catches it before kernel's ErrMissingResource can.
func TestQueryOverAnUnregisteredComponentFailsComposition(t *testing.T) {
	w := newCouplingWorld()
	_, errs := compose(t, ecsPlug{w}, sysPlug{w, []kernel.PluginName{"ecs"}})
	if len(errs) == 0 {
		t.Fatalf("composition succeeded with no store for the queried Component")
	}
	t.Logf("the error a user sees: %v", errors.Join(errs...))
}
