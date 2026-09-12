package proto

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// The Bundle conversion cog#246 built solves *spawning* by name and nothing
// else. The case it does not reach is the common one: changing an animation
// clip from "Idle" to "Walk" while the game runs. That is a write to a
// Component every frame, so it cannot go through a conversion, and it cannot go
// through an interning table either -- interning assigns the id, so the table
// would have to be declared and written by every System that ever changes a
// clip, bloating the lock set of a System whose actual business is two
// Components.
//
// A hash has no table on the writing side. This file measures whether that
// holds up.

// Clip and Model are domain tags. They carry no data; they exist so that a clip
// name cannot be assigned where a model name belongs.
type (
	Clip  struct{}
	Model struct{}
)

// The names, hashed once at package level. This is the thing an interned index
// cannot do: there is no table at package initialisation, and under cog#245
// two Engines would have two tables and two different ids for this one string.
var (
	idleClip = ecs.NameOf[Clip]("Idle")
	walkClip = ecs.NameOf[Clip]("Walk")
)

// Animated is a legal Component carrying what was declared as a string.
type Animated struct {
	Clip ecs.Name[Clip]
	Time float32
}

type Mover struct{ Speed float32 }

type AnimQ struct {
	A *Animated
	M Mover
}

// animate is the System the whole question is about. It changes which clip
// plays, every frame, and its signature names two Components and nothing else
// -- no table, no resource, no conversion.
func animate(q *ecs.Query[AnimQ]) {
	for _, it := range q.All() {
		if it.M.Speed > 0.1 {
			it.A.Clip = walkClip
		} else {
			it.A.Clip = idleClip
		}
	}
}

type animSub kernel.Subscription[app.UpdateEvent]

type animWorld struct {
	en    *ecs.Entities
	anims *ecs.Store[Animated]
	seed  []ecs.Entity
}

type animPlug struct {
	w *animWorld
	n int
	// interned swaps in the System that assigns through a synchronised table
	// instead, which is what a shared Hash[T] would require.
	interned bool
	table    *syncInterner
}

func (animPlug) Name() kernel.PluginName           { return "anim" }
func (animPlug) Dependencies() []kernel.PluginName { return nil }

func (p animPlug) Register(r *kernel.Registrar, _ any) error {
	ids := uint32(p.n + 16)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)
	anims := ecs.RegisterComponent[Animated](r, en, ids)
	movers := ecs.RegisterComponent[Mover](r, en, ids)
	p.w.en, p.w.anims = en, anims
	for i := range p.n {
		e := en.Alloc()
		anims.Add(e, Animated{Clip: idleClip})
		// Half of them move, so one tick has to produce both clips.
		movers.Add(e, Mover{Speed: float32(i % 2)})
		p.w.seed = append(p.w.seed, e)
	}
	system := any(animate)
	if p.interned {
		system = animateInterned(p.table)
	}
	r.Subscribe[animSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, system))
	return nil
}

func startAnim(tb testing.TB, p animPlug) *kernel.Engine {
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

// The case the Bundle conversion could not reach: the clip changes mid-frame,
// and the System that changes it declares nothing but the two Components it
// already touches.
func TestAClipChangesMidFrameWithNoExtraLock(t *testing.T) {
	const n = 16
	w := &animWorld{}
	e := startAnim(t, animPlug{w: w, n: n})
	e.Executioner().PublishEvent(tick).Wait()

	for i, entity := range w.seed {
		got, ok := w.anims.Get(entity)
		if !ok {
			t.Fatalf("entity %d lost its Animated", i)
		}
		want := idleClip
		if i%2 == 1 {
			want = walkClip
		}
		if got.Clip != want {
			t.Fatalf("entity %d plays %v, want %v", i, got.Clip, want)
		}
	}
	t.Logf("clips switched by a System naming two Components and nothing else")
}

// A Name is the same value everywhere, which is what makes it writable from a
// package-level var -- and what an assigned index can never be, because the
// index depends on which table assigned it and in what order.
func TestANameIsTheSameEverywhereAndAnIndexIsNot(t *testing.T) {
	if ecs.NameOf[Clip]("Walk") != walkClip {
		t.Fatalf("the same string hashed to two different Names")
	}

	// Two worlds, two tables, the same two names registered in opposite orders
	// -- which is all it takes for an index to disagree with itself.
	a, b := newModelTable(), newModelTable()
	a.Intern("models/crate.glb")
	a.Intern("models/barrel.glb")
	b.Intern("models/barrel.glb")
	b.Intern("models/crate.glb")
	crateA, _ := a.Lookup("models/crate.glb")
	crateB, _ := b.Lookup("models/crate.glb")
	if crateA == crateB {
		t.Fatalf("two independently built tables happened to agree; the test proves nothing")
	}
	t.Logf("one string, two tables, ids %d and %d -- but one Name: %v",
		crateA, crateB, ecs.NameOf[Model]("models/crate.glb"))
}

// A Name carries no pointer, which is the whole reason it exists.
func TestAnAnimatedComponentIsPointerFree(t *testing.T) {
	if err := ecs.PointerFree(reflect.TypeFor[Animated]()); err != nil {
		t.Fatalf("Animated is not a legal Component: %v", err)
	}
	// And the phantom tag really does separate the domains, so a clip name
	// cannot be assigned where a model name belongs.
	if reflect.TypeFor[ecs.Name[Clip]]() == reflect.TypeFor[ecs.Name[Model]]() {
		t.Fatalf("Name[Clip] and Name[Model] are the same type")
	}
}

// The consumer's half: resolve a Name back to the thing, and back to its text
// for tooling. Also the scale check -- 64 bits has to survive a real asset
// list without a collision, and the table catches one if it ever happens.
func TestANameTableResolvesAtRealisticScale(t *testing.T) {
	const n = 100_000
	table := ecs.NewNameTable[Model, int]()
	names := make([]ecs.Name[Model], n)
	for i := range n {
		text := fmt.Sprintf("models/props/%05d/variant_%03d.glb", i, i%128)
		if err := table.Register(text, i); err != nil {
			t.Fatalf("registering %d names hit a collision: %v", n, err)
		}
		names[i] = ecs.NameOf[Model](text)
	}
	if table.Len() != n {
		t.Fatalf("table holds %d of %d names", table.Len(), n)
	}
	for i, name := range names {
		got, ok := table.Lookup(name)
		if !ok || got != i {
			t.Fatalf("name %d resolved to %v ok=%v", i, got, ok)
		}
	}
	text, ok := table.TextOf(names[7])
	if !ok || text != "models/props/00007/variant_007.glb" {
		t.Fatalf("TextOf gave %q ok=%v", text, ok)
	}
	t.Logf("%d names, no collision, every one resolves and reverses", n)
}

// syncInterner is what a shared Hash[T] would have to be: a table that *assigns*
// the id, so it is shared mutable state and needs synchronisation the scheduler
// knows nothing about.
type syncInterner struct {
	mu   sync.Mutex
	next uint64
	ids  sync.Map // string -> uint64
}

func (s *syncInterner) id(text string) ecs.Name[Clip] {
	if v, ok := s.ids.Load(text); ok {
		return ecs.NameFromRaw[Clip](v.(uint64))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.ids.Load(text); ok {
		return ecs.NameFromRaw[Clip](v.(uint64))
	}
	s.next++
	s.ids.Store(text, s.next)
	return ecs.NameFromRaw[Clip](s.next)
}

func animateInterned(in *syncInterner) any {
	return func(q *ecs.Query[AnimQ]) {
		for _, it := range q.All() {
			if it.M.Speed > 0.1 {
				it.A.Clip = in.id("Walk")
			} else {
				it.A.Clip = in.id("Idle")
			}
		}
	}
}

// What the two cost per frame. The hash side is a package-level var and a
// 64-bit copy; the interner side is what assigning an id actually takes once
// the table has to be safe to touch from a System.
func BenchmarkAssignAClip(b *testing.B) {
	run := func(b *testing.B, n int, interned bool) {
		e := startAnim(b, animPlug{w: &animWorld{}, n: n, interned: interned, table: &syncInterner{}})
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
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("hashed-name/%d", n), func(b *testing.B) { run(b, n, false) })
		b.Run(fmt.Sprintf("sync-interner/%d", n), func(b *testing.B) { run(b, n, true) })
	}
}
