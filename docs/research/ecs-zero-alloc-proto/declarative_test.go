package proto

import (
	"context"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/scene"

	"protoecs/ecs"
)

// The ergonomic objection to naming an asset by a handle was that spawning
// stops being declarative: instead of writing the model's name where the entity
// is created, the author would have to intern it somewhere else and thread an
// id to every spawn site.
//
// With a hash there is nothing to solve. Hashing is pure, so the name resolves
// at package level, and the Bundle field simply *is* the Component field. No
// conversion, no table at the spawn site, no lock beyond the Components.

var cratePath = "models/crate.glb"

// crateModel is the declaration, hashed once. This is what an interned index
// could not be.
var crateModel = ecs.HashOf[ModelHash](cratePath)

// DeclBundle names the model where the entity is made.
type DeclBundle struct {
	P Placement
	D Drawable
}

type declSub kernel.Subscription[app.UpdateEvent]

type declWorld struct {
	en        *ecs.Entities
	drawables *ecs.Store[Drawable]
	made      []ecs.Entity
}

type declPlug struct{ w *declWorld }

func (declPlug) Name() kernel.PluginName           { return "declarative" }
func (declPlug) Dependencies() []kernel.PluginName { return nil }

func (p declPlug) Register(r *kernel.Registrar, _ any) error {
	const ids = 64
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	ecs.RegisterComponent[Placement](r, en, ids)
	p.w.en = en
	p.w.drawables = ecs.RegisterComponent[Drawable](r, en, ids)

	w := p.w
	r.Subscribe[declSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
		func(sp *ecs.Spawn[DeclBundle]) {
			if len(w.made) > 0 {
				return
			}
			for i := range 3 {
				w.made = append(w.made, sp.New(DeclBundle{
					P: Placement{Scale: float32(i + 1)},
					D: Drawable{Model: crateModel, Layers: scene.LayersAll},
				}))
			}
		}))
	return nil
}

// A Bundle names the model at the spawn site and what lands in the Store is a
// pointer-free hash. Nothing converts anything.
func TestABundleNamesAModelWhereTheEntityIsMade(t *testing.T) {
	w := &declWorld{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { t.Errorf("kernel error: %v", err); return true }).
		WithPlugins(declPlug{w})
	go e.Run(ctx)
	<-e.Ready()
	e.Executioner().PublishEvent(tick).Wait()

	if len(w.made) != 3 {
		t.Fatalf("spawned %d entities, want 3", len(w.made))
	}
	for i, entity := range w.made {
		got, ok := w.drawables.Get(entity)
		if !ok {
			t.Fatalf("entity %d has no Drawable", i)
		}
		if got.Model != crateModel {
			t.Fatalf("entity %d names %v, want %v", i, got.Model, crateModel)
		}
	}
	// And the same string hashes to the same thing anywhere, which is why the
	// package-level var above is legal at all.
	if ecs.HashOf[ModelHash](cratePath) != crateModel {
		t.Fatalf("the same path hashed to two different values")
	}
	t.Logf("three Bundles naming a model by path; three pointer-free Drawables")
}
