package proto

import (
	"context"
	"fmt"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"

	"github.com/dvoyni/cog/scene"
)

// The ergonomic objection to naming an asset by a handle: spawning stops being
// declarative. Instead of writing the model's name where the entity is created,
// the author has to intern it somewhere else and thread the id to every spawn
// site.
//
// It does not have to. cog#237 defines a Bundle as describing *one act of
// creation*, explicitly not as a Component set -- so a Bundle field is free to
// be a string even though the Component it becomes may not be. The conversion
// happens inside the Spawn, which already holds the write lock on the Component
// it produces.

// ModelPath is what a Bundle writes, and it is a plain string.
type ModelPath string

// DeclBundle is the declarative form: the model is named where the entity is
// made. Neither field is what is stored -- Placement is, and ModelPath becomes
// a Drawable carrying a dense id.
type DeclBundle struct {
	P Placement
	M ModelPath
}

// ResolvedBundle is the same spawn with the interning already done by hand at
// the call site, which is what the objection says the handle forces. It is the
// baseline the declarative form is measured against.
type ResolvedBundle struct {
	P Placement
	D Drawable
}

type declSub kernel.Subscription[app.UpdateEvent]

// missingModel is the id a path nobody registered resolves to. A conversion
// cannot report, so an unknown name has to have an answer; scene's own rule is
// the same shape -- a selector that matches nothing draws nothing.
const missingModel ModelID = 0

type declWorld struct {
	en         *ecs.Entities
	drawables  *ecs.Store[Drawable]
	placements *ecs.Store[Placement]
	made       []ecs.Entity
}

type declPlug struct {
	w *declWorld
	// perTick is how many entities the System spawns and despawns each frame,
	// 0 for the correctness test, which spawns once from the test instead.
	perTick  int
	resolved bool
	table    *modelTable
	paths    []ModelPath
}

func (declPlug) Name() kernel.PluginName           { return "declarative" }
func (declPlug) Dependencies() []kernel.PluginName { return nil }

func (p declPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.perTick*2 + 64)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	p.w.en = en
	p.w.placements = ecs.RegisterComponent[Placement](r, en, ids)
	p.w.drawables = ecs.RegisterComponent[Drawable](r, en, ids)

	// The whole of it. A ModelPath in a Bundle stands for a Drawable, and this
	// is how you get from one to the other.
	//
	// The table is interned at registration and read-only afterwards. That is
	// deliberate: the conversion runs inside the Spawn's lock set, which covers
	// the Components it writes and nothing else, so a conversion that *mutated*
	// shared state would be racing two concurrent spawning Systems with no lock
	// between them. What a conversion may touch is exactly the question cog#256
	// owns.
	table := p.table
	ecs.RegisterConversion(en, func(path ModelPath) Drawable {
		id, ok := table.Lookup(string(path))
		if !ok {
			id = missingModel
		}
		return Drawable{Model: id, Layers: scene.LayersAll}
	})

	if p.perTick == 0 {
		return nil
	}
	scratch := make([]ecs.Entity, 0, p.perTick)
	paths := p.paths
	if p.resolved {
		ids := make([]Drawable, len(paths))
		for i, path := range paths {
			id, _ := table.Lookup(string(path))
			ids[i] = Drawable{Model: id, Layers: scene.LayersAll}
		}
		r.Subscribe[declSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
			func(sp *ecs.Spawn[ResolvedBundle], we *ecs.WriteableEntities) {
				scratch = scratch[:0]
				for i := range p.perTick {
					scratch = append(scratch, sp.New(ResolvedBundle{
						P: Placement{Scale: 1}, D: ids[i%len(ids)],
					}))
				}
				for _, e := range scratch {
					we.Despawn(e)
				}
			}))
		return nil
	}
	r.Subscribe[declSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en,
		func(sp *ecs.Spawn[DeclBundle], we *ecs.WriteableEntities) {
			scratch = scratch[:0]
			for i := range p.perTick {
				scratch = append(scratch, sp.New(DeclBundle{
					P: Placement{Scale: 1}, M: paths[i%len(paths)],
				}))
			}
			for _, e := range scratch {
				we.Despawn(e)
			}
		}))
	return nil
}

// spawnOnce is the correctness case: a System that spawns the given bundles a
// single time, so the test can look at what landed in the Stores.
type onceSub kernel.Subscription[app.UpdateEvent]

type oncePlug struct {
	declPlug
	bundles []DeclBundle
}

func (oncePlug) Name() kernel.PluginName { return "declarative-once" }

func (p oncePlug) Register(r *kernel.Registrar, v any) error {
	if err := p.declPlug.Register(r, v); err != nil {
		return err
	}
	w, bundles := p.w, p.bundles
	r.Subscribe[onceSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](p.w.en,
		func(sp *ecs.Spawn[DeclBundle]) {
			if len(w.made) > 0 {
				return
			}
			for _, b := range bundles {
				w.made = append(w.made, sp.New(b))
			}
		}))
	return nil
}

func startDecl(tb testing.TB, p kernel.Plugin) *kernel.Engine {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)
	e := kernel.New(nil).
		Handler(func(err error) bool { tb.Errorf("kernel error: %v", err); return true }).
		WithPlugins(p)
	go e.Run(ctx)
	<-e.Ready()
	return e
}

// The declarative spawn survives the pointer-free rule: the author writes the
// model's name at the spawn site, and what lands in the Store is a dense id.
func TestABundleMayNameAModelByPath(t *testing.T) {
	table := newModelTable()
	crate := table.Intern("models/crate.glb")
	barrel := table.Intern("models/barrel.glb")
	w := &declWorld{}
	e := startDecl(t, oncePlug{
		declPlug: declPlug{w: w, table: table},
		bundles: []DeclBundle{
			{P: Placement{Scale: 1}, M: "models/crate.glb"},
			{P: Placement{Scale: 2}, M: "models/barrel.glb"},
			{P: Placement{Scale: 3}, M: "models/typo.glb"},
		},
	})
	e.Executioner().PublishEvent(tick).Wait()

	if len(w.made) != 3 {
		t.Fatalf("spawned %d entities, want 3", len(w.made))
	}
	want := []ModelID{crate, barrel, missingModel}
	for i, entity := range w.made {
		got, ok := w.drawables.Get(entity)
		if !ok {
			t.Fatalf("entity %d has no Drawable", i)
		}
		if got.Model != want[i] {
			t.Fatalf("entity %d resolved to %v, want %v", i, got.Model, want[i])
		}
	}
	t.Logf("three Bundles naming models by path became three pointer-free Drawables")
}

// What the conversion costs, whole-frame: spawning and despawning the same
// number of entities, naming the model by path against naming it by an id the
// caller interned itself.
func BenchmarkDeclarativeSpawn(b *testing.B) {
	paths := []ModelPath{
		"models/props/crate.glb", "models/props/barrel.glb",
		"models/props/crate_b.glb", "models/props/sack.glb",
	}
	run := func(b *testing.B, perTick int, resolved bool) {
		table := newModelTable()
		for _, p := range paths {
			table.Intern(string(p))
		}
		e := startDecl(b, declPlug{
			w: &declWorld{}, perTick: perTick, resolved: resolved,
			table: table, paths: paths,
		})
		ex := e.Executioner()
		for range 4 {
			ex.PublishEvent(tick).Wait()
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			ex.PublishEvent(tick).Wait()
		}
	}
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("by-path/%d", n), func(b *testing.B) { run(b, n, false) })
		b.Run(fmt.Sprintf("pre-resolved/%d", n), func(b *testing.B) { run(b, n, true) })
	}
}
